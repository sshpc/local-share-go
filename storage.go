package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// metaFile 是每个上传目录里记录的元数据文件名。
const metaFile = "meta.json"

// ErrNotFound 表示请求的文件 ID 不存在。
var ErrNotFound = errors.New("文件不存在")

// ErrInvalidID 表示文件 ID 格式非法。
var ErrInvalidID = errors.New("文件 ID 非法")

// FileMeta 是与一个已上传文件一同持久化的元数据。
type FileMeta struct {
	ID           string     `json:"id"`            // 文件 ID（UUID）
	OriginalName string     `json:"original_name"` // 用户上传时的原始文件名（已清理路径分隔符）
	Extension    string     `json:"extension"`     // 原始扩展名，含前导点，例如 ".pdf"
	Size         int64      `json:"size"`          // 字节数
	ContentType  string     `json:"content_type"`  // 上传时声明的 MIME 类型
	UploadedAt   time.Time  `json:"uploaded_at"`   // 上传时间
	DeletedAt    *time.Time `json:"deleted_at"`    // 软删除时间；nil 表示未删除
}

// Deleted 表示该文件是否已被软删除（在回收站里）。
func (m FileMeta) Deleted() bool { return m.DeletedAt != nil }

// SafeName 返回适合写入 Content-Disposition 的下载文件名。
// 原始文件名在 uploadHandler 中已去除路径分隔符，这里再做一次兜底。
func (m FileMeta) SafeName() string {
	name := cleanFileName(m.OriginalName)
	if name == "" {
		return m.ID + m.Extension
	}
	return name
}

// Storage 管理 upload_dir 下的文件存储。
//
// 布局：每个上传文件独占一个以 ID 命名的子目录，元数据与内容分开存放，
// 因此下载时无需遍历目录或猜测扩展名：
//
//	uploads/<id>/meta.json
//	uploads/<id>/data
type Storage struct {
	dir string
}

// NewStorage 创建存储并确保根目录存在。
func NewStorage(dir string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	return &Storage{dir: dir}, nil
}

// Dir 返回存储根目录。
func (s *Storage) Dir() string { return s.dir }

// validID 只接受 UUID 形态的 ID，杜绝把路径分隔符或 ".." 带进文件系统。
func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isDigit := r >= '0' && r <= '9'
			isLower := r >= 'a' && r <= 'f'
			isUpper := r >= 'A' && r <= 'F'
			if !isDigit && !isLower && !isUpper {
				return false
			}
		}
	}
	return true
}

// Create 为 id 建立目录，返回可写入内容的文件句柄。
func (s *Storage) Create(id string) (*os.File, error) {
	if !validID(id) {
		return nil, ErrInvalidID
	}
	dir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建文件目录失败: %w", err)
	}
	f, err := os.Create(filepath.Join(dir, "data"))
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	return f, nil
}

// SaveMeta 写入元数据。内容写完之后调用，保证 meta 存在的记录一定可下载。
func (s *Storage) SaveMeta(meta FileMeta) error {
	if !validID(meta.ID) {
		return ErrInvalidID
	}
	dir := filepath.Join(s.dir, meta.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建文件目录失败: %w", err)
	}
	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}
	tmp := filepath.Join(dir, metaFile+".tmp")
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("写入元数据失败: %w", err)
	}
	// 先写临时文件再改名，避免半截的 meta.json 被当成有效记录。
	if err := os.Rename(tmp, filepath.Join(dir, metaFile)); err != nil {
		return fmt.Errorf("提交元数据失败: %w", err)
	}
	return nil
}

// Load 读取指定 ID 的元数据。ID 非法返回 ErrInvalidID，不存在返回 ErrNotFound。
func (s *Storage) Load(id string) (FileMeta, error) {
	var meta FileMeta
	if !validID(id) {
		return meta, ErrInvalidID
	}
	buf, err := os.ReadFile(filepath.Join(s.dir, id, metaFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return meta, ErrNotFound
		}
		return meta, fmt.Errorf("读取元数据失败: %w", err)
	}
	if err := json.Unmarshal(buf, &meta); err != nil {
		return meta, fmt.Errorf("解析元数据失败: %w", err)
	}
	return meta, nil
}

// DataPath 返回指定 ID 的文件内容路径，同时确认它存在。
func (s *Storage) DataPath(id string) (string, error) {
	if !validID(id) {
		return "", ErrInvalidID
	}
	path := filepath.Join(s.dir, id, "data")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	return path, nil
}

// Remove 删除一个文件的目录，用于写入失败时回滚。
func (s *Storage) Remove(id string) error {
	if !validID(id) {
		return ErrInvalidID
	}
	return os.RemoveAll(filepath.Join(s.dir, id))
}

// Stats 是存储的汇总信息，用于首页展示。
type Stats struct {
	Files      int   // 未删除的文件数量
	TotalSize  int64 // 未删除文件的总字节数
	TrashFiles int   // 回收站里的文件数
	TrashSize  int64 // 回收站占用字节数
}

// FileEntry 是文件列表里的一项。
type FileEntry struct {
	ID         string
	Name       string
	Ext        string
	Size       int64
	SizeText   string
	ModTime    time.Time
	TimeText   string
	Deleted    bool
	DeletedTxt string
}

