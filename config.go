package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// 默认值，可被 config.json 或环境变量覆盖。
const (
	defaultServerAddr   = "0.0.0.0:8085"
	defaultUploadDir    = "./uploads"
	defaultPortAttempts = 20
)

// Config 是服务配置。
type Config struct {
	ServerAddr    string `json:"server_addr"`     // 监听地址和端口
	UploadDir     string `json:"upload_dir"`      // 文件存储目录
	PublicBaseURL string `json:"public_base_url"` // 对外访问地址；留空则自动推导
	AutoPort      bool   `json:"auto_port"`       // 端口被占用时是否自动顺延
	PortAttempts  int    `json:"port_attempts"`   // 自动顺延的最大尝试次数
	OpenBrowser   bool   `json:"open_browser"`    // 启动后是否自动打开浏览器
}

// loadConfig 读取配置：先是默认值，再被 config.json 覆盖，最后被环境变量覆盖。
// config.json 允许 // 与 /* */ 注释。
func loadConfig() (Config, error) {
	cfg := Config{
		ServerAddr:   defaultServerAddr,
		UploadDir:    defaultUploadDir,
		AutoPort:     true,
		PortAttempts: defaultPortAttempts,
		OpenBrowser:  true,
	}

	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(stripJSONComments(data), &cfg); err != nil {
			return cfg, fmt.Errorf("解析 config.json 失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, fmt.Errorf("读取 config.json 失败: %w", err)
	}

	for key, dst := range map[string]*string{
		"SERVER_ADDR":     &cfg.ServerAddr,
		"UPLOAD_DIR":      &cfg.UploadDir,
		"PUBLIC_BASE_URL": &cfg.PublicBaseURL,
	} {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	// 布尔与数字环境变量单独处理，允许显式关掉自动端口。
	if v := os.Getenv("AUTO_PORT"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.AutoPort = b
		}
	}
	if v := os.Getenv("OPEN_BROWSER"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.OpenBrowser = b
		}
	}
	if v := os.Getenv("PORT_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PortAttempts = n
		}
	}

	if cfg.PortAttempts <= 0 {
		cfg.PortAttempts = defaultPortAttempts
	}
	return cfg, nil
}

// stripJSONComments 去掉 JSON 里的 // 行注释与 /* */ 块注释，
// 字符串字面量内部的内容原样保留。
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}

		// 行注释
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
			continue
		}

		// 块注释
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++ // 跳过结尾的 '/'
			continue
		}

		out = append(out, c)
	}

	return out
}

// splitAddr 把 "host:port" 拆开，缺省端口用 8085。
func splitAddr(addr string) (host string, port int, err error) {
	if addr == "" {
		addr = defaultServerAddr
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		// 允许只写端口，如 "8081"
		if n, convErr := strconv.Atoi(addr); convErr == nil {
			return "0.0.0.0", n, nil
		}
		return "", 0, fmt.Errorf("监听地址格式不正确 %q，应为 host:port", addr)
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 0 || n > 65535 {
		return "", 0, fmt.Errorf("端口无效: %q", p)
	}
	return h, n, nil
}

// listen 按配置绑定端口。端口被占用且开启 auto_port 时顺延到下一个可用端口。
// 返回的字符串是实际监听的 host:port。
func listen(cfg Config, logf func(format string, args ...any)) (net.Listener, string, error) {
	host, port, err := splitAddr(cfg.ServerAddr)
	if err != nil {
		return nil, "", err
	}

	attempts := 1
	if cfg.AutoPort {
		attempts = cfg.PortAttempts
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		cur := port + i
		if cur > 65535 {
			break
		}
		addr := net.JoinHostPort(host, strconv.Itoa(cur))
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			if i > 0 {
				logf("端口 %d 已被占用，改用 %d", port, cur)
			}
			return ln, addr, nil
		}
		lastErr = err
		if i == 0 && !cfg.AutoPort {
			return nil, "", fmt.Errorf("端口 %d 已被占用，且配置中关闭了自动切换（auto_port: false）: %w", cur, err)
		}
		logf("端口 %d 不可用：%v", cur, err)
	}

	return nil, "", fmt.Errorf("在 %d 个候选端口上都无法监听: %w", attempts, lastErr)
}

// loopbackHosts 是判定「本机访问」的地址集合。
func isLoopbackHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// virtualAdapterHints 用于在无法确定默认路由接口时，把虚拟网卡排在后面。
var virtualAdapterHints = []string{
	"vmware", "virtualbox", "vbox", "hyper-v", "vethernet",
	"loopback", "tap", "tun", "docker", "wsl", "bluetooth",
}

// detectLANIP 探测本机在局域网中的 IPv4 地址。
// 优先用「默认路由所在的接口」，这种方式不会真的发包；
// 探测不到（例如完全离线）时退化为枚举网卡并排除虚拟/回环地址。
func detectLANIP() string {
	// 1) 通过 UDP「连接」让系统选出出口网卡（不会产生真实流量）。
	if conn, err := net.Dial("udp4", "8.8.8.8:80"); err == nil {
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			conn.Close()
			if ip := addr.IP.To4(); ip != nil && !ip.IsLoopback() && isPrivateIPv4(ip) {
				return ip.String()
			}
		} else {
			conn.Close()
		}
	}

	// 2) 退化为枚举网卡。
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var best lanCandidate

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		isVirtual := false
		for _, hint := range virtualAdapterHints {
			if strings.Contains(name, hint) {
				isVirtual = true
				break
			}
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			cur := lanCandidate{ip: ip.String(), private: isPrivateIPv4(ip), virtual: isVirtual}
			if betterCandidate(cur, best) {
				best = cur
			}
		}
	}

	return best.ip
}

// lanCandidate 是网卡探测中的一个候选地址。
type lanCandidate struct {
	ip      string
	private bool
	virtual bool
}

// betterCandidate 优先真实网卡，其次私网地址。
func betterCandidate(cur, best lanCandidate) bool {
	if best.ip == "" {
		return true
	}
	if cur.virtual != best.virtual {
		return !cur.virtual
	}
	return cur.private && !best.private
}

// isPrivateIPv4 判断是否为 RFC1918 私网地址。
func isPrivateIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 10 ||
		(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
		(ip4[0] == 192 && ip4[1] == 168)
}
