package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Chromium 先创建 DevToolsActivePort 再写内容：空文件 → 只有端口行 → 两行齐全。
// 读取方必须等到最后一步，否则会把中间态误判为「没有调试端口」（CI 上实测出现的 ERR no-port）。
func TestReadDevToolsPortWaitsForCompleteFile(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "Default")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "DevToolsActivePort")

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(file, nil, 0o644) // 1) 文件已存在但是空的
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(file, []byte("9222\n"), 0o644) // 2) 只有端口行
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(file, []byte("9222\n/devtools/browser/abc\n"), 0o644) // 3) 写完整
	}()

	port, ws := readDevToolsPort(dir, 5*time.Second)
	if port != "9222" || ws != "/devtools/browser/abc" {
		t.Fatalf("应等到文件写完整：port=%q ws=%q", port, ws)
	}
}

// 文件永远写不全（或根本不存在）时，到期返回空串，由调用方报 ERR no-port。
func TestReadDevToolsPortTimesOut(t *testing.T) {
	dir := t.TempDir()
	if port, ws := readDevToolsPort(dir, 200*time.Millisecond); port != "" || ws != "" {
		t.Fatalf("不存在文件应返回空：%q %q", port, ws)
	}
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("9222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if port, ws := readDevToolsPort(dir, 300*time.Millisecond); port != "" || ws != "" {
		t.Fatalf("只有端口行不应视为就绪：%q %q", port, ws)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Fatal("半成品文件应一直等到超时，而不是立刻返回")
	}
}
