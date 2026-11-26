package main

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	return s
}

// testTemplates 用与生产相同的模板集合，避免测试里另造一份。
func testTemplates(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("解析模板失败: %v", err)
	}
	return tpl
}

func TestValidID(t *testing.T) {
	if !validID("3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d") {
		t.Fatal("合法 UUID 被判为非法")
	}
	bad := []string{
		"", "abc", "../../etc/passwd",
		"3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0",
		"3f2b1c8a9d4e4a71b0c35e6f7a8b9c0d",
		"zzzzzzzz-9d4e-4a71-b0c3-5e6f7a8b9c0d",
		"3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d/../x",
	}
	for _, id := range bad {
		if validID(id) {
			t.Fatalf("非法 ID %q 被判为合法", id)
		}
	}
}

func TestCleanFileName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\me\a.txt`, "a.txt"},
		{"解压密码.txt", "解压密码.txt"},
		{"", ""},
		{"..", ""},
	}
	for _, tc := range tests {
		if got := cleanFileName(tc.in); got != tc.want {
			t.Fatalf("cleanFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestContentDisposition(t *testing.T) {
	got := contentDisposition("中文 报告.pdf")
	if !strings.Contains(got, `filename="`) {
		t.Fatalf("缺少 ASCII 回退名: %q", got)
	}
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("缺少 RFC 5987 文件名: %q", got)
	}
	if !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("不是 attachment: %q", got)
	}
	// 头值里不允许出现裸 CR/LF 或未转义引号
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("头值包含换行: %q", got)
	}
}

func TestStorageRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	id := "3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d"

	f, err := s.Create(id)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.WriteString("hello"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	f.Close()

	meta := FileMeta{ID: id, OriginalName: "报告.pdf", Extension: ".pdf", Size: 5}
	if err := s.SaveMeta(meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	got, err := s.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.OriginalName != "报告.pdf" || got.ID != id {
		t.Fatalf("Load 得到 %+v", got)
	}

	path, err := s.DataPath(id)
	if err != nil {
		t.Fatalf("DataPath: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("内容 = %q", body)
	}

	if err := s.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := s.Load(id); err != ErrNotFound {
		t.Fatalf("删除后应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("删除后目录仍存在: %v", err)
	}
}

func TestStorageNotFoundAndInvalid(t *testing.T) {
	s := newTestStorage(t)
	if _, err := s.Load("3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d"); err != ErrNotFound {
		t.Fatalf("缺失记录应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := s.Load("../etc/passwd"); err != ErrInvalidID {
		t.Fatalf("非法 ID 应返回 ErrInvalidID，实际 %v", err)
	}
	if _, err := s.DataPath("../../x"); err != ErrInvalidID {
		t.Fatalf("非法 ID 应返回 ErrInvalidID，实际 %v", err)
	}
}

func TestDownloadHandlerRejectsPrefixAndTraversal(t *testing.T) {
	s := newTestStorage(t)
	app := &App{store: s}

	cases := []string{
		"/download/abc",
		"/download/3f2b1c8a", // UUID 前缀不再命中
		"/download/3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d",
	}
	for _, path := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetPathValue("id", strings.TrimPrefix(path, "/download/"))
		app.downloadHandler(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 期望 400/404，实际 %d", path, rec.Code)
		}
	}
}
