// 测试替身：同一个可执行文件扮演 python.exe / venv 里的 python / 转写服务，行为由参数与环境变量决定。
// 仅用于 funasr_test.go，不做任何真实推理、联网或安装。
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(2)
	}
	// 通用探针（直接给 runFunasrToolProcess 用）
	switch args[0] {
	case "--sleep":
		time.Sleep(60 * time.Second)
		return
	case "--exit":
		code, _ := strconv.Atoi(args[1])
		if len(args) > 2 {
			fmt.Fprint(os.Stderr, strings.Join(args[2:], " "))
		}
		os.Exit(code)
	case "--lines":
		for _, l := range args[1:] {
			fmt.Println(l)
		}
		return
	case "--chunks": // 把一个中文字符的字节故意拆成两次写
		b := []byte("中")
		os.Stdout.Write(append([]byte("a"), b[:1]...))
		os.Stdout.Sync()
		time.Sleep(200 * time.Millisecond)
		os.Stdout.Write(append(b[1:], 'b', '\n'))
		return
	}
	if args[0] == "-I" {
		args = args[1:]
	}
	switch {
	case args[0] == "--version":
		fmt.Println("Python 3.12.13")
	case strings.HasSuffix(args[0], "server.py"):
		serve(args)
	case args[0] == "-m" && len(args) >= 3 && args[1] == "venv":
		makeVenv(args[2])
	case args[0] == "-m" && len(args) >= 3 && args[1] == "pip":
		pip(args[2:])
	case args[0] == "-c":
		code := ""
		if len(args) > 1 {
			code = args[1]
		}
		if strings.Contains(code, "importlib.metadata") {
			fmt.Println(envOr("FAKE_FUNASR_VERSION", "1.3.30"))
		} else if strings.Contains(code, "ensurepip") {
			os.WriteFile(filepath.Join(exeDir(), "pip-repaired"), nil, 0o644)
		}
	default:
		fmt.Fprintln(os.Stderr, "fakefunasr: 不认识的参数", args)
		os.Exit(2)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func exeDir() string {
	p, _ := os.Executable()
	return filepath.Dir(p)
}

// sitePackages 以 <env>/Scripts/python.exe 为基准定位 <env>/Lib/site-packages/funasr。
func funasrDir() string {
	return filepath.Join(exeDir(), "..", "Lib", "site-packages", "funasr")
}

func makeVenv(dir string) {
	self, _ := os.Executable()
	data, err := os.ReadFile(self)
	if err != nil {
		os.Exit(1)
	}
	os.MkdirAll(filepath.Join(dir, "Scripts"), 0o755)
	os.MkdirAll(filepath.Join(dir, "Lib", "site-packages"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "Scripts", "python.exe"), data, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pip(args []string) {
	switch args[0] {
	case "install":
		if len(args) > 1 && args[1] == "--help" {
			if os.Getenv("FAKE_PIP_BROKEN") == "1" {
				if _, err := os.Stat(filepath.Join(exeDir(), "pip-repaired")); err != nil {
					fmt.Fprintln(os.Stderr, "ModuleNotFoundError: cachecontrol")
					os.Exit(1)
				}
			}
			return
		}
		upgrade := false
		for _, a := range args {
			if a == "--upgrade" {
				upgrade = true
			}
		}
		if upgrade {
			// 更新路径：先「改动」环境，再按需失败，用来验证回滚
			os.MkdirAll(funasrDir(), 0o755)
			os.WriteFile(filepath.Join(funasrDir(), "MODIFIED"), []byte("x"), 0o644)
			switch os.Getenv("FAKE_FAIL") {
			case "update-pip":
				fmt.Fprintln(os.Stderr, "ERROR: No matching distribution found")
				os.Exit(1)
			case "update-pip-blockrollback":
				// 在备份目录里造出 failed-env，让回滚拒绝覆盖
				os.MkdirAll(filepath.Join(filepath.Dir(os.Getenv("TEMP")), "failed-env"), 0o755)
				fmt.Fprintln(os.Stderr, "ERROR: boom")
				os.Exit(1)
			}
			fmt.Println("Successfully installed funasr")
			return
		}
		if os.Getenv("FAKE_FAIL") == "install-pip" {
			fmt.Fprintln(os.Stderr, "ERROR: Could not find a version that satisfies the requirement torch")
			os.Exit(1)
		}
		if os.Getenv("FAKE_FAIL") != "install-nopkg" {
			os.MkdirAll(funasrDir(), 0o755)
			os.WriteFile(filepath.Join(funasrDir(), "__init__.py"), nil, 0o644)
		}
		fmt.Println("Collecting funasr")
		fmt.Println("Successfully installed funasr")
	case "show":
		fmt.Printf("Name: funasr\r\nVersion: %s\r\nSummary: fake\r\n", envOr("FAKE_FUNASR_VERSION", "1.3.29"))
	case "check":
		if os.Getenv("FAKE_FAIL") == "update-check" {
			fmt.Fprintln(os.Stderr, "funasr 1.3.30 has requirement x, which is not installed.")
			os.Exit(1)
		}
		fmt.Println("No broken requirements found.")
	}
}

func serve(args []string) {
	port := "8620"
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port = args[i+1]
		}
	}
	dir := filepath.Dir(args[0])
	f, _ := os.OpenFile(filepath.Join(dir, "server-starts.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if f != nil {
		fmt.Fprintf(f, "%d env=%s\n", os.Getpid(), os.Getenv("MODELSCOPE_CACHE"))
		f.Close()
	}
	switch os.Getenv("FAKE_FAIL") {
	case "boot-crash":
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):")
		fmt.Fprintln(os.Stderr, "100%|██████████| 1/1 [00:01<00:00,  1.28s/it]")
		fmt.Fprintln(os.Stderr, envOr("FAKE_BOOT_MSG", "RuntimeError: 模型加载失败"))
		os.Exit(1)
	case "boot-hang":
		time.Sleep(60 * time.Second)
		return
	}
	fmt.Println("[guizhi-asr] tick")
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bind:", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"sensevoice"}]}`)
	})
	http.Serve(ln, mux)
}
