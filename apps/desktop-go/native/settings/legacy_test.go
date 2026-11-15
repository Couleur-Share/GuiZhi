package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/kit"
)

// realisticEntries 是一份接近真实用户的旧 localStorage（含机密字段，用来确认它们不会流到不该去的地方）。
func realisticEntries() map[string]string {
	return map[string]string{
		"guizhi-settings": `{"state":{"themeMode":"dark","themeColor":"green","themeHue":150,"themeSaturation":30,
			"fontSize":"large","language":"zh-CN","launchAtStartup":true,"enableNotifications":false,
			"backupIntervalHours":0,"backupKeepCount":30,"transcribeDiarize":true,
			"networkProxy":{"mode":"manual","protocol":"http","host":"127.0.0.1","port":7890,"password":"proxy-pw"},
			"flareSolverr":{"enabled":true,"connection":"local","port":8191,"sshHost":""},
			"ytDlpPath":"C:\\tools\\yt-dlp.exe","ffmpegPath":"C:\\tools\\ffmpeg.exe",
			"aiApiKey":"sk-secret","aiProviders":[{"id":"p","apiKey":"sk-secret"}],
			"webdavPassword":"plaintext-secret",
			"scenarioModelDefaults":{"qa":" m-qa ","bogus":"x"}},"version":3}`,
		"ui-storage":                    `{"state":{"appModule":"wiki","isSidebarCollapsed":true,"sidebarPanelWidth":9999,"libraryListDensity":"compact"},"version":0}`,
		"guizhi-library-table-config":   `{"columns":[{"id":"title","visible":true,"width":300}]}`,
		"guizhi-library-smart-views-v1": `[{"id":"v1","name":"未读","scope":"all"}]`,
		"guizhi-content-reading-v1":     `{"item1":{"tab":"body","scrollTopByTab":{"body":120},"updatedAt":1}}`,
		"guizhi-snapshot-reading-v1":    `{"k":{"updatedAt":2}}`,
		"wechat-reader-width":           "wide",
		"guizhi-ask-active-session":     "sess-1",
		"guizhi-setup-dismissed":        "1",
		"guizhi-migration-dismissed":    "1",
		"reading-original-catalog:e1":   "true",
		"reading-original-catalog:e2":   "false",
		"reading-ai-catalog:i1:body":    "true",
		"guizhi-legacy-ls-imported":     "1731600000000",
		"guizhi-table-config":           `{"old":true}`,
		"some-unrelated-key":            "x",
	}
}

func TestParseLegacyRealisticProfile(t *testing.T) {
	leg := ParseLegacy(realisticEntries())
	if !leg.HasSettings || len(leg.Issues) != 0 {
		t.Fatalf("应当干净地解出：has=%v issues=%v", leg.HasSettings, leg.Issues)
	}

	ui := leg.UI
	if ui.ThemeMode != ThemeDark || ui.ThemeColor != "green" || ui.ThemeHue != 150 || ui.FontSize != FontLarge {
		t.Errorf("界面设置不对：%+v", ui)
	}
	if got := ui.ScenarioModelDefaults; len(got) != 1 || got["qa"] != "m-qa" {
		t.Errorf("场景默认模型应去空白并丢弃未知场景：%v", got)
	}
	encoded, _ := json.Marshal(ui)
	for _, secret := range []string{"sk-secret", "plaintext-secret", "proxy-pw"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("界面设置里不该出现机密 %q", secret)
		}
	}

	app := leg.App
	if string(app["language"]) != `"zh"` || string(app["launchAtStartup"]) != "true" || string(app["enableNotifications"]) != "false" ||
		string(app["transcribeDiarize"]) != "true" {
		t.Errorf("Go 侧设置不对：%v", app)
	}
	if string(app["backupIntervalHours"]) != "1" || string(app["backupKeepCount"]) != "30" {
		t.Errorf("备份数字应夹到范围内：%s %s", app["backupIntervalHours"], app["backupKeepCount"])
	}
	if !strings.Contains(string(app["networkProxy"]), `"host":"127.0.0.1"`) || !strings.Contains(string(app["flareSolverr"]), `"enabled":true`) {
		t.Errorf("代理与 FlareSolverr 应以原始对象交给写入处：%s %s", app["networkProxy"], app["flareSolverr"])
	}
	for _, key := range []string{"ytDlpPath", "ffmpegPath"} {
		if _, ok := app[key]; ok {
			t.Errorf("%s 不应导入：可执行文件路径只接受文件选择器返回过的值", key)
		}
	}
	for _, key := range []string{"aiApiKey", "aiProviders", "webdavPassword", "themeMode"} {
		if _, ok := app[key]; ok {
			t.Errorf("%s 不属于 Go 侧设置", key)
		}
	}

	want := map[string]string{
		PrefAppModule:          `"wiki"`,
		PrefSidebarCollapsed:   `true`,
		PrefSidebarPanelWidth:  `640`,
		PrefLibraryViewMode:    `"list"`,
		PrefLibraryColumns:     `{"columns":[{"id":"title","visible":true,"width":300}]}`,
		PrefSmartViews:         `[{"id":"v1","name":"未读","scope":"all"}]`,
		PrefContentReading:     `{"item1":{"tab":"body","scrollTopByTab":{"body":120},"updatedAt":1}}`,
		PrefSnapshotReading:    `{"k":{"updatedAt":2}}`,
		PrefWechatWidth:        `"wide"`,
		PrefAskActiveSession:   `"sess-1"`,
		PrefSetupDismissed:     `true`,
		PrefMigrationDismissed: `true`,
		PrefCatalogOpen:        `{"ai:i1:body":true,"original:e1":true}`,
	}
	for key, w := range want {
		if got := string(leg.Prefs[key]); got != w {
			t.Errorf("%s = %s，期望 %s", key, got, w)
		}
	}
	for _, key := range []string{PrefItemListPaneWidth, PrefWikiCatalogPaneWidth} {
		if _, ok := leg.Prefs[key]; ok {
			t.Errorf("旧数据没有 %s，不应凭空产出（缺省项在使用处取默认值）", key)
		}
	}
	if len(leg.Prefs) != len(want) {
		t.Errorf("偏好数量不对：%d 个，期望 %d 个", len(leg.Prefs), len(want))
	}
}