// scan 扫描存储目录，返回全部条目（含回收站）的统计与列表。
// meta.json 缺失或损坏时仍按磁盘内容列出，不会凭空丢文件。
func (s *Storage) scan() (Stats, []FileEntry) {
	var stats Stats
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return stats, nil
	}

	files := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		id := entry.Name()

		info, err := os.Stat(filepath.Join(s.dir, id, "data"))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		var meta FileMeta
		if data, err := os.ReadFile(filepath.Join(s.dir, id, metaFile)); err == nil {
			_ = json.Unmarshal(data, &meta)
		}

		name := meta.SafeName()
		mod := info.ModTime()
		if !meta.UploadedAt.IsZero() {
			mod = meta.UploadedAt.Local()
		}

		fe := FileEntry{
			ID:       id,
			Name:     name,
			Ext:      strings.TrimPrefix(strings.ToLower(meta.Extension), "."),
			Size:     info.Size(),
			SizeText: humanSize(info.Size()),
			ModTime:  mod,
			TimeText: formatTime(mod),
			Deleted:  meta.Deleted(),
		}

		if meta.Deleted() {
			stats.TrashFiles++
			stats.TrashSize += info.Size()
			fe.TimeText = formatTime(meta.DeletedAt.Local())
			fe.DeletedTxt = fe.TimeText
		} else {
			stats.Files++
			stats.TotalSize += info.Size()
		}

		files = append(files, fe)
	}

	// 未删除的按上传时间倒序；回收站按删除时间倒序（用 ModTime 近似即可，
	// 调用方按 Deleted 过滤后排序结果一致）。
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})

	return stats, files
}

// Stats 返回存储汇总信息。
func (s *Storage) Stats() Stats {
	stats, _ := s.scan()
	return stats
}

// ListResult 是分页查询的结果。
type ListResult struct {
	Files []FileEntry
	Total int // 该分类下的总条目数
	Page  int
	Size  int
	Pages int
}

// List 按分页返回文件列表。trash 为 true 时返回回收站内容。
// page 从 1 开始；size<=0 时默认 5 条。
func (s *Storage) List(trash bool, page, size int) ListResult {
	_, all := s.scan()

	matched := make([]FileEntry, 0, len(all))
	for _, f := range all {
		if f.Deleted == trash {
			matched = append(matched, f)
		}
	}

	if size <= 0 {
		size = 5
	}
	if page < 1 {
		page = 1
	}

	total := len(matched)
	pages := (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}

	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}

	return ListResult{
		Files: matched[start:end],
		Total: total,
		Page:  page,
		Size:  size,
		Pages: pages,
	}
}

// Delete 软删除：只打上删除时间标记，文件仍留在磁盘上。
func (s *Storage) Delete(id string) error {
	meta, err := s.Load(id)
	if err != nil {
		return err
	}
	if meta.Deleted() {
		return nil // 已在回收站，视为成功
	}
	now := time.Now().UTC()
	meta.DeletedAt = &now
	return s.SaveMeta(meta)
}

// DeleteMany 批量软删除，返回成功处理的条目数。
func (s *Storage) DeleteMany(ids []string) (int, error) {
	n := 0
	var firstErr error
	for _, id := range ids {
		if err := s.Delete(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// Restore 从回收站还原。
func (s *Storage) Restore(id string) error {
	meta, err := s.Load(id)
	if err != nil {
		return err
	}
	if !meta.Deleted() {
		return nil
	}
	meta.DeletedAt = nil
	return s.SaveMeta(meta)
}

// RestoreMany 批量还原。
func (s *Storage) RestoreMany(ids []string) (int, error) {
	n := 0
	var firstErr error
	for _, id := range ids {
		if err := s.Restore(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// Purge 彻底删除一个文件（从磁盘移除目录）。
func (s *Storage) Purge(id string) error {
	return s.Remove(id)
}

// PurgeMany 批量彻底删除。
func (s *Storage) PurgeMany(ids []string) (int, error) {
	n := 0
	var firstErr error
	for _, id := range ids {
		if err := s.Remove(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// PurgeAllTrash 清空回收站，返回删除的条目数。
func (s *Storage) PurgeAllTrash() (int, error) {
	_, files := s.scan()
	n := 0
	var firstErr error
	for _, f := range files {
		if !f.Deleted {
			continue
		}
		if err := s.Remove(f.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// formatTime 把时间格式化成「刚刚 / x 分钟前 / x 小时前 / 日期」。
func formatTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// cleanFileName 去掉用户文件名里的路径成分，只保留最后一段，
// 防止 "../../evil.txt" 之类的名字被写进元数据或响应头。
func cleanFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	// 控制字符会破坏 Content-Disposition 头。
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if len(name) > 200 {
		// 按扩展名截断，保留后缀。
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		keep := 200 - len(ext)
		if keep < 1 {
			keep = 1
		}
		if len(base) > keep {
			base = base[:keep]
		}
		name = base + ext
	}
	return name
}

// contentDisposition 生成同时兼容新旧浏览器的 Content-Disposition。
// filename 使用 ASCII 回退值，filename* 携带 UTF-8 原名（RFC 5987）。
func contentDisposition(name string) string {
	fallback := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			fallback = append(fallback, '_')
			continue
		}
		fallback = append(fallback, r)
	}
	ascii := string(fallback)
	if ascii == "" {
		ascii = "download"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(name))
}
