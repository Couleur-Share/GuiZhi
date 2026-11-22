package settings

import (
	"encoding/json"
	"reflect"
	"testing"
)

func customValues() Values {
	v := Default()
	v.UI.ThemeMode = ThemeDark
	v.UI.FontSize = FontLarge
	v.UI.AutoSave = false
	v.UI.ScenarioModelDefaults = map[string]string{"qa": "model-qa"}
	v.App.Language = LangEn
	v.App.BackupAutoEnabled = false
	v.App.BackupIntervalHours = 72
	v.App.BackupKeepCount = 20
	v.App.NetworkProxy = NetworkProxy{Mode: "manual", Protocol: "socks5", Host: "10.0.0.2", Port: 1080, Username: "u", Password: "proxy-pw", Bypass: "localhost"}
	v.App.FlareSolverr = FlareSolverr{Enabled: true, Connection: "ssh", Port: 9000, SSHHost: "box"}
	return v
}

// 导出的 settings 记录带全部 UI 与 App 键；导入把它盖回去得到同一份设置（往返不丢）。
func TestTransferStateRoundTrips(t *testing.T) {
	want := customValues()
	state, err := TransferState(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"themeMode", "scenarioModelDefaults", "language", "backupKeepCount", "networkProxy", "flareSolverr"} {
		if _, ok := state[key]; !ok {
			t.Errorf("导出记录缺少 %s", key)
		}
	}
	got, err := ImportedValues(Default(), state, PersistedVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Errorf("往返应得到同一份设置：\n got %+v\nwant %+v", got, want)
	}
}

// 文件里没有的键保持本机原值（机器绑定的工具路径、开机自启、壁纸不在导出文件里，不能被默认值冲掉）。
func TestImportedValuesKeepsKeysAbsentFromFile(t *testing.T) {
	base := customValues()
	base.App.YtDlpPath = `C:\tools\yt-dlp.exe`
	base.App.LaunchAtStartup = true
	base.UI.BackgroundImageFileName = "bg.png"

	got, err := ImportedValues(base, map[string]json.RawMessage{
		"themeMode": json.RawMessage(`"light"`),
		"language":  json.RawMessage(`"zh"`),
	}, PersistedVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got.UI.ThemeMode != ThemeLight || got.App.Language != LangZh {
		t.Errorf("文件里的键应生效：%+v %+v", got.UI.ThemeMode, got.App.Language)
	}
	if got.App.YtDlpPath != base.App.YtDlpPath || !got.App.LaunchAtStartup || got.UI.BackgroundImageFileName != "bg.png" {
		t.Errorf("机器绑定项应保持原值：%+v", got)
	}
	if got.UI.FontSize != base.UI.FontSize || got.App.BackupKeepCount != base.App.BackupKeepCount {
		t.Errorf("文件没带的键应保持原值：%+v", got)
	}
}

// 可执行文件路径不导入：文件里写了也不改（只接受文件选择器返回过的值）。
func TestImportedValuesIgnoresExecutablePaths(t *testing.T) {
	base := Default()
	got, err := ImportedValues(base, map[string]json.RawMessage{
		"ytDlpPath":  json.RawMessage(`"C:\\evil\\yt-dlp.exe"`),
		"ffmpegPath": json.RawMessage(`"C:\\evil\\ffmpeg.exe"`),
	}, PersistedVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got.App.YtDlpPath != "" || got.App.FfmpegPath != "" {
		t.Errorf("不应导入可执行文件路径：%+v", got.App)
	}
}

// 类型不对的值按渲染层规则退回（不让一份被手改坏的文件把设置写成非法值）；以文件为准的场景模型表不残留旧项。
func TestImportedValuesNormalizesAndReplacesScenarioDefaults(t *testing.T) {
	base := customValues()
	got, err := ImportedValues(base, map[string]json.RawMessage{
		"backupIntervalHours":   json.RawMessage(`9999`),
		"scenarioModelDefaults": json.RawMessage(`{"summary":"m-sum"}`),
	}, PersistedVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got.App.BackupIntervalHours != MaxBackupIntervalHours {
		t.Errorf("备份间隔应夹到上限：%d", got.App.BackupIntervalHours)
	}
	if want := map[string]string{"summary": "m-sum"}; !reflect.DeepEqual(got.UI.ScenarioModelDefaults, want) {
		t.Errorf("场景模型应以文件为准：%v", got.UI.ScenarioModelDefaults)
	}
}

// 界面布局往返：Prefs → uiLayout → Prefs；导入不带走「上次停在哪个模块」。
func TestTransferLayoutRoundTrips(t *testing.T) {
	prefs := map[string]json.RawMessage{
		PrefAppModule:            json.RawMessage(`"wiki"`),
		PrefSidebarCollapsed:     json.RawMessage(`true`),
		PrefSidebarPanelWidth:    json.RawMessage(`300`),
		PrefItemListPaneWidth:    json.RawMessage(`400`),
		PrefLibraryViewMode:      json.RawMessage(`"list"`),
		PrefLibraryColumns:       json.RawMessage(`{"columns":[{"id":"title","visible":true,"width":300}]}`),
		PrefWikiCatalogPaneWidth: json.RawMessage(`260`),
	}
	layout := TransferLayout(func(key string) (json.RawMessage, bool) { raw, ok := prefs[key]; return raw, ok })
	if _, ok := layout["ui-storage"]; !ok {
		t.Fatalf("应带 ui-storage：%v", layout)
	}
	if _, ok := layout["guizhi-library-table-config"]; !ok {
		t.Fatalf("应带列配置：%v", layout)
	}
	encoded, err := json.Marshal(layout)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	got := ImportedPrefs(back)
	if _, ok := got[PrefAppModule]; ok {
		t.Error("导入配置不应改「上次停在哪个模块」")
	}
	for _, key := range TransferLayoutKeys() {
		if string(got[key]) != string(prefs[key]) {
			t.Errorf("%s：得到 %s，应为 %s", key, got[key], prefs[key])
		}
	}
}

func TestTransferLayoutOmitsAbsentPrefs(t *testing.T) {
	layout := TransferLayout(func(string) (json.RawMessage, bool) { return nil, false })
	if len(layout) != 0 {
		t.Errorf("没有任何偏好时不应写出空壳：%v", layout)
	}
}

// 坏的布局数据被丢弃，不影响其余项；白名单之外的键不进偏好。
func TestImportedPrefsDropsBadAndUnlistedEntries(t *testing.T) {
	got := ImportedPrefs(map[string]json.RawMessage{
		"ui-storage":                  json.RawMessage(`"不是对象"`),
		"guizhi-library-table-config": json.RawMessage(`{"columns":[]}`),
		"guizhi-library-smart-views-v1": json.RawMessage(
			`[{"id":"v1"}]`), // 不在传输白名单里，即使文件里夹带也不写入
	})
	if _, ok := got[PrefLibraryColumns]; !ok {
		t.Error("有效的列配置应保留")
	}
	if _, ok := got[PrefSmartViews]; ok {
		t.Error("白名单之外的偏好不应写入")
	}
	if len(got) != 1 {
		t.Errorf("坏数据应被丢弃：%v", got)
	}
}
