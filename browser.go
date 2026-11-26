package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser 用系统默认浏览器打开 url。
//
// 这是一次「尽力而为」的操作：没有图形界面、缺少 xdg-open 或被系统策略拦截时
// 只返回错误，不影响服务本身继续运行。
func openBrowser(url string) error {
	var (
		cmd  *exec.Cmd
		errs []error
	)

	switch runtime.GOOS {
	case "windows":
		// 用 rundll32 而不是 cmd /c start，避免窗口一闪而过和参数转义问题。
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		// Linux/BSD：优先 xdg-open，再退回几个常见实现。
		for _, name := range []string{"xdg-open", "gio", "sensible-browser", "x-www-browser"} {
			if _, err := exec.LookPath(name); err == nil {
				if name == "gio" {
					cmd = exec.Command("gio", "open", url)
				} else {
					cmd = exec.Command(name, url)
				}
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("未找到可用的浏览器启动命令（xdg-open 等）")
		}
	}

	if err := cmd.Start(); err != nil {
		e := fmt.Errorf("启动浏览器失败: %w", err)
		errs = append(errs, e)
		return e
	}
	// 不让子进程变成僵尸：异步回收。
	go func() { _ = cmd.Wait() }()

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