func TestParseLegacyEmptyAndNoise(t *testing.T) {
	leg := ParseLegacy(nil)
	if leg.HasSettings || len(leg.Issues) != 0 || len(leg.App) != 0 || len(leg.Prefs) != 0 || !leg.UI.Equal(DefaultUI()) {
		t.Fatalf("没有旧数据时应当什么都不产出：%+v", leg)
	}
	leg = ParseLegacy(map[string]string{"unrelated": "x", "guizhi-legacy-ls-imported": "1"})
	if leg.HasSettings || len(leg.Prefs) != 0 || len(leg.Issues) != 0 {
		t.Fatalf("无关键不应产出内容：%+v", leg)
	}
}

// 坏数据只记入 Issues（键名与原因），不影响其余键；Issues 里不能带值（值可能含口令）。
func TestParseLegacyBadEntriesAreReportedWithoutValues(t *testing.T) {
	entries := map[string]string{
		"guizhi-settings":               `{"state":{"aiApiKey":"sk-secret" BROKEN`,
		"ui-storage":                    `{"nostate":true}`,
		"guizhi-library-table-config":   `[1,2]`,
		"guizhi-library-smart-views-v1": `{"not":"array"}`,
		"guizhi-content-reading-v1":     `not json sk-secret`,
		"guizhi-snapshot-reading-v1":    `{"ok":1}`,
		"wechat-reader-width":           "huge",
		"guizhi-ask-active-session":     "",
	}
	leg := ParseLegacy(entries)
	if leg.HasSettings {
		t.Error("坏的设置不应被当成已读到")
	}
	if len(leg.Issues) != 5 {
		t.Fatalf("应有 5 条问题（设置、ui-storage、列配置、视图、阅读位置）：%v", leg.Issues)
	}
	for _, msg := range leg.Issues {
		if strings.Contains(msg, "sk-secret") {
			t.Errorf("问题说明里不能带值：%s", msg)
		}
	}
	if string(leg.Prefs[PrefSnapshotReading]) != `{"ok":1}` {
		t.Error("好的键不受坏键影响")
	}
	for _, key := range []string{PrefWechatWidth, PrefAskActiveSession, PrefLibraryColumns, PrefSmartViews, PrefContentReading} {
		if _, ok := leg.Prefs[key]; ok {
			t.Errorf("坏数据不该产出 %s", key)
		}
	}
}

func TestParseLegacyVersionHandling(t *testing.T) {
	standard := func(version string) UI {
		raw := `{"state":{"motionPreference":"standard"}` + version + `}`
		return ParseLegacy(map[string]string{LegacySettingsKey: raw}).UI
	}
	if got := standard(`,"version":2`).MotionPreference; got != MotionSystem {
		t.Errorf("v2 的标准动效应迁移为跟随系统，得到 %s", got)
	}
	if got := standard(`,"version":3`).MotionPreference; got != MotionStandard {
		t.Errorf("v3 起主动选的标准动效应保留，得到 %s", got)
	}
	if got := standard(``).MotionPreference; got != MotionStandard {
		t.Errorf("没有 version 时按当前版本处理（渲染层 migrate 对 undefined 版本不套用旧规则），得到 %s", got)
	}
}

