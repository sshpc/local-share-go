package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// MigrateLegacy 把旧版布局的扁平文件搬到新布局：
//
//	旧：uploads/<uuid><ext>
//	新：uploads/<uuid>/data + uploads/<uuid>/meta.json
//
// 旧版不保存原始文件名，因此迁移后文件名退化为 <uuid><ext>。
// 该函数是幂等的：没有旧文件时立即返回；已迁移的文件不会被重复处理。
func (s *Storage) MigrateLegacy() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}

	migrated := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			path := filepath.Join(s.dir, entry.Name())
			id, ext, ok := parseLegacyName(entry.Name())
			if !ok {
				continue
			}
			if err := s.migrateOne(path, id, ext); err != nil {
				log.Printf("迁移 %s 失败: %v", entry.Name(), err)
				continue
			}
			migrated++
		}
	}
	return migrated, nil
}

// parseLegacyName 从旧文件名里拆出 UUID 与扩展名。
func parseLegacyName(name string) (id, ext string, ok bool) {
	if len(name) < 36 {
		return "", "", false
	}
	id = name[:36]
	if !validID(id) {
		return "", "", false
	}
	ext = name[36:]
	// 旧版只保留扩展名，出现路径成分说明不是我们写的文件。
	if strings.ContainsAny(ext, `/\`) {
		return "", "", false
	}
	return id, ext, true
}

func (s *Storage) migrateOne(src, id, ext string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("不是普通文件")
	}

	dir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, "data")
	if _, err := os.Stat(dst); err == nil {
		// 目标已存在，说明迁移过；删掉旧的以免重复。
		return os.Remove(src)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}

	meta := FileMeta{
		ID:           id,
		OriginalName: id + ext,
		Extension:    strings.ToLower(ext),
		Size:         info.Size(),
		UploadedAt:   info.ModTime().UTC(),
	}
	if err := s.SaveMeta(meta); err != nil {
		return err
	}
	log.Printf("已迁移旧文件 %s%s (%.1f KB)", id, ext, float64(info.Size())/1024)
	return nil
}
