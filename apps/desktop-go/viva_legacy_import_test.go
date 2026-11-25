package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/settings"
)

// 首次启动：从旧版 WebView2 的 localStorage 导入设置；第二次启动已初始化，幂等跳过（即使旧数据已经变了）。
func TestImportVivaLegacyFirstRunThenIdempotent(t *testing.T) {
	env := newSettingsEnv(t)
	fx := loadWebView2Fixture(t)
	folder := installWebView2Fixture(t, fx)

	if err := importVivaLegacy(context.Background(), env.lib, env.ns, folder); err != nil {
		t.Fatal(err)
	}
	if !env.store.Initialized() {
		t.Fatal("首次导入应写出 ui-prefs.json 提交点")
	}
	first := env.snapshot(t)
	if first.UI.ThemeMode != settings.ThemeDark || first.UI.ThemeColor != "green" || !first.App.LaunchAtStartup {
		t.Fatalf("设置没有按旧版导入：UI=%+v LaunchAtStartup=%v", first.UI, first.App.LaunchAtStartup)
	}

	// 旧数据目录换成空的：已初始化，什么都不改
	if err := importVivaLegacy(context.Background(), env.lib, env.ns, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if again := env.snapshot(t); again.UI.ThemeMode != first.UI.ThemeMode || again.UI.ThemeColor != first.UI.ThemeColor {
		t.Fatalf("第二次不该改动已导入的设置：%+v → %+v", first.UI, again.UI)
	}
}

// 导入失败不阻断启动：返回错误但不写提交点（下次启动自动重试），原因进脱敏的 error.log。
func TestImportVivaLegacyFailureDoesNotCommit(t *testing.T) {
	env := newSettingsEnv(t)
	bad := t.TempDir()
	dir := filepath.Join(bad, webview2LocalStorageRel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CURRENT"), []byte("MANIFEST-000001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST-000001"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := importVivaLegacy(context.Background(), env.lib, env.ns, bad); err == nil {
		t.Fatal("旧数据损坏应返回错误")
	}
	if env.store.Initialized() {
		t.Error("失败不该写提交点，否则下次启动不会重试")
	}
	logFile := filepath.Join(env.paths.UserData, "logs", "error.log")
	if b, err := os.ReadFile(logFile); err != nil || !strings.Contains(string(b), "native-settings") {
		t.Errorf("失败应写进 error.log：%v", err)
	}
	// 数据恢复后下次启动能成功导入
	fx := loadWebView2Fixture(t)
	if err := importVivaLegacy(context.Background(), env.lib, env.ns, installWebView2Fixture(t, fx)); err != nil || !env.store.Initialized() {
		t.Fatalf("重试应成功：%v", err)
	}
}

// 全新安装（没有旧数据）：也写提交点（来源 none），之后不再导入。
func TestImportVivaLegacyFreshInstall(t *testing.T) {
	env := newSettingsEnv(t)
	if err := importVivaLegacy(context.Background(), env.lib, env.ns, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !env.store.Initialized() {
		t.Fatal("全新安装也应完成初始化")
	}
}

// 手工用 -userdata / -db 指向临时目录试跑时，不能误读本机真实的 %LOCALAPPDATA%\GuiZhi\WebView2。
func TestVivaLegacyWebView2FolderIsolation(t *testing.T) {
	real := func() string { return `C:\Users\u\AppData\Local\GuiZhi\WebView2` }
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	ud := `D:\tmp\run1`

	cases := []struct {
		name     string
		explicit bool
		env      map[string]string
		want     string
	}{
		{"正式启动（没给 -userdata/-db）：读真实旧配置", false, nil, real()},
		{"临时试跑：只读该次 userData 下的 webview2", true, nil, filepath.Join(ud, "webview2")},
		{"临时试跑但明确指了 WEBVIEW2_USER_DATA_FOLDER：按它", true, map[string]string{"WEBVIEW2_USER_DATA_FOLDER": `E:\wv`}, real()},
		{"正式启动 + 环境变量：仍取当前进程的 WebView2 目录", false, map[string]string{"WEBVIEW2_USER_DATA_FOLDER": `E:\wv`}, real()},
	}
	for _, c := range cases {
		if got := vivaLegacyWebView2Folder(c.explicit, env(c.env), ud, real); got != c.want {
			t.Errorf("%s：got %q want %q", c.name, got, c.want)
		}
	}
}
