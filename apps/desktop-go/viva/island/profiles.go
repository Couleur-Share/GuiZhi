package island

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// 岛的临时配置目录清理（计划 §10.5 的 R5）。
//
// Viva v0.6.4 给每个 Ephemeral 页面起一个 WebView2 profile 名 "viva-ephemeral-<进程号>-<序号>"
// （internal/platform/windows/web_windows.go），WebView2 因此在
// <os.UserCacheDir()>\<窗口 Class>\WebView2\<代理槽>\EBWebView 下留一个 "WV2Profile_<名字>" 目录：
// 只有元数据（实测约 0.8 MB、109 个文件；cookie、存储与缓存都在内存里），但页面关闭、进程退出之后
// 都不会被删，每开一个岛就多一个（探针 islandprobe 复测：开 5 个岛留下 5 个）。
// 所以必须由应用侧清理：在每次开岛之前扫一遍。

// ephemeralProfilePrefix 是 Viva 的 Ephemeral 页面在 EBWebView 下留下的目录名前缀。
const ephemeralProfilePrefix = "WV2Profile_viva-ephemeral-"

// profileClass 是 Viva 窗口的 Class（决定 WebView2 用户数据根目录）；空表示还没设置，此时不清理。
var profileClass atomic.Value // string

// openCount 是本进程里开着的岛数：为 0 时本进程以前留下的目录都已无人使用，可以删。
var openCount atomic.Int32

// failureHook 在清理有失败时收到汇总错误（交给应用写日志）；nil 表示不通知。
var failureHook atomic.Pointer[func(error)]

// SetProfileClass 登记 Viva 窗口的 Class，岛的清理据此找到 WebView2 的用户数据根目录。
// 应用启动时调用一次（viva/app.Run）；空串等于关闭清理。
func SetProfileClass(class string) { profileClass.Store(class) }

// SetFailureHook 登记清理失败的接收方（nil 取消）。只在有目录删不掉时调用，不在成功时调用。
func SetFailureHook(fn func(error)) {
	if fn == nil {
		failureHook.Store(nil)
		return
	}
	failureHook.Store(&fn)
}

// profileRoot 返回 Viva 放 WebView2 用户数据的根目录。Viva 不读 WEBVIEW2_USER_DATA_FOLDER，
// 路径规则与它的 userDataFolder 一致：缓存目录（取不到时退回临时目录）\<Class>\WebView2。
func profileRoot() (string, bool) {
	class, _ := profileClass.Load().(string)
	if class == "" {
		return "", false
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, class, "WebView2"), true
}

// SweepResult 是一次清理的结果。
type SweepResult struct {
	// Removed 是删掉的目录数；Kept 是因为还在用而保留的数目（别的实例的、或本进程还开着岛时自己的）。
	Removed, Kept int
	// Failed 是没删掉的目录与原因（常见：浏览器进程还没释放文件，下次开岛前会重试）。
	Failed []string
}

// Err 把失败汇总成一个错误；没有失败时返回 nil。
func (r SweepResult) Err() error {
	if len(r.Failed) == 0 {
		return nil
	}
	return fmt.Errorf("island: 有 %d 个岛的临时配置目录没能删除（下次开岛前重试）：%s", len(r.Failed), strings.Join(r.Failed, "；"))
}

// parseEphemeralProfile 从目录名里取出创建它的进程号与序号；不是 Viva 的临时配置目录时 ok 为 false。
func parseEphemeralProfile(name string) (pid, seq int, ok bool) {
	rest, found := strings.CutPrefix(name, ephemeralProfilePrefix)
	if !found {
		return 0, 0, false
	}
	pidText, seqText, found := strings.Cut(rest, "-")
	if !found {
		return 0, 0, false
	}
	pid, ok1 := parsePositive(pidText)
	seq, ok2 := parsePositive(seqText)
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return pid, seq, true
}

// parsePositive 只接受十进制数字组成的正整数（不收正负号、空白与别的字符）。
func parsePositive(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}

// ebWebViewDirs 列出 root 下可能放 profile 的 EBWebView 目录：每个代理槽一个（Proxy1、Proxy2…），
// 外加没有代理时的 root\EBWebView。只做目录读取，不展开通配符（用户名里可能有 [ 之类）。
func ebWebViewDirs(root string) []string {
	dirs := []string{filepath.Join(root, "EBWebView")}
	entries, err := os.ReadDir(root)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "EBWebView" {
			dirs = append(dirs, filepath.Join(root, e.Name(), "EBWebView"))
		}
	}
	return dirs
}

// sweepEphemeralProfiles 删除 root 下 Viva 留下的临时配置目录，规则：
//   - 创建它的进程已不存在（别的实例崩溃或被强杀）→ 删；
//   - 创建者是本进程（self）→ 只有本进程没有岛开着（ownInUse 为 false）才删，因为序号是 Viva 内部的，
//     认不出哪个目录属于哪个已开的岛；
//   - 创建者是另一个活着的进程 → 保留（那是另一个实例的岛）。
//
// 只处理名字严格符合 WV2Profile_viva-ephemeral-<正整数>-<正整数> 的目录，别的东西（Default、缓存、
// 其他程序的 profile）一概不碰。alive 判断进程是否存在，由调用方注入以便测试。
func sweepEphemeralProfiles(root string, self int, alive func(pid int) bool, ownInUse bool) SweepResult {
	var res SweepResult
	for _, dir := range ebWebViewDirs(root) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 目录不存在是常态（还没开过岛）
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pid, _, ok := parseEphemeralProfile(e.Name())
			if !ok {
				continue
			}
			switch {
			case pid == self && ownInUse:
				res.Kept++
				continue
			case pid != self && alive(pid):
				res.Kept++
				continue
			}
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				res.Failed = append(res.Failed, fmt.Errorf("%s：%w", e.Name(), err).Error())
				continue
			}
			res.Removed++
		}
	}
	return res
}

// sweepStaleProfiles 在开岛之前调用（openMu 已持有）：清掉以前留下、现在没人用的临时配置目录。
// 同步执行：每个目录约 100 个小文件，毫秒级；放到后台会与随后创建的新 profile 抢同一个目录。
func sweepStaleProfiles() SweepResult {
	root, ok := profileRoot()
	if !ok {
		return SweepResult{}
	}
	res := sweepEphemeralProfiles(root, os.Getpid(), processAlive, openCount.Load() > 0)
	if err := res.Err(); err != nil {
		if hook := failureHook.Load(); hook != nil {
			(*hook)(err)
		}
	}
	return res
}
