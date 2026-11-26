package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

// version 由发布流水线通过 -ldflags "-X main.version=v1.0.0" 注入；
// 本地 go build 则为 dev。
var version = "dev"

// App 持有服务的全部依赖。
type App struct {
	store         *Storage
	tpl           *template.Template
	publicBaseURL string // 对外地址前缀；为空时按请求推导
	lanURL        string // 局域网访问地址，如 http://192.168.1.10:8081
	lanIP         string // 探测到的局域网 IP
	port          int    // 实际监听端口
}

// humanSize 把字节数格式化成便于展示的字符串。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// templateFuncs 是模板里用到的辅助函数（html/template 不内置算术）。
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}
}

// parseTemplates 解析内嵌模板。辅助函数必须在解析前注册，
// 否则模板里的 {{sub .Page 1}} 会在 ParseFS 阶段就报「function not defined」。
func parseTemplates() (*template.Template, error) {
	return template.New("root").Funcs(templateFuncs()).ParseFS(templateFS, "templates/*.html")
}

// newApp 构造应用，启动阶段完成模板解析等一次性准备工作。
func newApp(cfg Config) (*App, error) {
	store, err := NewStorage(cfg.UploadDir)
	if err != nil {
		return nil, err
	}

	tpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("解析模板失败: %w", err)
	}

	// 把旧版扁平布局（uploads/<uuid><ext>）迁移到新布局，幂等。
	if n, err := store.MigrateLegacy(); err != nil {
		log.Printf("检查旧文件迁移时出错: %v", err)
	} else if n > 0 {
		log.Printf("已迁移 %d 个旧格式文件到新布局", n)
	}

	return &App{
		store:         store,
		tpl:           tpl,
		publicBaseURL: strings.TrimSuffix(cfg.PublicBaseURL, "/"),
	}, nil
}

// routes 注册路由。使用 Go 1.22+ 的方法与通配符语法，
// 未匹配的路径自动 404，方法不匹配自动 405。
func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.indexHandler)
	mux.HandleFunc("POST /upload", a.uploadHandler)
	mux.HandleFunc("GET /r/{id}", a.resultHandler)
	mux.HandleFunc("GET /d/{id}", a.downloadPageHandler)
	mux.HandleFunc("GET /download/{id}", a.downloadHandler)
	mux.HandleFunc("GET /qr/{id}", a.qrHandler)
	mux.HandleFunc("GET /site-qr", a.siteQRHandler)

	// 文件管理：软删除进回收站、还原、彻底删除、清空回收站。
	mux.HandleFunc("POST /delete", a.deleteHandler)
	mux.HandleFunc("POST /restore", a.restoreHandler)
	mux.HandleFunc("POST /purge", a.purgeHandler)
	mux.HandleFunc("POST /purge-all", a.purgeAllHandler)

	// 进程内的健康检查，便于部署时探活。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return a.recoverer(a.logger(mux))
}

// baseURL 决定用于拼接下载链接的对外地址。
//
// 优先级：
//  1. 显式配置的 public_base_url；
//  2. 通过 localhost 打开页面时，改用探测到的局域网地址，
//     否则手机扫码拿到 localhost 会打不开；
//  3. 其余情况按请求的 Host 推导（局域网 IP 访问时天然正确）。
func (a *App) baseURL(r *http.Request) string {
	if a.publicBaseURL != "" {
		return a.publicBaseURL
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	// 判断是否为「本机访问」时只看主机名，但拼链接要保留原始 Host（含端口）。
	hostOnly := r.Host
	if h, _, err := net.SplitHostPort(hostOnly); err == nil {
		hostOnly = h
	}

	if a.lanURL != "" && isLoopbackHost(hostOnly) {
		return a.lanURL
	}

	if r.Host == "" {
		return scheme + "://localhost"
	}
	return scheme + "://" + r.Host
}

func (a *App) downloadURL(r *http.Request, id string) string {
	return fmt.Sprintf("%s/download/%s", a.baseURL(r), id)
}

// statusRecorder 记录响应状态码与字节数，供日志使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// logger 输出结构化的单行访问日志。
func (a *App) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		log.Printf("%s %s %d %dB %s %s",
			r.Method, r.URL.Path, rec.status, rec.bytes,
			time.Since(start).Round(time.Millisecond), clientIP(r))
	})
}

// recoverer 捕获处理器里的 panic，避免整个进程退出。
func (a *App) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v (%s %s)", rec, r.Method, r.URL.Path)
				http.Error(w, "服务器内部错误", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// render 执行模板并把结果先写入缓冲区，
// 这样模板出错时还能返回 500 而不是半截 HTML。
func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := a.tpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("渲染模板 %s 失败: %v", name, err)
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func main() {
	log.SetFlags(log.LstdFlags)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	app, err := newApp(cfg)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	ln, addr, err := listen(cfg, log.Printf)
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	app.port = port

	// 探测局域网地址，用于控制台提示、首页展示和 localhost 访问时的链接替换。
	if cfg.PublicBaseURL == "" {
		if ip := detectLANIP(); ip != "" {
			app.lanIP = ip
			app.lanURL = fmt.Sprintf("http://%s", net.JoinHostPort(ip, portStr))
		}
	}

	srv := &http.Server{
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0, // 上传大文件耗时不定，不设整体读超时
		WriteTimeout:      0, // 下载同理；改用 IdleTimeout 清理空闲连接
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// 收到中断信号后优雅关闭，等待在途请求完成。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	log.Printf("服务已启动，监听 %s（版本 %s）", addr, version)
	log.Printf("  本机访问:   http://localhost:%d", port)
	if app.lanURL != "" {
		log.Printf("  局域网访问: %s", app.lanURL)
		log.Printf("  （手机连同一个 Wi-Fi 后用上面的局域网地址，或直接扫首页二维码）")
	} else {
		log.Printf("  局域网访问: 未探测到可用的局域网 IPv4 地址")
	}
	log.Printf("  上传目录:   %s", app.store.Dir())
	if cfg.PublicBaseURL != "" {
		log.Printf("  链接前缀:   %s（config 中已固定）", app.publicBaseURL)
	} else {
		log.Printf("  链接前缀:   按访问来源自动适配")
	}

	// 服务已经起来后再开浏览器，避免页面先于服务加载失败。
	if cfg.OpenBrowser {
		go func() {
			// 留一点时间让 Serve 进入监听循环。
			time.Sleep(150 * time.Millisecond)
			url := fmt.Sprintf("http://localhost:%d", port)
			if err := openBrowser(url); err != nil {
				log.Printf("自动打开浏览器失败（可手动访问 %s）: %v", url, err)
			} else {
				log.Printf("已自动打开浏览器: %s", url)
			}
		}()
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("服务异常退出: %v", err)
		}
	case <-ctx.Done():
		log.Println("正在关闭服务…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("优雅关闭失败: %v", err)
		}
		log.Println("服务已停止")
	}
}
