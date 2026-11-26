package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacy(t *testing.T) {
	s := newTestStorage(t)
	legacyID := "3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d"
	if err := os.WriteFile(filepath.Join(s.Dir(), legacyID+".pdf"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 非旧格式文件应当被忽略
	if err := os.WriteFile(filepath.Join(s.Dir(), "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "not-a-uuid.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := s.MigrateLegacy()
	if err != nil {
		t.Fatalf("MigrateLegacy: %v", err)
	}
	if n != 1 {
		t.Fatalf("应迁移 1 个文件，实际 %d", n)
	}

	meta, err := s.Load(legacyID)
	if err != nil {
		t.Fatalf("迁移后应能 Load: %v", err)
	}
	if meta.Extension != ".pdf" || meta.Size != 3 {
		t.Fatalf("元数据不正确: %+v", meta)
	}
	path, err := s.DataPath(legacyID)
	if err != nil {
		t.Fatalf("迁移后应有内容文件: %v", err)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "old" {
		t.Fatalf("内容 = %q", body)
	}

	// 幂等：再跑一次不应该再迁移
	n, err = s.MigrateLegacy()
	if err != nil || n != 0 {
		t.Fatalf("第二次迁移应无操作，n=%d err=%v", n, err)
	}

	// 无关文件保留
	if _, err := os.Stat(filepath.Join(s.Dir(), "notes.txt")); err != nil {
		t.Fatalf("无关文件被误删: %v", err)
	}
}
