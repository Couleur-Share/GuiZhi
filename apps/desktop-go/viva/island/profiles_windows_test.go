//go:build windows

package island

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// deadPID 起一个立刻退出的子进程并等它结束，返回它的进程号（此时系统里没有这个进程）。
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("起子进程失败：%v", err)
	}
	return cmd.ProcessState.Pid()
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("本进程应当被判为活着")
	}
	pid := deadPID(t)
	if processAlive(pid) {
		t.Errorf("已退出的子进程 %d 应当被判为已退出", pid)
	}
	// 系统进程（PID 4）打不开也读不到退出码，按活着处理：宁可少删
	if !processAlive(4) {
		t.Error("读不到信息的系统进程应当按活着处理")
	}
}

// 真实的进程判断串起来：已退出进程留下的目录删，本进程的看岛是否开着，活着的别的进程（父进程）的不动。
func TestSweepStaleProfilesWithRealProcessLiveness(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	SetProfileClass("GuiZhiSweepTestWin")
	t.Cleanup(func() { SetProfileClass(""); openCount.Store(0) })
	root, _ := profileRoot()
	eb := filepath.Join(root, "Proxy1", "EBWebView")

	gone := seedProfile(t, eb, "WV2Profile_viva-ephemeral-"+strconv.Itoa(deadPID(t))+"-1")
	own := seedProfile(t, eb, "WV2Profile_viva-ephemeral-"+strconv.Itoa(os.Getpid())+"-2")
	parent := seedProfile(t, eb, "WV2Profile_viva-ephemeral-"+strconv.Itoa(os.Getppid())+"-3")

	openCount.Store(1)
	res := sweepStaleProfiles()
	if exists(gone) || !exists(own) || !exists(parent) || res.Removed != 1 || res.Kept != 2 {
		t.Fatalf("有岛开着：gone=%v own=%v parent=%v %+v", exists(gone), exists(own), exists(parent), res)
	}
	openCount.Store(0)
	res = sweepStaleProfiles()
	if exists(own) || !exists(parent) || res.Removed != 1 || res.Kept != 1 {
		t.Fatalf("没有岛开着：own=%v parent=%v %+v", exists(own), exists(parent), res)
	}
}

// 删不掉的目录（浏览器进程还占着文件）：其余照删，失败带原因汇总并通知钩子，占用解除后下一次清掉。
func TestSweepReportsLockedProfileAndRetries(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	SetProfileClass("GuiZhiSweepTestLock")
	var reported error
	SetFailureHook(func(err error) { reported = err })
	t.Cleanup(func() { SetProfileClass(""); SetFailureHook(nil); openCount.Store(0) })
	root, _ := profileRoot()
	eb := filepath.Join(root, "Proxy1", "EBWebView")

	pid := deadPID(t)
	locked := seedProfile(t, eb, "WV2Profile_viva-ephemeral-"+strconv.Itoa(pid)+"-1")
	free := seedProfile(t, eb, "WV2Profile_viva-ephemeral-"+strconv.Itoa(pid)+"-2")
	// Go 的 os.Open 在 Windows 上不带 FILE_SHARE_DELETE：占着文件期间删除会失败，和浏览器进程占着文件同一种情形。
	hold, err := os.Open(filepath.Join(locked, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			hold.Close()
		}
	})

	res := sweepStaleProfiles()
	if exists(free) {
		t.Error("没被占用的目录应当照删")
	}
	if !exists(locked) {
		t.Fatal("被占用的目录不该凭空消失（测试前提不成立：占用没有阻止删除）")
	}
	if res.Removed != 1 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0], "WV2Profile_viva-ephemeral-"+strconv.Itoa(pid)+"-1") {
		t.Fatalf("结果 %+v", res)
	}
	if reported == nil || !strings.Contains(reported.Error(), "1 个") {
		t.Errorf("失败应当通知钩子，得到 %v", reported)
	}

	hold.Close()
	released = true
	reported = nil
	res = sweepStaleProfiles()
	if exists(locked) || res.Removed != 1 || len(res.Failed) != 0 {
		t.Fatalf("占用解除后应当清掉：exists=%v %+v", exists(locked), res)
	}
	if reported != nil {
		t.Errorf("没有失败时不该通知钩子，得到 %v", reported)
	}
}
