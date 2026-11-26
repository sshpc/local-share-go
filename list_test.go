package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestStorageListEmpty(t *testing.T) {
	s := newTestStorage(t)
	if got := s.List(false, 1, 5); len(got.Files) != 0 || got.Total != 0 {
		t.Fatalf("空目录应返回空列表，实际 %+v", got)
	}
}

func seedFile(t *testing.T, s *Storage, id, name, content string, uploadedAt time.Time) {
	t.Helper()
	f, err := s.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = strings.ToLower(name[i:])
	}
	err = s.SaveMeta(FileMeta{
		ID:           id,
		OriginalName: name,
		Extension:    ext,
		Size:         int64(len(content)),
		UploadedAt:   uploadedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}

var testIDs = []string{
	"00000000-0000-4000-8000-000000000001",
	"00000000-0000-4000-8000-000000000002",
	"00000000-0000-4000-8000-000000000003",
	"00000000-0000-4000-8000-000000000004",
	"00000000-0000-4000-8000-000000000005",
	"00000000-0000-4000-8000-000000000006",
	"00000000-0000-4000-8000-000000000007",
}

func TestStorageListOrderAndFields(t *testing.T) {
	s := newTestStorage(t)
	now := time.Now().UTC()
	seedFile(t, s, testIDs[0], "旧文件.pdf", "aaa", now.Add(-2*time.Hour))
	seedFile(t, s, testIDs[1], "新文件.jpg", "bbbb", now)

	got := s.List(false, 1, 5)
	if len(got.Files) != 2 {
		t.Fatalf("应列出 2 个文件，实际 %d", len(got.Files))
	}
	// 最新的排最前
	if got.Files[0].Name != "新文件.jpg" {
		t.Fatalf("排序错误，首项 = %q", got.Files[0].Name)
	}
	if got.Files[0].Size != 4 || got.Files[0].SizeText != "4 B" {
		t.Fatalf("大小字段错误: %+v", got.Files[0])
	}
	if got.Files[0].Ext != "jpg" {
		t.Fatalf("扩展名 = %q, want jpg", got.Files[0].Ext)
	}
	if got.Files[1].Name != "旧文件.pdf" {
		t.Fatalf("第二项 = %q", got.Files[1].Name)
	}
}

func TestStoragePagination(t *testing.T) {
	s := newTestStorage(t)
	base := time.Now().UTC()
	for i := 0; i < 7; i++ {
		seedFile(t, s, testIDs[i], "f"+string(rune('a'+i))+".txt", "x", base.Add(time.Duration(i)*time.Minute))
	}

	// 默认 5 条
	page1 := s.List(false, 1, 5)
	if len(page1.Files) != 5 || page1.Total != 7 || page1.Pages != 2 {
		t.Fatalf("第1页应为 5 条/共7/2页，实际 len=%d total=%d pages=%d",
			len(page1.Files), page1.Total, page1.Pages)
	}
	page2 := s.List(false, 2, 5)
	if len(page2.Files) != 2 {
		t.Fatalf("第2页应为 2 条，实际 %d", len(page2.Files))
	}

	// 两页不重叠
	if page1.Files[0].ID == page2.Files[0].ID {
		t.Fatal("两页内容重叠")
	}

	// 超出范围的页码收敛到最后一页
	over := s.List(false, 99, 5)
	if over.Page != over.Pages {
		t.Fatalf("超范围页码应收敛，page=%d pages=%d", over.Page, over.Pages)
	}

	// 非法的页大小回落到默认
	if got := s.List(false, 1, 0); got.Size != 5 {
		t.Fatalf("size=0 应回落为 5，实际 %d", got.Size)
	}
}

func TestNormalizePageSize(t *testing.T) {
	cases := map[int]int{
		0: 5, 1: 5, 5: 5, 10: 10, 20: 20, 50: 50, 100: 100, 999: 5, -3: 5,
	}
	for in, want := range cases {
		if got := normalizePageSize(in); got != want {
			t.Fatalf("normalizePageSize(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestSoftDeleteAndRestore(t *testing.T) {
	s := newTestStorage(t)
	seedFile(t, s, testIDs[0], "报告.pdf", "hello", time.Now().UTC())

	if err := s.Delete(testIDs[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 活跃列表为空，回收站有一条
	if got := s.List(false, 1, 5); got.Total != 0 {
		t.Fatalf("删除后活跃列表应为空，实际 %d", got.Total)
	}
	if got := s.List(true, 1, 5); got.Total != 1 {
		t.Fatalf("回收站应有 1 条，实际 %d", got.Total)
	}

	stats := s.Stats()
	if stats.Files != 0 || stats.TrashFiles != 1 {
		t.Fatalf("统计不正确: %+v", stats)
	}

	// 文件仍在磁盘上（软删除）
	if _, err := s.DataPath(testIDs[0]); err != nil {
		t.Fatalf("软删除不应移除磁盘文件: %v", err)
	}

	// 还原
	if err := s.Restore(testIDs[0]); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := s.List(false, 1, 5); got.Total != 1 {
		t.Fatalf("还原后应有 1 条，实际 %d", got.Total)
	}
	if got := s.List(true, 1, 5); got.Total != 0 {
		t.Fatalf("还原后回收站应为空，实际 %d", got.Total)
	}
}

func TestDeleteManyAndPurgeAll(t *testing.T) {
	s := newTestStorage(t)
	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		seedFile(t, s, testIDs[i], "f"+string(rune('a'+i))+".txt", "x", base.Add(time.Duration(i)*time.Minute))
	}

	n, err := s.DeleteMany([]string{testIDs[0], testIDs[1]})
	if err != nil || n != 2 {
		t.Fatalf("批量删除应成功 2 个，实际 n=%d err=%v", n, err)
	}
	if got := s.List(true, 1, 5); got.Total != 2 {
		t.Fatalf("回收站应有 2 条，实际 %d", got.Total)
	}

	// 一键清空
	purged, err := s.PurgeAllTrash()
	if err != nil || purged != 2 {
		t.Fatalf("清空应删除 2 个，实际 n=%d err=%v", purged, err)
	}
	if got := s.List(true, 1, 5); got.Total != 0 {
		t.Fatalf("清空后回收站应为空，实际 %d", got.Total)
	}
	if got := s.List(false, 1, 5); got.Total != 1 {
		t.Fatalf("未删除的文件应保留，实际 %d", got.Total)
	}

	// 彻底删除后磁盘上也消失
	if _, err := s.DataPath(testIDs[0]); err != ErrNotFound {
		t.Fatalf("彻底删除后应找不到文件，实际 %v", err)
	}
}

func TestDownloadRejectsDeletedFile(t *testing.T) {
	s := newTestStorage(t)
	seedFile(t, s, testIDs[0], "报告.pdf", "hello", time.Now().UTC())
	app := &App{store: s, tpl: testTemplates(t)}

	// 未删除时可下载
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/download/"+testIDs[0], nil)
	req.SetPathValue("id", testIDs[0])
	app.downloadHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("未删除时应可下载，实际 %d", rec.Code)
	}

	if err := s.Delete(testIDs[0]); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/download/"+testIDs[0], nil)
	req.SetPathValue("id", testIDs[0])
	app.downloadHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("已删除的文件应拒绝下载，实际 %d", rec.Code)
	}
}

func TestDeleteHandlerBatch(t *testing.T) {
	s := newTestStorage(t)
	seedFile(t, s, testIDs[0], "a.txt", "x", time.Now().UTC())
	app := &App{store: s, tpl: testTemplates(t)}

	form := url.Values{}
	form.Add("id", testIDs[0])
	req := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.deleteHandler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("删除后应 303 重定向，实际 %d", rec.Code)
	}
	if got := s.List(true, 1, 5); got.Total != 1 {
		t.Fatalf("回收站应有 1 条，实际 %d", got.Total)
	}
}

func TestPurgeAllHandler(t *testing.T) {
	s := newTestStorage(t)
	seedFile(t, s, testIDs[0], "a.txt", "x", time.Now().UTC())
	_ = s.Delete(testIDs[0])
	app := &App{store: s, tpl: testTemplates(t)}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/purge-all", nil)
	app.purgeAllHandler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("清空后应 303，实际 %d", rec.Code)
	}
	if got := s.List(true, 1, 5); got.Total != 0 {
		t.Fatalf("清空后回收站应为空，实际 %d", got.Total)
	}
}

func TestIndexListsFiles(t *testing.T) {
	s := newTestStorage(t)
	seedFile(t, s, testIDs[0], "报告.pdf", "hello", time.Now().UTC())

	app := &App{store: s, tpl: testTemplates(t), port: 8081, lanURL: "http://192.168.1.5:8081"}
	rec := httptest.NewRecorder()
	app.indexHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("首页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"报告.pdf",
		"/download/" + testIDs[0],
		"手机电脑局域网互传文件",
		"回收站",
		"删除选中",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("首页缺少 %q", want)
		}
	}
	// 三步介绍已移除
	for _, gone := range []string{"双击运行 exe", "手机扫码打开本页", "上传或下载文件"} {
		if strings.Contains(body, gone) {
			t.Fatalf("首页不应再包含三步介绍 %q", gone)
		}
	}
}

