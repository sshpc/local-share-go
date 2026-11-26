package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 通过真实路由发请求，确保 PathValue 等由 mux 提供的语义也被覆盖。
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	app := &App{
		store: newTestStorage(t),
		tpl:   testTemplates(t),
	}
	srv := httptest.NewServer(app.routes())
	t.Cleanup(srv.Close)
	return srv
}

func TestUnknownPathIs404(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/anything/else", "/download", "/qr"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s 应为 404，实际 %d", path, resp.StatusCode)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/upload")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /upload 应为 405，实际 %d", resp.StatusCode)
	}
}
