package island

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// seedProfile 在 dir 下建一个带文件与子目录的假 profile 目录（像 WebView2 留下的那样有层级），返回它的路径。
func seedProfile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(p, "Default", "Network"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"Preferences", filepath.Join("Default", "History"), filepath.Join("Default", "Network", "Cookies")} {
		if err := os.WriteFile(filepath.Join(p, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestParseEphemeralProfile(t *testing.T) {
	cases := []struct {
		name     string
		pid, seq int
		ok       bool
	}{
		{"WV2Profile_viva-ephemeral-1234-7", 1234, 7, true},
		{"WV2Profile_viva-ephemeral-1-1", 1, 1, true},
		{"WV2Profile_viva-ephemeral-0-1", 0, 0, false},                    // 进程号必须为正
		{"WV2Profile_viva-ephemeral-1234-0", 0, 0, false},                 // 序号从 1 起
		{"WV2Profile_viva-ephemeral-1234", 0, 0, false},                   // 缺序号
		{"WV2Profile_viva-ephemeral-1234-7-9", 0, 0, false},               // 多余的段
		{"WV2Profile_viva-ephemeral-abc-1", 0, 0, false},                  // 非数字
		{"WV2Profile_viva-ephemeral--1", 0, 0, false},                     // 空进程号
		{"WV2Profile_viva-ephemeral-+12-3", 0, 0, false},                  // 正负号不收
		{"WV2Profile_viva-ephemeral-12- 3", 0, 0, false},                  // 空白不收
		{"WV2Profile_other", 0, 0, false},                                 // 别的 profile
		{"Default", 0, 0, false},                                          // 浏览器自己的目录
		{"wv2profile_viva-ephemeral-1-1", 0, 0, false},                    // 名字由 WebView2 生成，大小写固定
		{"xWV2Profile_viva-ephemeral-1-1", 0, 0, false},                   // 前缀必须在开头
		{"WV2Profile_viva-ephemeral-99999999999999999999-1", 0, 0, false}, // 溢出
	}
	for _, c := range cases {
		pid, seq, ok := parseEphemeralProfile(c.name)
		if ok != c.ok || pid != c.pid || seq != c.seq {
			t.Errorf("%q → (%d, %d, %v)，期望 (%d, %d, %v)", c.name, pid, seq, ok, c.pid, c.seq, c.ok)
		}
	}
}

// 清理规则的全貌：删什么、留什么、绝不碰什么。
func TestSweepRules(t *testing.T) {
	root := t.TempDir()
	eb1 := filepath.Join(root, "Proxy1", "EBWebView")
	eb2 := filepath.Join(root, "Proxy2", "EBWebView")
	ebNone := filepath.Join(root, "EBWebView")
	const self, other = 100, 300
	alive := func(pid int) bool { return pid == other }

	remove := []string{ // 应当被删：已退出进程的，以及本进程的（没有岛开着时）
		seedProfile(t, eb1, "WV2Profile_viva-ephemeral-200-1"),
		seedProfile(t, eb2, "WV2Profile_viva-ephemeral-200-2"),
		seedProfile(t, ebNone, "WV2Profile_viva-ephemeral-200-3"),
		seedProfile(t, eb1, "WV2Profile_viva-ephemeral-100-4"),
	}
	keep := []string{ // 应当保留
		seedProfile(t, eb1, "WV2Profile_viva-ephemeral-300-1"), // 另一个活着的实例的岛
		seedProfile(t, eb1, "Default"),                         // 浏览器自己的目录
		seedProfile(t, eb1, "WV2Profile_other"),                // 别的 profile
		seedProfile(t, eb1, "WV2Profile_viva-ephemeral-abc-1"), // 名字不合格
		seedProfile(t, eb1, "WV2Profile_viva-ephemeral-200"),   // 缺序号
		// 位置不对：不在 <代理槽>\EBWebView 的直接子级
		seedProfile(t, filepath.Join(root, "Proxy1"), "WV2Profile_viva-ephemeral-200-5"),
		seedProfile(t, filepath.Join(eb1, "Default"), "WV2Profile_viva-ephemeral-200-6"),
	}
	// 同名的普通文件（不是目录）不删
	filePath := filepath.Join(eb1, "WV2Profile_viva-ephemeral-200-9")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep = append(keep, filePath)

	res := sweepEphemeralProfiles(root, self, alive, false)
	for _, p := range remove {
		if exists(p) {
			t.Errorf("应当被删却还在：%s", p)
		}
	}
	for _, p := range keep {
		if !exists(p) {
			t.Errorf("不该删却被删了：%s", p)
		}
	}
	if res.Removed != len(remove) || res.Kept != 1 || len(res.Failed) != 0 {
		t.Errorf("结果 %+v，期望 Removed=%d Kept=1 无失败", res, len(remove))
	}
	if err := res.Err(); err != nil {
		t.Errorf("没有失败时 Err 应为 nil，得到 %v", err)
	}
}

// 本进程还有岛开着时：序号是 Viva 内部的，认不出哪个目录属于哪个已开的岛，所以本进程的一概不删；
// 已退出进程留下的照删。
func TestSweepKeepsOwnProfilesWhileAnIslandIsOpen(t *testing.T) {
	root := t.TempDir()
	eb := filepath.Join(root, "Proxy1", "EBWebView")
	own := seedProfile(t, eb, "WV2Profile_viva-ephemeral-100-1")
	gone := seedProfile(t, eb, "WV2Profile_viva-ephemeral-200-1")

	res := sweepEphemeralProfiles(root, 100, func(int) bool { return false }, true)
	if !exists(own) {
		t.Error("本进程有岛开着时，本进程的目录不能删")
	}
	if exists(gone) {
		t.Error("已退出进程的目录应当删")
	}
	if res.Removed != 1 || res.Kept != 1 {
		t.Errorf("结果 %+v，期望 Removed=1 Kept=1", res)
	}
}

func TestSweepMissingRootIsNoop(t *testing.T) {
	res := sweepEphemeralProfiles(filepath.Join(t.TempDir(), "不存在"), 1, func(int) bool { return false }, false)
	if res.Removed != 0 || res.Kept != 0 || len(res.Failed) != 0 {
		t.Errorf("根目录不存在时应当什么都不做，得到 %+v", res)
	}
}

// 没登记 Class 就不清理：不能凭空猜一个目录去删。
func TestSweepStaleProfilesWithoutClassDoesNothing(t *testing.T) {
	SetProfileClass("")
	if root, ok := profileRoot(); ok {
		t.Fatalf("没有 Class 时不应有根目录，得到 %q", root)
	}
	if res := sweepStaleProfiles(); res.Removed != 0 || res.Kept != 0 || len(res.Failed) != 0 {
		t.Errorf("得到 %+v", res)
	}
}

// 根目录的规则与 Viva 的 userDataFolder 一致：缓存目录\<Class>\WebView2；清理按本进程是否有岛开着决定删不删自己的目录。
func TestSweepStaleProfilesUsesVivaCacheRootAndOpenCount(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	SetProfileClass("GuiZhiSweepTest")
	t.Cleanup(func() { SetProfileClass(""); openCount.Store(0) })

	root, ok := profileRoot()
	if !ok {
		t.Fatal("登记了 Class 应当有根目录")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "GuiZhiSweepTest", "WebView2"); root != want {
		t.Fatalf("根目录 = %q，期望 %q", root, want)
	}
	if rel, err := filepath.Rel(cache, root); err != nil || rel == "" || rel[0] == '.' {
		t.Fatalf("根目录必须落在被隔离的缓存目录里：%q（缓存 %q）", root, cache)
	}

	own := seedProfile(t, filepath.Join(root, "Proxy1", "EBWebView"), "WV2Profile_viva-ephemeral-"+strconv.Itoa(os.Getpid())+"-1")

	openCount.Store(1)
	sweepStaleProfiles()
	if !exists(own) {
		t.Fatal("有岛开着时不能删本进程的目录")
	}
	openCount.Store(0)
	res := sweepStaleProfiles()
	if exists(own) || res.Removed != 1 {
		t.Fatalf("没有岛开着时应当删掉本进程留下的目录：removed=%d exists=%v", res.Removed, exists(own))
	}
}
