package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/settings"
)

// makeWebView2Folder 造一个只含给定 localStorage 键值的 WebView2 用户数据目录。
// 键值按 Chromium 的 Latin1 编码写（只限 ASCII）；含中文的真实数据见 testdata/webview2-localstorage 夹具。
func makeWebView2Folder(t *testing.T, kv map[string]string) string {
	t.Helper()
	folder := t.TempDir()
	db, err := leveldb.OpenFile(filepath.Join(folder, webview2LocalStorageRel), nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range kv {
		if err := db.Put([]byte("_"+rendererOrigin+"\x00\x01"+k), []byte("\x01"+v), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return folder
}

// prefsInFile 取 ui-prefs.json 的 prefs 段（键 → 紧凑 JSON）。
func prefsInFile(t *testing.T, path string) map[string]string {
	t.Helper()
	top := readPrefsFile(t, path)
	out := map[string]string{}
	if raw, ok := top["prefs"]; ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		for k, v := range m {
			out[k] = compactRaw(t, v)
		}
	}
	return out
}

func TestImportLegacyFromRealWebView2Fixture(t *testing.T) {
	env := newSettingsEnv(t)
	fx := loadWebView2Fixture(t)
	if err := env.ns.importLegacy(context.Background(), installWebView2Fixture(t, fx), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got := env.snapshot(t)

	// 界面设置：guizhi-settings 里属于界面的部分
	ui := got.UI
	if ui.ThemeMode != settings.ThemeDark || ui.ThemeColor != "green" || ui.ThemeHue != 150 || ui.ThemeSaturation != 30 ||
		ui.FontSize != settings.FontLarge || ui.MotionPreference != settings.MotionReduced {
		t.Errorf("外观没有按旧设置导入：%+v", ui)
	}
	if want := map[string]string{"summary": "模型-甲", "qa": "fixture-model"}; len(ui.ScenarioModelDefaults) != 2 ||
		ui.ScenarioModelDefaults["summary"] != want["summary"] || ui.ScenarioModelDefaults["qa"] != want["qa"] {
		t.Errorf("场景默认模型 = %v", ui.ScenarioModelDefaults)
	}

	// Go 侧设置：补进 settings 表，由同一套校验写入
	app := got.App
	wantProxy := settings.NetworkProxy{Mode: "manual", Protocol: "http", Host: "127.0.0.1", Port: 7890, Username: "fixture-user", Password: "fixture-proxy-password", Bypass: "<local>,localhost"}
	if app.Language != settings.LangZh || !app.LaunchAtStartup || app.EnableNotifications ||
		!app.BackupAutoEnabled || app.BackupIntervalHours != 12 || app.BackupKeepCount != 20 {
		t.Errorf("Go 侧设置没有按旧设置导入：%+v", app)
	}
	if app.NetworkProxy != wantProxy {
		t.Errorf("代理 = %v，期望 %v", app.NetworkProxy, wantProxy)
	}
	if want := (settings.FlareSolverr{Enabled: true, Connection: "local", Port: 8191}); app.FlareSolverr != want {
		t.Errorf("FlareSolverr = %+v", app.FlareSolverr)
	}

	// 偏好：与解析器的结果逐项一致（不多不少），并核对几项字面值
	leg := settings.ParseLegacy(fx.Entries)
	wantPrefs := map[string]string{}
	for k, v := range leg.Prefs {
		wantPrefs[k] = compactRaw(t, v)
	}
	if gotPrefs := prefsInFile(t, env.prefsFile()); !equalStringMaps(gotPrefs, wantPrefs) {
		t.Errorf("偏好与解析结果不一致：\n得到 %v\n期望 %v", gotPrefs, wantPrefs)
	}
	for key, want := range map[string]string{
		settings.PrefSidebarPanelWidth: "333", settings.PrefItemListPaneWidth: "410", settings.PrefWikiCatalogPaneWidth: "250",
		settings.PrefSidebarCollapsed: "true", settings.PrefAppModule: `"wiki"`, settings.PrefLibraryViewMode: `"list"`,
		settings.PrefWechatWidth: `"wide"`, settings.PrefAskActiveSession: `"ask-session-fixture"`,
		settings.PrefSetupDismissed: "true", settings.PrefMigrationDismissed: "true",
	} {
		raw, ok := env.store.Get(key)
		if !ok || string(raw) != want {
			t.Errorf("偏好 %s = %s（存在=%v），期望 %s", key, raw, ok, want)
		}
	}

	// 导入记录
	rec := env.store.Import()
	if rec == nil || rec.Source != "webview2" || rec.Origin != rendererOrigin || rec.Keys != len(fx.Entries) {
		t.Fatalf("导入记录不对：%+v", rec)
	}
	if len(rec.Issues) != 0 {
		t.Errorf("夹具是完好的数据，不该有问题：%v", rec.Issues)
	}

	// 机密：AI 密钥不导入（归 ai-models.json）；代理口令只进 settings 表，不进界面设置文件，也不进日志用的导入记录
	file, err := os.ReadFile(env.prefsFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sk-fixture-not-a-real-key", "fixture-proxy-password"} {
		if strings.Contains(string(file), secret) {
			t.Errorf("机密 %q 不该出现在 ui-prefs.json 里", secret)
		}
	}
	rows, err := env.lib.db.Query(context.Background(), "SELECT key, value FROM settings")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows.Values {
		if v, _ := r[1].(string); strings.Contains(v, "sk-fixture-not-a-real-key") {
			t.Errorf("AI 密钥不该导入 settings 表（键 %v）", r[0])
		}
	}
	if _, err := os.Stat(filepath.Join(env.paths.ConfigDir(), "ai-models.json")); !os.IsNotExist(err) {
		t.Error("导入设置不该动 ai-models.json")
	}
	// ytDlpPath / ffmpegPath 故意不导入
	for _, key := range []string{"ytDlpPath", "ffmpegPath"} {
		if _, ok := env.row(key); ok {
			t.Errorf("%s 不该被导入（可执行文件路径只接受文件选择器返回的值）", key)
		}
	}
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// 导入只发生一次：之后即使旧数据变了、换了位置，也不再动现有设置。
func TestImportLegacyRunsOnce(t *testing.T) {
	env := newSettingsEnv(t)
	fx := loadWebView2Fixture(t)
	ctx := context.Background()
	if err := env.ns.importLegacy(ctx, installWebView2Fixture(t, fx), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	first := env.snapshot(t)
	// 用户之后在原生界面里改了设置
	if _, err := env.ns.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeLight; v.App.BackupKeepCount = 5 }); err != nil {
		t.Fatal(err)
	}
	changed := env.snapshot(t)

	// 再来一次（旧数据依然在）：什么都不该变
	if err := env.ns.importLegacy(ctx, installWebView2Fixture(t, fx), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := env.snapshot(t); !got.Equal(changed) || got.Equal(first) {
		t.Fatalf("已初始化后不该再导入：\n%+v", got)
	}
}

// 库里已有的值优先：导入只补缺的键（例如备份还原后库里已有设置）。
func TestImportLegacyKeepsExistingDatabaseValues(t *testing.T) {
	env := newSettingsEnv(t)
	env.putRow(t, "language", `"en"`)
	env.putRow(t, "backupKeepCount", "7")
	fx := loadWebView2Fixture(t)
	if err := env.ns.importLegacy(context.Background(), installWebView2Fixture(t, fx), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	app := env.snapshot(t).App
	if app.Language != settings.LangEn || app.BackupKeepCount != 7 {
		t.Errorf("库里已有的值被覆盖了：language=%s keep=%d", app.Language, app.BackupKeepCount)
	}
	if app.BackupIntervalHours != 12 || !app.LaunchAtStartup {
		t.Errorf("库里没有的键应从旧设置补上：%+v", app)
	}
}

func TestImportLegacyFreshInstall(t *testing.T) {
	env := newSettingsEnv(t)
	if err := env.ns.importLegacy(context.Background(), t.TempDir(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !env.store.Initialized() {
		t.Fatal("全新安装也要写出提交点，之后不再探测旧数据")
	}
	rec := env.store.Import()
	if rec == nil || rec.Source != "none" || rec.Keys != 0 || len(rec.Issues) != 0 {
		t.Fatalf("导入记录 = %+v", rec)
	}
	if !env.snapshot(t).Equal(settings.Default()) {
		t.Error("全新安装应是出厂默认值")
	}
	for key := range appSettingKeys {
		if _, ok := env.row(key); ok {
			t.Errorf("全新安装不该往表里写 %s", key)
		}
	}
	if got := prefsInFile(t, env.prefsFile()); len(got) != 0 {
		t.Errorf("全新安装不该有偏好：%v", got)
	}
}

func TestImportLegacySourcePriority(t *testing.T) {
	ctx := context.Background()
	fx := loadWebView2Fixture(t)
	partial := map[string]string{"guizhi-setup-dismissed": "1"} // WebView2 里只有偏好，没有归知设置

	cases := []struct {
		name       string
		webview2   func(t *testing.T) string
		electron   func(t *testing.T) string // 返回 Electron 版的用户数据目录
		wantSource string
		wantKeys   int // -1：只要求读到了数据
	}{
		{"WebView2 有设置：用它，Electron 的数据不管",
			func(t *testing.T) string { return installWebView2Fixture(t, fx) },
			copyFixtureUserData, "webview2", len(fx.Entries)},
		{"WebView2 只有零散偏好、Electron 还没导入过：用 Electron 的",
			func(t *testing.T) string { return makeWebView2Folder(t, partial) },
			copyFixtureUserData, "electron", -1},
		{"WebView2 只有零散偏好、Electron 已导入过：用 WebView2 的",
			func(t *testing.T) string { return makeWebView2Folder(t, partial) },
			func(t *testing.T) string {
				ud := copyFixtureUserData(t)
				if err := markLegacyImported(ud); err != nil {
					t.Fatal(err)
				}
				return ud
			}, "webview2", 1},
		{"WebView2 没有数据、Electron 已导入过：全新安装",
			func(t *testing.T) string { return t.TempDir() },
			func(t *testing.T) string {
				ud := copyFixtureUserData(t)
				if err := markLegacyImported(ud); err != nil {
					t.Fatal(err)
				}
				return ud
			}, "none", 0},
		{"WebView2 没有数据、Electron 还没导入过：从 Electron 升上来",
			func(t *testing.T) string { return t.TempDir() },
			copyFixtureUserData, "electron", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newSettingsEnv(t)
			if err := env.ns.importLegacy(ctx, c.webview2(t), c.electron(t)); err != nil {
				t.Fatal(err)
			}
			rec := env.store.Import()
			if rec == nil || rec.Source != c.wantSource {
				t.Fatalf("来源 = %+v，期望 %s", rec, c.wantSource)
			}
			switch {
			case c.wantKeys >= 0 && rec.Keys != c.wantKeys:
				t.Errorf("读到的 localStorage 键数 = %d，期望 %d", rec.Keys, c.wantKeys)
			case c.wantKeys < 0 && rec.Keys == 0:
				t.Error("应读到旧数据，实际一个键都没有")
			}
		})
	}
}

// Electron 版直接升级上来：真实 Electron 夹具里的设置与偏好都能导入。
func TestImportLegacyFromRealElectronFixture(t *testing.T) {
	env := newSettingsEnv(t)
	if err := env.ns.importLegacy(context.Background(), t.TempDir(), copyFixtureUserData(t)); err != nil {
		t.Fatal(err)
	}
	rec := env.store.Import()
	if rec == nil || rec.Source != "electron" || rec.Origin != "file://" {
		t.Fatalf("导入记录 = %+v", rec)
	}
	got := env.snapshot(t)
	if got.App.Language != settings.LangZh {
		t.Errorf("language = %q", got.App.Language)
	}
	// 夹具里的 theme 是 Electron 时代的键，不属于现在的界面设置：界面设置应是默认值
	if !got.UI.Equal(settings.DefaultUI()) {
		t.Errorf("界面设置应为默认值：%+v", got.UI)
	}
	if raw, ok := env.store.Get(settings.PrefSetupDismissed); !ok || string(raw) != "true" {
		t.Errorf("setup.dismissed = %s（存在=%v）", raw, ok)
	}
}

// 读不了旧数据：返回错误、不写提交点，下次启动重试；之后读得到时能补上导入。
func TestImportLegacyCorruptSourceRetries(t *testing.T) {
	env := newSettingsEnv(t)
	ctx := context.Background()
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

	err := env.ns.importLegacy(ctx, bad, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "WebView2") {
		t.Fatalf("应返回说明来源的错误：%v", err)
	}
	if env.hasPrefsFile() || env.store.Initialized() {
		t.Fatal("读取失败不该写提交点，否则旧设置永远丢了")
	}
	for key := range appSettingKeys {
		if _, ok := env.row(key); ok {
			t.Fatalf("读取失败不该往表里写 %s", key)
		}
	}

	// 下次启动：数据读得到了
	fx := loadWebView2Fixture(t)
	if err := env.ns.importLegacy(ctx, installWebView2Fixture(t, fx), t.TempDir()); err != nil {
		t.Fatalf("重试应成功：%v", err)
	}
	if got := env.snapshot(t).UI.ThemeMode; got != settings.ThemeDark {
		t.Errorf("重试后应已导入：主题 %s", got)
	}
}

// 一个键非法（FlareSolverr 的 SSH 模式缺主机别名）只跳过它，不挡住别的键，也不影响提交；问题记在导入记录里（不含值）。
func TestImportLegacyInvalidKeyBecomesIssue(t *testing.T) {
	env := newSettingsEnv(t)
	folder := makeWebView2Folder(t, map[string]string{
		"guizhi-settings": `{"state":{"language":"en","themeMode":"dark","flareSolverr":{"enabled":true,"connection":"ssh","port":8191,"sshHost":""}},"version":3}`,
	})
	if err := env.ns.importLegacy(context.Background(), folder, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got := env.snapshot(t)
	if got.App.Language != settings.LangEn || got.UI.ThemeMode != settings.ThemeDark {
		t.Errorf("其他键应照常导入：%+v %s", got.App.Language, got.UI.ThemeMode)
	}
	if got.App.FlareSolverr != settings.DefaultFlareSolverr() {
		t.Errorf("非法的 FlareSolverr 应被跳过：%+v", got.App.FlareSolverr)
	}
	if _, ok := env.row("flareSolverr"); ok {
		t.Error("非法的 FlareSolverr 不该写进表")
	}
	rec := env.store.Import()
	if rec == nil || len(rec.Issues) != 1 || !strings.HasPrefix(rec.Issues[0], "flareSolverr: ") {
		t.Fatalf("应记录一条 flareSolverr 的问题：%+v", rec)
	}
}

// 坏数据不影响其余键：保存损坏的 guizhi-settings 时，仍能导入偏好，并记下问题。
func TestImportLegacyBadSettingsKeepsPrefs(t *testing.T) {
	env := newSettingsEnv(t)
	folder := makeWebView2Folder(t, map[string]string{
		"guizhi-settings":        `{not json`,
		"guizhi-setup-dismissed": "1",
	})
	if err := env.ns.importLegacy(context.Background(), folder, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !env.snapshot(t).Equal(settings.Default()) {
		t.Error("设置损坏时界面与 Go 侧设置都应是默认值")
	}
	if raw, ok := env.store.Get(settings.PrefSetupDismissed); !ok || string(raw) != "true" {
		t.Errorf("偏好应照常导入：%s", raw)
	}
	if rec := env.store.Import(); rec == nil || len(rec.Issues) == 0 || !strings.HasPrefix(rec.Issues[0], settings.LegacySettingsKey) {
		t.Errorf("应记录 guizhi-settings 损坏：%+v", rec)
	}
}

// 自动化实例（截图 / 基准）：固定出厂外观，不读旧版 WebView2、不写任何设置。
func TestAutomationBootNeverTouchesUserSettings(t *testing.T) {
	env := newSettingsEnv(t)
	fx := loadWebView2Fixture(t)
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", installWebView2Fixture(t, fx)) // 即使旧配置就摆在默认位置

	spec := nativeBoot{lib: env.lib, automation: true}.baseTheme(env.ns)
	if spec.Mode != kit.DefaultThemeSpec().Mode || spec.Hue != kit.DefaultThemeSpec().Hue {
		t.Errorf("自动化实例应是出厂外观：%+v", spec)
	}
	if env.hasPrefsFile() || env.store.Initialized() {
		t.Fatal("自动化实例不该创建界面设置文件")
	}
	for key := range appSettingKeys {
		if _, ok := env.row(key); ok {
			t.Fatalf("自动化实例不该往表里写 %s", key)
		}
	}
}

// 真实运行：先导入旧版设置，再把外观映射成 kit.ThemeSpec。
func TestBootLoadsImportedAppearance(t *testing.T) {
	env := newSettingsEnv(t)
	fx := loadWebView2Fixture(t)
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", installWebView2Fixture(t, fx))

	spec := nativeBoot{lib: env.lib}.baseTheme(env.ns)
	if spec.Mode != kit.ModeDark || spec.Hue != 150 || spec.Saturation != 30 || spec.FontSize != "large" || spec.Motion != kit.MotionReduced {
		t.Fatalf("外观应来自导入的旧设置：%+v", spec)
	}
	if !env.store.Initialized() {
		t.Error("启动应已完成导入")
	}

	// 第二次启动：不再导入，直接读到上次保存的外观（即使旧数据已经没了）
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", t.TempDir())
	again := nativeBoot{lib: env.lib}.baseTheme(newNativeSettings(env.lib, openUIPrefsStore(env.prefsFile(), nil), nil))
	if again.Mode != kit.ModeDark || again.Hue != 150 {
		t.Errorf("第二次启动的外观 = %+v", again)
	}
}

// 导入失败（旧数据损坏）不能挡住启动：退回出厂外观并记日志，下次再试。
func TestBootSurvivesCorruptLegacyData(t *testing.T) {
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
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", bad)

	spec := nativeBoot{lib: env.lib}.baseTheme(env.ns)
	if spec.Mode != kit.DefaultThemeSpec().Mode {
		t.Errorf("应退回出厂外观：%+v", spec)
	}
	if env.store.Initialized() {
		t.Error("导入失败不该写提交点")
	}
	// 失败原因进 error.log（脱敏的那份），方便排查
	logFile := filepath.Join(env.paths.UserData, "logs", "error.log")
	if b, err := os.ReadFile(logFile); err != nil || !strings.Contains(string(b), "native-settings") {
		t.Errorf("失败应写进 error.log：%v", err)
	}
}

func testFlags(theme, hue, font, motion, wallpaper string) nativeFlags {
	opacity, blur, width, height := 1.0, 0.0, 1280, 800
	return nativeFlags{theme: &theme, hue: &hue, font: &font, motion: &motion, wallpaper: &wallpaper,
		wpOpacity: &opacity, wpBlur: &blur, width: &width, height: &height}
}

func TestNativeFlagsThemeSpecOverlay(t *testing.T) {
	base := kit.DefaultThemeSpec()
	base.Mode, base.Hue, base.Saturation, base.FontSize, base.Motion = kit.ModeDark, 150, 30, "large", kit.MotionReduced

	t.Run("没给开关：保留设置里的外观", func(t *testing.T) {
		got, err := testFlags("", "", "", "", "").themeSpec(base, false)
		if err != nil || got.Mode != kit.ModeDark || got.Hue != 150 || got.Saturation != 30 || got.FontSize != "large" || got.Motion != kit.MotionReduced {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("自动化实例没给明暗：固定浅色，其余保留", func(t *testing.T) {
		got, err := testFlags("", "", "", "", "").themeSpec(kit.DefaultThemeSpec(), true)
		if err != nil || got.Mode != kit.ModeLight {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("开关覆盖设置", func(t *testing.T) {
		got, err := testFlags("light", "200,40", "small", "off", "").themeSpec(base, false)
		if err != nil || got.Mode != kit.ModeLight || got.Hue != 200 || got.Saturation != 40 || got.FontSize != "small" || got.Motion != kit.MotionOff {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("只覆盖一项：其他项保留", func(t *testing.T) {
		got, err := testFlags("", "", "small", "", "").themeSpec(base, false)
		if err != nil || got.FontSize != "small" || got.Mode != kit.ModeDark || got.Hue != 150 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	for name, f := range map[string]nativeFlags{
		"明暗写错":  testFlags("blue", "", "", "", ""),
		"主题色写错": testFlags("", "no-such-hue", "", "", ""),
		"字号写错":  testFlags("", "", "huge", "", ""),
		"动效写错":  testFlags("", "", "", "fast", ""),
		"壁纸不存在": testFlags("", "", "", "", filepath.Join(t.TempDir(), "missing.png")),
	} {
		t.Run(name+"：报错而不是静默落回默认", func(t *testing.T) {
			if _, err := f.themeSpec(base, false); err == nil {
				t.Fatal("应返回错误")
			}
		})
	}
}
