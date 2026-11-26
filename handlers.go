package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
)

// itoa 是 strconv.Itoa 的短别名，模板数据里用得多。
func itoa(n int) string { return strconv.Itoa(n) }

// indexData 是首页的模板数据。
type indexData struct {
	Error     string
	LANURL    string // 局域网访问地址，用于展示与二维码
	LocalURL  string // 本机访问地址
	Port      int
	LANIP     string
	UploadDir string
	FileCount int
	TotalSize string
	TrashN    int
	TrashSize string
	OS        string
	Version   string

	List  ListResult // 当前分页的文件列表
	Trash ListResult // 回收站分页
	Page  int
	Size  int
	Sizes []int  // 可选的每页条数
	View  string // "files" 或 "trash"
	Flash string
	Pages []int // 分页按钮
}

// 分页大小允许的取值，防止被构造出超大页面。
var allowedPageSizes = []int{5, 10, 20, 50, 100}

const defaultPageSize = 5

// normalizePageSize 把任意输入收敛到允许的页大小。
func normalizePageSize(n int) int {
	for _, s := range allowedPageSizes {
		if s == n {
			return s
		}
	}
	return defaultPageSize
}

// resultData 是上传成功页的模板数据。
type resultData struct {
	FileID      string
	FileName    string
	FileSize    string
	SizeBytes   int64
	DownloadURL string
}

// indexHandler 返回首页：本机信息 + 二维码、上传区、文件列表（可分页、可删除）、回收站。
// 手机扫码打开的是同一个页面，因此上传和下载在这里都能完成。
func (a *App) indexHandler(w http.ResponseWriter, r *http.Request) {
	// 精确匹配 "/"，其余未注册路径返回 404 而不是上传页。
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	size := normalizePageSize(atoiDefault(q.Get("size"), defaultPageSize))
	page := atoiDefault(q.Get("page"), 1)
	view := q.Get("view")
	if view != "trash" {
		view = "files"
	}

	stats, _ := a.store.scan()
	list := a.store.List(false, page, size)
	trash := a.store.List(true, 1, size)

	data := indexData{
		LANURL:    a.lanURL,
		LocalURL:  "http://localhost:" + itoa(a.port),
		Port:      a.port,
		LANIP:     a.lanIP,
		UploadDir: a.store.Dir(),
		FileCount: stats.Files,
		TotalSize: humanSize(stats.TotalSize),
		TrashN:    stats.TrashFiles,
		TrashSize: humanSize(stats.TrashSize),
		OS:        runtime.GOOS,
		Version:   version,
		List:      list,
		Trash:     trash,
		Page:      page,
		Size:      size,
		Sizes:     allowedPageSizes,
		View:      view,
		Flash:     q.Get("msg"),
		Pages:     pageRange(list.Page, list.Pages),
	}
	if msg := q.Get("error"); msg != "" {
		data.Error = msg
	}
	a.render(w, http.StatusOK, "index", data)
}

// pageRange 生成分页按钮的页码，最多 7 个，超出时省略两端。
func pageRange(cur, total int) []int {
	if total <= 7 {
		out := make([]int, 0, total)
		for i := 1; i <= total; i++ {
			out = append(out, i)
		}
		return out
	}
	start := cur - 3
	if start < 1 {
		start = 1
	}
	end := start + 6
	if end > total {
		end = total
		start = end - 6
	}
	out := make([]int, 0, 7)
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}

