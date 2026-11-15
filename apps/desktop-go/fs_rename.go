package main

// 原子替换文件用的重命名。
// Windows 上目标文件刚被写过时，杀毒软件或索引服务会短暂占住它（打开时不带 FILE_SHARE_DELETE），
// 此时 os.Rename 覆盖会得到 ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION / ERROR_LOCK_VIOLATION。
// 这类占用通常几毫秒到几百毫秒内释放，所以对这三个错误退避重试（与 Go 工具链 internal/robustio 同一思路），
// 其余错误立即返回。实测：全量测试中设置保存偶发 "rename ai-models.json.tmp: Access is denied"（2026-11-15），
// 真实机器上等价于“保存 AI 配置偶发失败、重试就好”。
// Replace a file by rename, retrying the transient sharing errors Windows scanners cause.

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

const (
	errnoAccessDenied     syscall.Errno = 5  // ERROR_ACCESS_DENIED
	errnoSharingViolation syscall.Errno = 32 // ERROR_SHARING_VIOLATION
	errnoLockViolation    syscall.Errno = 33 // ERROR_LOCK_VIOLATION

	// 总重试预算：与 Go 工具链 robustio 的 arbitraryTimeout 一致。真正的权限问题（只读目标）也是 ACCESS_DENIED，
	// 最多多等这么久才报出原错误。
	renameRetryBudget = 2 * time.Second
)

// renameReplace 用 os.Rename 把 oldpath 原子替换到 newpath，对 Windows 的瞬时占用错误退避重试。
func renameReplace(oldpath, newpath string) error {
	return renameWithRetry(os.Rename, oldpath, newpath, renameRetryBudget)
}

// renameWithRetry 是 renameReplace 的可注入版本（测试用）：rename 返回瞬时占用错误时按 2ms 起、翻倍、封顶 100ms 重试，
// 直到成功、遇到其他错误或用尽 budget。
func renameWithRetry(rename func(oldpath, newpath string) error, oldpath, newpath string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	delay := 2 * time.Millisecond
	for {
		err := rename(oldpath, newpath)
		if err == nil || !isTransientSharingError(err) || time.Now().Add(delay).After(deadline) {
			return err
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
}

// isTransientSharingError 判断是不是 Windows 上“文件被别的句柄短暂占用”的错误码。其他系统的同号 errno 含义不同，不算。
func isTransientSharingError(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errnoAccessDenied || errno == errnoSharingViolation || errno == errnoLockViolation
}
