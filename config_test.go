package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestStripJSONComments(t *testing.T) {
	in := []byte(`{
  // 行注释
  "a": "http://example.com//x", /* 块注释 */
  "b": 1 // 末尾注释
}`)
	out := string(stripJSONComments(in))
	if !contains(out, `"http://example.com//x"`) {
		t.Fatalf("字符串内的 // 被误删: %s", out)
	}
	if contains(out, "行注释") || contains(out, "块注释") || contains(out, "末尾注释") {
		t.Fatalf("注释未被移除: %s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestSplitAddr(t *testing.T) {
	tests := []struct {
		in       string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{"0.0.0.0:8081", "0.0.0.0", 8081, false},
		{"127.0.0.1:9999", "127.0.0.1", 9999, false},
		{"8081", "0.0.0.0", 8081, false},
		{"", "0.0.0.0", 8085, false},
		{"0.0.0.0:abc", "", 0, true},
		{"0.0.0.0:99999", "", 0, true},
	}
	for _, tc := range tests {
		h, p, err := splitAddr(tc.in)
		if (err != nil) != tc.wantErr {
			t.Fatalf("splitAddr(%q) err=%v", tc.in, err)
		}
		if !tc.wantErr && (h != tc.wantHost || p != tc.wantPort) {
			t.Fatalf("splitAddr(%q) = %s:%d, want %s:%d", tc.in, h, p, tc.wantHost, tc.wantPort)
		}
	}
}

func TestListenAutoPort(t *testing.T) {
	// 先占住一个端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	cfg := Config{ServerAddr: "127.0.0.1:" + portStr, AutoPort: true, PortAttempts: 5}
	l2, addr, err := listen(cfg, t.Logf)
	if err != nil {
		t.Fatalf("应自动切换到其他端口: %v", err)
	}
	defer l2.Close()

	_, gotPortStr, _ := net.SplitHostPort(addr)
	gotPort, _ := strconv.Atoi(gotPortStr)
	if gotPort == port {
		t.Fatalf("端口应变化，仍是 %d", gotPort)
	}
	if gotPort <= port {
		t.Fatalf("应向后顺延，得到 %d（原 %d）", gotPort, port)
	}
}

func TestListenNoAutoPortFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())

	cfg := Config{ServerAddr: "127.0.0.1:" + portStr, AutoPort: false}
	if _, _, err := listen(cfg, t.Logf); err == nil {
		t.Fatal("关闭自动切换时应因端口占用而失败")
	}
}

func TestBaseURLUsesLANForLoopback(t *testing.T) {
	app := &App{lanURL: "http://192.168.1.50:8081"}

	cases := []struct {
		host string
		want string
	}{
		{"localhost:8081", "http://192.168.1.50:8081"},
		{"127.0.0.1:8081", "http://192.168.1.50:8081"},
		{"192.168.1.50:8081", "http://192.168.1.50:8081"},
		{"10.0.0.5:8081", "http://10.0.0.5:8081"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
		req.Host = tc.host
		if got := app.baseURL(req); got != tc.want {
			t.Fatalf("baseURL(Host=%s) = %s, want %s", tc.host, got, tc.want)
		}
	}
}

func TestBaseURLKeepsPort(t *testing.T) {
	app := &App{} // 没有 lanURL，按请求推导
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "example.com:9000"
	if got := app.baseURL(req); got != "http://example.com:9000" {
		t.Fatalf("端口应保留，得到 %s", got)
	}
}

func TestBaseURLPublicOverride(t *testing.T) {
	app := &App{publicBaseURL: "https://share.example.com", lanURL: "http://192.168.1.50:8081"}
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "localhost:8081"
	if got := app.baseURL(req); got != "https://share.example.com" {
		t.Fatalf("显式配置应优先，得到 %s", got)
	}
}

func TestDetectLANIP(t *testing.T) {
	ip := detectLANIP()
	if ip == "" {
		t.Skip("环境没有可用的局域网地址")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		t.Fatalf("探测结果不是合法 IPv4: %q", ip)
	}
	if parsed.IsLoopback() {
		t.Fatalf("不应返回回环地址: %s", ip)
	}
	if !isPrivateIPv4(parsed) {
		t.Logf("注意：探测到非私网地址 %s", ip)
	}
}