// atoiDefault 解析整数，失败或为空时用默认值。
func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// redirectHome 回到列表页，带上提示信息。
func (a *App) redirectHome(w http.ResponseWriter, r *http.Request, msg string) {
	q := r.URL.Query()
	params := make([]string, 0, 4)
	target := "/?"
	if v := q.Get("view"); v == "trash" {
		params = append(params, "view=trash")
	}
	if v := q.Get("size"); v != "" {
		params = append(params, "size="+v)
	}
	if v := q.Get("page"); v != "" {
		params = append(params, "page="+v)
	}
	if msg != "" {
		params = append(params, "msg="+urlQueryEscape(msg))
	}
	if len(params) > 0 {
		target = "/?" + strings.Join(params, "&")
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// urlQueryEscape 只做最小的转义，避免引入额外依赖。
func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

// collectIDs 从表单里收集 id（支持复选框多选）与单个 id 字段。
func collectIDs(r *http.Request) []string {
	if err := r.ParseForm(); err != nil {
		return nil
	}
	ids := r.Form["id"]
	// 去重，保持顺序
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// deleteHandler 软删除选中文件（进入回收站）。支持批量。
func (a *App) deleteHandler(w http.ResponseWriter, r *http.Request) {
	ids := collectIDs(r)
	if len(ids) == 0 {
		a.redirectHome(w, r, "没有选中文件")
		return
	}
	n, err := a.store.DeleteMany(ids)
	if err != nil && n == 0 {
		a.redirectHome(w, r, "删除失败："+err.Error())
		return
	}
	a.redirectHome(w, r, fmt.Sprintf("已移入回收站 %d 个文件", n))
}

// restoreHandler 从回收站还原选中文件。支持批量。
func (a *App) restoreHandler(w http.ResponseWriter, r *http.Request) {
	ids := collectIDs(r)
	if len(ids) == 0 {
		a.redirectHome(w, r, "没有选中文件")
		return
	}
	n, err := a.store.RestoreMany(ids)
	if err != nil && n == 0 {
		a.redirectHome(w, r, "还原失败："+err.Error())
		return
	}
	a.redirectHome(w, r, fmt.Sprintf("已还原 %d 个文件", n))
}

// purgeHandler 彻底删除选中文件。支持批量。
func (a *App) purgeHandler(w http.ResponseWriter, r *http.Request) {
	ids := collectIDs(r)
	if len(ids) == 0 {
		a.redirectHome(w, r, "没有选中文件")
		return
	}
	n, err := a.store.PurgeMany(ids)
	if err != nil && n == 0 {
		a.redirectHome(w, r, "彻底删除失败："+err.Error())
		return
	}
	a.redirectHome(w, r, fmt.Sprintf("已彻底删除 %d 个文件", n))
}

// purgeAllHandler 一键清空回收站。
func (a *App) purgeAllHandler(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.PurgeAllTrash()
	if err != nil {
		a.redirectHome(w, r, "清空失败："+err.Error())
		return
	}
	a.redirectHome(w, r, fmt.Sprintf("已清空回收站，删除 %d 个文件", n))
}

// siteQRHandler 生成指向本站点（局域网地址）的二维码，
// 手机扫码即可打开首页，从手机端上传文件。
func (a *App) siteQRHandler(w http.ResponseWriter, r *http.Request) {
	target := a.lanURL
	if target == "" {
		target = a.baseURL(r)
	}

	png, err := qrcode.Encode(target, qrcode.Medium, 256)
	if err != nil {
		log.Printf("生成站点二维码失败: %v", err)
		http.Error(w, "生成二维码失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(png)))
	_, _ = w.Write(png)
}

// resultHandler 展示上传结果。上传成功后重定向到这里，
// 避免刷新页面时重复提交同一个文件。
func (a *App) resultHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := a.store.Load(id)
	if err != nil {
		a.handleStoreError(w, err)
		return
	}
	a.render(w, http.StatusOK, "result", resultData{
		FileID:      meta.ID,
		FileName:    meta.SafeName(),
		FileSize:    humanSize(meta.Size),
		SizeBytes:   meta.Size,
		DownloadURL: a.downloadURL(r, meta.ID),
	})
}

// uploadHandler 接收文件、落盘、记录元数据，然后重定向到结果页。
// 不限制文件大小：磁盘能写多少就接收多少。
func (a *App) uploadHandler(w http.ResponseWriter, r *http.Request) {
	// 只在内存里缓冲 8MB，超出部分由标准库落到临时文件，
	// 完成后再由我们把内容复制到存储目录。
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, fmt.Sprintf("解析上传内容失败：%v", err), http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		msg := "未选择文件"
		if !errors.Is(err, http.ErrMissingFile) {
			msg = fmt.Sprintf("获取文件失败：%v", err)
		}
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	defer file.Close()

	if header.Filename == "" {
		http.Error(w, "文件名不能为空", http.StatusBadRequest)
		return
	}

	id := newFileID()
	out, err := a.store.Create(id)
	if err != nil {
		log.Printf("创建文件失败: %v", err)
		http.Error(w, "保存文件失败", http.StatusInternalServerError)
		return
	}

	written, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = a.store.Remove(id) // 写入失败时清理半截目录
		log.Printf("写入文件失败: %v %v", copyErr, closeErr)
		http.Error(w, "保存文件失败", http.StatusInternalServerError)
		return
	}

	if written == 0 {
		_ = a.store.Remove(id)
		http.Error(w, "文件内容为空", http.StatusBadRequest)
		return
	}

	name := cleanFileName(header.Filename)
	meta := FileMeta{
		ID:           id,
		OriginalName: name,
		Extension:    strings.ToLower(filepath.Ext(name)),
		Size:         written,
		ContentType:  header.Header.Get("Content-Type"),
		UploadedAt:   time.Now().UTC(),
	}
	if err := a.store.SaveMeta(meta); err != nil {
		_ = a.store.Remove(id)
		log.Printf("保存元数据失败: %v", err)
		http.Error(w, "保存文件失败", http.StatusInternalServerError)
		return
	}

	log.Printf("已保存文件 %s (%s, %s)", meta.ID, meta.SafeName(), humanSize(meta.Size))

	// PRG：重定向到结果页，刷新不会重复上传。
	http.Redirect(w, r, "/r/"+id, http.StatusSeeOther)
}

// downloadHandler 按 ID 精确下载文件。
// 直接返回文件流，浏览器自带的下载管理器会显示进度；
// 需要页面内的进度条时走 /d/{id}。
func (a *App) downloadHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := a.store.Load(id)
	if err != nil {
		a.handleStoreError(w, err)
		return
	}
	// 回收站里的文件不可下载，但仍可通过还原取回。
	if meta.Deleted() {
		http.Error(w, "文件已被删除（在回收站中）", http.StatusNotFound)
		return
	}
	path, err := a.store.DataPath(id)
	if err != nil {
		a.handleStoreError(w, err)
		return
	}

	w.Header().Set("Content-Disposition", contentDisposition(meta.SafeName()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 统一按二进制下载，避免浏览器直接渲染上传的内容。
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

// downloadPageData 是下载页的模板数据。
type downloadPageData struct {
	FileID    string
	FileName  string
	FileSize  string
	SizeBytes int64
	RawURL    string
}

// downloadPageHandler 返回带进度条的下载页。
func (a *App) downloadPageHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := a.store.Load(id)
	if err != nil {
		a.handleStoreError(w, err)
		return
	}
	if meta.Deleted() {
		a.renderError(w, http.StatusNotFound, "文件已删除",
			"这个文件已被移到回收站，暂时无法下载。可以在首页的回收站里还原后再试。")
		return
	}
	a.render(w, http.StatusOK, "download", downloadPageData{
		FileID:    meta.ID,
		FileName:  meta.SafeName(),
		FileSize:  humanSize(meta.Size),
		SizeBytes: meta.Size,
		RawURL:    "/download/" + meta.ID,
	})
}

// qrHandler 为已存在的文件生成下载二维码。
func (a *App) qrHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := a.store.Load(id)
	if err != nil {
		a.handleStoreError(w, err)
		return
	}
	if meta.Deleted() {
		http.Error(w, "文件已被删除", http.StatusNotFound)
		return
	}

	png, err := qrcode.Encode(a.downloadURL(r, id), qrcode.Medium, 256)
	if err != nil {
		log.Printf("生成二维码失败: %v", err)
		http.Error(w, "生成二维码失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Length", fmt.Sprint(len(png)))
	_, _ = w.Write(png)
}

// handleStoreError 把存储层错误映射成合适的 HTTP 状态码。
func (a *App) handleStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidID):
		http.Error(w, "文件 ID 格式非法", http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "文件不存在", http.StatusNotFound)
	default:
		log.Printf("存储错误: %v", err)
		http.Error(w, "读取文件失败", http.StatusInternalServerError)
	}
}

// errorData 是错误页的模板数据。
type errorData struct {
	Title   string
	Message string
}

// renderError 渲染一个友好的错误页，而不是纯文本。
func (a *App) renderError(w http.ResponseWriter, status int, title, message string) {
	var buf bytes.Buffer
	if err := a.tpl.ExecuteTemplate(&buf, "error", errorData{Title: title, Message: message}); err != nil {
		http.Error(w, title, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// newFileID 生成文件 ID。uuid 生成失败时退化为随机十六进制，不返回空 ID。
func newFileID() string {
	id, err := uuid.NewRandom()
	if err == nil {
		return id.String()
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 极端情况下仍要保证目录不冲突。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return uuid.UUID(buf).String()
}