func TestParseLegacyLanguageQuirks(t *testing.T) {
	lang := func(js string) json.RawMessage {
		return ParseLegacy(map[string]string{LegacySettingsKey: `{"state":` + js + `,"version":3}`}).App["language"]
	}
	if got := lang(`{"language":5}`); string(got) != `"en"` {
		t.Errorf("类型错误的 language 渲染层按空串归一为 en，得到 %s", got)
	}
	if got := lang(`{}`); got != nil {
		t.Errorf("缺省的 language 不应产出（让已有值或默认值生效），得到 %s", got)
	}
}

func TestParseLegacyCatalogFlagsAreCapped(t *testing.T) {
	entries := map[string]string{}
	for i := 0; i < maxCatalogFlags+25; i++ {
		entries[fmt.Sprintf("reading-original-catalog:e%04d", i)] = "true"
	}
	leg := ParseLegacy(entries)
	var flags map[string]bool
	if err := json.Unmarshal(leg.Prefs[PrefCatalogOpen], &flags); err != nil {
		t.Fatal(err)
	}
	if len(flags) != maxCatalogFlags {
		t.Fatalf("目录开关应封顶 %d，得到 %d", maxCatalogFlags, len(flags))
	}
	if len(leg.Issues) != 1 {
		t.Errorf("截断要留一条说明：%v", leg.Issues)
	}
	// 确定性：按键名排序后保留靠前的
	if !flags["original:e0000"] || flags[fmt.Sprintf("original:e%04d", maxCatalogFlags+24)] {
		t.Error("截断应当是确定的（保留键名靠前的）")
	}
}

func TestUIStoragePaneWidthsFollowRendererClamp(t *testing.T) {
	// 渲染层 clamp：非有限数（含非数字）取下限；null 与缺省取默认值（不产出）
	parse := func(state string) map[string]json.RawMessage {
		return ParseLegacy(map[string]string{"ui-storage": `{"state":` + state + `,"version":0}`}).Prefs
	}
	got := parse(`{"sidebarPanelWidth":"300","itemListPaneWidth":null,"wikiCatalogPaneWidth":10}`)
	if string(got[PrefSidebarPanelWidth]) != "220" {
		t.Errorf("数字字符串应取下限：%s", got[PrefSidebarPanelWidth])
	}
	if _, ok := got[PrefItemListPaneWidth]; ok {
		t.Error("null 应等同缺省，不产出")
	}
	if string(got[PrefWikiCatalogPaneWidth]) != "220" {
		t.Errorf("低于下限应夹到下限：%s", got[PrefWikiCatalogPaneWidth])
	}
}

func TestKnownLegacyKeysAreSortedAndComplete(t *testing.T) {
	exact, prefixes := KnownLegacyKeys()
	if !sort.StringsAreSorted(exact) {
		t.Error("精确键应排序，输出才稳定")
	}
	for _, key := range []string{LegacySettingsKey, "ui-storage", "guizhi-library-table-config", "guizhi-table-config"} {
		if i := sort.SearchStrings(exact, key); i >= len(exact) || exact[i] != key {
			t.Errorf("缺少 %s", key)
		}
	}
	if len(prefixes) != 2 {
		t.Errorf("动态前缀应有 2 个：%v", prefixes)
	}
}

// 偏好键与窗格范围必须和 native/kit 的 PaneSpec 一致（两处各存一份：kit 不依赖 settings）。
func TestPaneBoundsMatchKit(t *testing.T) {
	specs := map[string]kit.PaneSpec{
		PrefSidebarPanelWidth: kit.PaneSidebar, PrefItemListPaneWidth: kit.PaneItemList, PrefWikiCatalogPaneWidth: kit.PaneWikiCatalog,
	}
	if len(PaneWidths) != len(specs) {
		t.Fatalf("窗格数量不一致：settings %d，kit %d", len(PaneWidths), len(specs))
	}
	for _, b := range PaneWidths {
		spec, ok := specs[b.Key]
		if !ok {
			t.Errorf("kit 没有键 %s 的窗格", b.Key)
			continue
		}
		if spec.Key != b.Key || float64(spec.Default) != b.Default || float64(spec.Min) != b.Min || float64(spec.Max) != b.Max {
			t.Errorf("%s：kit=%+v settings=%+v", b.Key, spec, b)
		}
	}
}
