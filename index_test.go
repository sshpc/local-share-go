package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQRHandlerRejectsUnknownID(t *testing.T) {
	s := newTestStorage(t)
	app := &App{store: s}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/qr/3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d", nil)
	req.SetPathValue("id", "3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d")
	app.qrHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知 ID 的二维码应为 404，实际 %d", rec.Code)
	}
}

func TestIndexRendersInfo(t *testing.T) {
	app := &App{
		store:  newTestStorage(t),
		tpl:    testTemplates(t),
		lanURL: "http://192.168.1.50:8081",
		lanIP:  "192.168.1.50",
		port:   8081,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app.indexHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("首页应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"手机电脑局域网互传文件",
		"192.168.1.50",
		"8081",
		"手机扫码即可互传",
		"/site-qr",
		"不限大小",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("首页缺少 %q", want)
		}
	}

	// 自动上传：不应再有需要手动点的上传提交按钮
	if strings.Contains(body, `<button type="submit"`) {
		t.Fatal("首页不应再包含手动提交按钮")
	}
	if !strings.Contains(body, "multiple") {
		t.Fatal("文件输入应支持多选")
	}
}

func TestIndexWithoutLAN(t *testing.T) {
	app := &App{store: newTestStorage(t), tpl: testTemplates(t), port: 9000}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app.indexHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("没有局域网地址时也应正常渲染，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "未探测到") {
		t.Fatal("应提示未探测到局域网地址")
	}
}

func TestSiteQRHandler(t *testing.T) {
	app := &App{tpl: testTemplates(t), lanURL: "http://192.168.1.50:8081"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/site-qr", nil)
	app.siteQRHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("站点二维码应返回 200，实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("二维码内容为空")
	}
}

func TestDownloadPageRenders(t *testing.T) {
	s := newTestStorage(t)
	id := "3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d"
	f, err := s.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("0123456789")
	f.Close()
	if err := s.SaveMeta(FileMeta{ID: id, OriginalName: "报告.pdf", Extension: ".pdf", Size: 10}); err != nil {
		t.Fatal(err)
	}

	app := &App{store: s, tpl: testTemplates(t)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/"+id, nil)
	req.SetPathValue("id", id)
	app.downloadPageHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("下载页应返回 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "报告.pdf") {
		t.Fatal("下载页缺少文件名")
	}
	if !strings.Contains(body, `data-size="10"`) {
		t.Fatalf("下载页缺少文件大小: %.300s", body)
	}
	if !strings.Contains(body, "/download/"+id) {
		t.Fatal("下载页缺少直链")
	}
	if !strings.Contains(body, ">下载<") {
		t.Fatal("下载页主按钮文案应为「下载」")
	}
}

func TestStorageStats(t *testing.T) {
	s := newTestStorage(t)
	if got := s.Stats(); got.Files != 0 || got.TotalSize != 0 {
		t.Fatalf("空目录统计应为 0，实际 %+v", got)
	}

	for i, id := range []string{
		"3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d",
		"4f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d",
	} {
		f, err := s.Create(id)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(strings.Repeat("x", 10*(i+1)))
		f.Close()
	}

	got := s.Stats()
	if got.Files != 2 {
		t.Fatalf("文件数 = %d, want 2", got.Files)
	}
	if got.TotalSize != 30 {
		t.Fatalf("总大小 = %d, want 30", got.TotalSize)
	}
}