func TestIndexTrashView(t *testing.T) {
	s := newTestStorage(t)
	seedFile(t, s, testIDs[0], "已删.pdf", "hello", time.Now().UTC())
	_ = s.Delete(testIDs[0])

	app := &App{store: s, tpl: testTemplates(t), port: 8081}
	rec := httptest.NewRecorder()
	app.indexHandler(rec, httptest.NewRequest(http.MethodGet, "/?view=trash", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "已删.pdf") {
		t.Fatal("回收站视图应显示已删除文件")
	}
	if !strings.Contains(body, "还原") {
		t.Fatal("回收站应提供还原按钮")
	}
	if !strings.Contains(body, "一键清空回收站") {
		t.Fatal("回收站应提供一键清空")
	}
}

func TestFormatTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "刚刚"},
		{now.Add(-5 * time.Minute), "5 分钟前"},
		{now.Add(-3 * time.Hour), "3 小时前"},
		{now.Add(-2 * 24 * time.Hour), "2 天前"},
	}
	for _, tc := range cases {
		if got := formatTime(tc.in); got != tc.want {
			t.Fatalf("formatTime(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPageRange(t *testing.T) {
	if got := pageRange(1, 3); len(got) != 3 {
		t.Fatalf("3 页应返回 3 个按钮，实际 %d", len(got))
	}
	if got := pageRange(10, 20); len(got) != 7 {
		t.Fatalf("多页时应返回 7 个按钮，实际 %d", len(got))
	}
}
