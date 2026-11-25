//go:build windows

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// tempLoginKeys 在 HKCU\Software\GuiZhiTest-<测试名与时间戳> 下建临时的 Run / Approved 子键路径，测试结束整棵删除。
// 绝不触碰真实的 Software\Microsoft\Windows\CurrentVersion\Run。
func tempLoginKeys(t *testing.T) (runSub, approvedSub string) {
	t.Helper()
	base := fmt.Sprintf(`Software\GuiZhiTest-%s-%d`, strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()), time.Now().UnixNano())
	t.Cleanup(func() {
		for _, sub := range []string{base + `\Run`, base + `\Approved`, base} {
			_ = registry.DeleteKey(registry.CURRENT_USER, sub)
		}
	})
	return base + `\Run`, base + `\Approved`
}

func readRegString(t *testing.T, sub, name string) (string, bool) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, sub, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return v, err == nil
}

func TestApplyRegistryLoginItemWritesAndRemoves(t *testing.T) {
	runSub, approvedSub := tempLoginKeys(t)
	const name = "GuiZhi"
	exe := `C:\Program Files\GuiZhi\GuiZhi.exe`

	// 关闭一个从没开过的登录项：键不存在也是成功
	if err := applyRegistryLoginItem(registry.CURRENT_USER, runSub, approvedSub, name, exe, false); err != nil {
		t.Fatalf("关闭不存在的登录项应成功：%v", err)
	}

	// 任务管理器曾把它禁用：StartupApproved 里有值
	ak, _, err := registry.CreateKey(registry.CURRENT_USER, approvedSub, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := ak.SetBinaryValue(name, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	ak.Close()

	if err := applyRegistryLoginItem(registry.CURRENT_USER, runSub, approvedSub, name, exe, true); err != nil {
		t.Fatalf("开启失败：%v", err)
	}
	got, ok := readRegString(t, runSub, name)
	if !ok || got != `"`+exe+`" --mygo-opened-at-login` {
		t.Fatalf("Run 值 = %q（存在=%v）", got, ok)
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, approvedSub, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if _, _, err := k.GetBinaryValue(name); err == nil {
			t.Error("重新开启应清掉任务管理器留下的禁用标记")
		}
	}

	// 再次开启：覆盖为新路径（升级后刷新到当前安装位置）
	exe2 := `D:\Apps\GuiZhi\GuiZhi.exe`
	if err := applyRegistryLoginItem(registry.CURRENT_USER, runSub, approvedSub, name, exe2, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := readRegString(t, runSub, name); got != `"`+exe2+`" --mygo-opened-at-login` {
		t.Fatalf("应刷新为新路径：%q", got)
	}

	// 关闭：删除 Run 值
	if err := applyRegistryLoginItem(registry.CURRENT_USER, runSub, approvedSub, name, exe2, false); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if _, ok := readRegString(t, runSub, name); ok {
		t.Fatal("关闭后 Run 值应已删除")
	}
	// 幂等
	if err := applyRegistryLoginItem(registry.CURRENT_USER, runSub, approvedSub, name, exe2, false); err != nil {
		t.Fatalf("重复关闭应成功：%v", err)
	}
}

// 生产键路径必须与旧版 MyGo 壳逐字一致：升级后旧登记原样生效，不会留下两条自启项。值名与旧版的应用名一致。
func TestLoginKeyPathsMatchLegacyShell(t *testing.T) {
	if runKeyPath != `Software\Microsoft\Windows\CurrentVersion\Run` ||
		approvedKeyPath != `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run` {
		t.Fatalf("键路径变了：%s | %s", runKeyPath, approvedKeyPath)
	}
	if webview2AppName != "GuiZhi" {
		t.Fatalf("值名 = %q", webview2AppName)
	}
}

// 读操作系统原始命令行：go test 的命令行里没有登录项参数；同时确认它能读到进程的真实参数（至少含 -test. 开头的参数）。
func TestOSCommandLineArgsReadsRealCommandLine(t *testing.T) {
	args := osCommandLineArgs()
	if wasOpenedAtLoginFromArgs(args) {
		t.Fatalf("测试进程不是登录项拉起：%v", args)
	}
	found := false
	for _, a := range args {
		if strings.HasPrefix(a, "-test.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应读到测试进程的真实命令行参数：%v", args)
	}
}
