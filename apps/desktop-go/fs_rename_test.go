package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// 瞬时占用错误会被重试到成功；其他错误立即返回；用尽预算后返回最后一次的原错误。
func TestRenameWithRetry(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("重试只针对 Windows 的占用错误码")
	}
	linkErr := func(e syscall.Errno) error { return &os.LinkError{Op: "rename", Old: "a", New: "b", Err: e} }

	calls := 0
	err := renameWithRetry(func(string, string) error {
		calls++
		if calls < 4 {
			return linkErr(errnoAccessDenied)
		}
		return nil
	}, "a", "b", time.Second)
	if err != nil || calls != 4 {
		t.Fatalf("应在第 4 次成功：calls=%d err=%v", calls, err)
	}

	// 三种占用错误码都重试
	for _, e := range []syscall.Errno{errnoAccessDenied, errnoSharingViolation, errnoLockViolation} {
		calls = 0
		if err := renameWithRetry(func(string, string) error {
			calls++
			if calls == 1 {
				return linkErr(e)
			}
			return nil
		}, "a", "b", time.Second); err != nil || calls != 2 {
			t.Fatalf("errno %d 应重试一次：calls=%d err=%v", e, calls, err)
		}
	}

	// 其他错误（如目标目录不存在）不重试
	calls = 0
	other := linkErr(syscall.Errno(3)) // ERROR_PATH_NOT_FOUND
	if err := renameWithRetry(func(string, string) error { calls++; return other }, "a", "b", time.Second); err != other || calls != 1 {
		t.Fatalf("非占用错误应立即返回：calls=%d err=%v", calls, err)
	}

	// 一直被占用：预算用尽后返回原错误，且不会无限等
	calls = 0
	start := time.Now()
	err = renameWithRetry(func(string, string) error { calls++; return linkErr(errnoSharingViolation) }, "a", "b", 150*time.Millisecond)
	if !errors.Is(err, errnoSharingViolation) || calls < 2 {
		t.Fatalf("用尽预算应返回原错误：calls=%d err=%v", calls, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("预算 150ms 却等了 %v", d)
	}
}

// 真实占用：另一个句柄（Go 的 os.Open 不带 FILE_SHARE_DELETE）占住目标文件，稍后释放；
// renameReplace 应当等到释放后替换成功，内容是新的。
func TestRenameReplaceWaitsForHeldDestination(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("FILE_SHARE_DELETE 语义只在 Windows 上")
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "ai-models.json")
	src := dst + ".tmp"
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	// 前提自检：占用期间直接 os.Rename 确实失败（否则这个测试什么也没验证）。
	probe := dst + ".probe"
	if err := os.WriteFile(probe, []byte("p"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(probe, dst); err == nil {
		held.Close()
		t.Skip("本机上占用不阻止覆盖重命名，无法构造该场景")
	}
	time.AfterFunc(120*time.Millisecond, func() { held.Close() })
	if err := renameReplace(src, dst); err != nil {
		t.Fatalf("释放占用后应替换成功：%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Fatalf("目标内容=%q err=%v", got, err)
	}
}
