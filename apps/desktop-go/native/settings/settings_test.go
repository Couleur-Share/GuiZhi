package settings

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func raw(t *testing.T, js string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatalf("测试数据不是 JSON：%v", err)
	}
	return m
}

func TestDefaultsAreAlreadyNormalized(t *testing.T) {
	v := Default()
	before := v.Clone()
	v.Normalize()
	if !v.Equal(before) {
		t.Fatalf("默认值经归一化后变了：\n前 %+v\n后 %+v", before, v)
	}
}

func TestDecodeUIEmptyIsDefault(t *testing.T) {
	for _, in := range []map[string]json.RawMessage{nil, {}, raw(t, `{"unknown":1}`)} {
		if got := DecodeUI(in, 3); !got.Equal(DefaultUI()) {
			t.Errorf("空输入应得到默认值：%+v", got)
		}
	}
}

// 已下线的同步字段（含明文凭据）不能被带进来，也不能出现在写回文件的内容里。
func TestDecodeUIDropsRetiredKeysAndSecrets(t *testing.T) {
	in := raw(t, `{"webdavPassword":"plaintext-secret","s3SecretAccessKey":"plaintext-secret","aiApiKey":"sk-secret",
		"aiProviders":[{"apiKey":"sk-secret"}],"themeColor":"green"}`)
	got := DecodeUI(in, 2)
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "webdav") {
		t.Fatalf("界面设置里不该有凭据或已下线字段：%s", encoded)
	}
	if got.ThemeColor != "green" {
		t.Errorf("已知字段照常解码：%q", got.ThemeColor)
	}
}

// 类型不对的字段退回默认值，而不是整份作废或带着错类型往下走。
func TestDecodeUIWrongTypesFallBackToDefaults(t *testing.T) {
	in := raw(t, `{"renderMarkdown":"yes","autoSave":null,"themeHue":"red","themeColor":5,"fontSize":["large"],
		"backgroundImageOpacity":"0.5","scenarioModelDefaults":"x","debugMode":1,"closeAction":{"a":1}}`)
	got := DecodeUI(in, 3)
	want := DefaultUI()
	if !got.Equal(want) {
		t.Fatalf("类型错误的字段应全部取默认值：\n%+v", got)
	}
}

func TestNormalizeExtrasKeepNativeLookValid(t *testing.T) {
	u := DefaultUI()
	u.ThemeHue, u.ThemeSaturation = 400, -20
	u.CustomThemeHex = "blue"
	u.ThemeColor = ""
	u.Normalize()
	if u.ThemeHue != 360 || u.ThemeSaturation != 0 || u.CustomThemeHex != DefaultCustomThemeHex || u.ThemeColor != DefaultThemeColor {
		t.Fatalf("坏值应收敛到合法范围：%+v", u)
	}
	for _, hex := range []string{"#3b82f6", "#ABCDEF", "#000000"} {
		u.CustomThemeHex = hex
		u.Normalize()
		if u.CustomThemeHex != hex {
			t.Errorf("合法的自定义色不该被改：%s → %s", hex, u.CustomThemeHex)
		}
	}
	// 0,0 是合法的灰色主题，不能被当成缺省
	u.ThemeHue, u.ThemeSaturation = 0, 0
	u.Normalize()
	if u.ThemeHue != 0 || u.ThemeSaturation != 0 {
		t.Errorf("灰色主题 (0,0) 被改了：%v,%v", u.ThemeHue, u.ThemeSaturation)
	}
}

func TestNormalizeBackgroundFileName(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"  ":                           "",
		"a.png":                        "a.png",
		" a.png ":                      "a.png",
		"local-image://a%20b.png":      "a b.png",
		"local-image://%E5%9B%BE.png":  "图.png",
		"local-image://%E0%A4%A":       "%E0%A4%A", // 百分号序列非法：用原文
		"local-image://%FF.png":        "%FF.png",  // 解出非法 UTF-8：JS 抛 URIError，用原文
		"https://example.com/a.png":    "",
		"HTTP://example.com/a.png":     "",
		"data:image/png;base64,AAAA":   "",
		"blob:abc":                     "",
		"../a.png":                     "",
		"a..b.png":                     "",
		"local-image://%2e%2e%2fx.png": "",
		"dir/a.png":                    "",
		"dir\\a.png":                   "",
		"a?.png":                       "",
		"a#1.png":                      "",
		"a\x00b.png":                   "",
		"local-image://":               "",
		"\u00a0壁纸.png\u3000":           "壁纸.png",
		"\ufeffa.png":                  "a.png",
		"\u0085a.png":                  "\u0085a.png", // NEL 不是 JS 空白
	}
	for in, want := range cases {
		if got := NormalizeBackgroundFileName(in); got != want {
			t.Errorf("NormalizeBackgroundFileName(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestToFixed1MatchesJS(t *testing.T) {
	// 期望值来自 node：Number(x.toFixed(1))
	cases := [][2]float64{
		{0.25, 0.3}, {0.15, 0.1}, {0.35, 0.3}, {2.5, 2.5}, {14.25, 14.3}, {33.35, 33.4}, {0.05, 0.1}, {1.45, 1.4},
		{50, 50}, {0, 0}, {7, 7}, {0.04, 0}, {0.049999999999999996, 0}, {12.345, 12.3}, {49.95, 50}, {0.5, 0.5},
		{0.45, 0.5}, {0.95, 0.9}, {1.05, 1.1}, {2.675, 2.7}, {10.15, 10.2}, {3.35, 3.4},
	}
	for _, c := range cases {
		if got := toFixed1(c[0]); got != c[1] {
			t.Errorf("toFixed1(%v) = %v，期望 %v", c[0], got, c[1])
		}
	}
}

func TestClampBackup(t *testing.T) {
	cases := []struct {
		in               float64
		fallback, lo, hi int
		want             int
	}{
		{24, 24, 1, 168, 24}, {0, 24, 1, 168, 1}, {-5, 24, 1, 168, 1}, {1000, 24, 1, 168, 168},
		{6.5, 24, 1, 168, 7}, {6.4, 24, 1, 168, 6}, {2.5, 10, 1, 100, 3}, {-0.5, 10, 1, 100, 1},
	}
	for _, c := range cases {
		if got := ClampBackup(c.in, c.fallback, c.lo, c.hi); got != c.want {
			t.Errorf("ClampBackup(%v) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

func TestDecodeAppFromTableRows(t *testing.T) {
	rows := raw(t, `{"language":"zh-CN","launchAtStartup":true,"backupIntervalHours":0,"backupKeepCount":500,
		"enableNotifications":"no","ytDlpPath":"C:\\x\\yt-dlp.exe","unknown":1,"networkProxy":{"host":"h"}}`)
	got := DecodeApp(rows)
	if got.Language != LangZh || !got.LaunchAtStartup || got.BackupIntervalHours != 1 || got.BackupKeepCount != 100 {
		t.Fatalf("解码结果不对：%+v", got)
	}
	if !got.EnableNotifications {
		t.Error("类型错误的 enableNotifications 应取默认值 true")
	}
	if got.YtDlpPath != `C:\x\yt-dlp.exe` {
		t.Errorf("工具路径应原样带出：%q", got.YtDlpPath)
	}
	if got.NetworkProxy != DefaultProxy() {
		t.Errorf("代理由调用方用 Go 侧归一化函数填入，这里应保持默认：%v", got.NetworkProxy)
	}
}

func TestChangedKeysAndClone(t *testing.T) {
	a := DefaultUI()
	b := a.Clone()
	b.ThemeMode = ThemeDark
	b.ScenarioModelDefaults["qa"] = "m1"
	changed, err := ChangedKeys(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 || string(changed["themeMode"]) != `"dark"` || string(changed["scenarioModelDefaults"]) != `{"qa":"m1"}` {
		t.Fatalf("变更键不对：%v", changed)
	}
	if len(a.ScenarioModelDefaults) != 0 {
		t.Fatal("Clone 必须深拷贝 map")
	}
	if !a.Equal(a.Clone()) {
		t.Error("克隆应与原值相等")
	}
	var empty UI = DefaultUI()
	empty.ScenarioModelDefaults = nil
	if !empty.Equal(DefaultUI()) {
		t.Error("nil map 与空 map 应相等")
	}
	same, err := ChangedKeys(a, a)
	if err != nil || len(same) != 0 {
		t.Errorf("相同的值不该有变更：%v %v", same, err)
	}
}

// 代理口令不能经日志或 %v 流出去。
func TestNetworkProxyNeverPrintsPassword(t *testing.T) {
	p := DefaultProxy()
	p.Password = "p@ss-秘密"
	p.Username = "me"
	app := DefaultApp()
	app.NetworkProxy = p
	for _, s := range []string{
		fmt.Sprint(p), fmt.Sprintf("%v", p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p),
		fmt.Sprintf("%v", app), fmt.Sprintf("%+v", app), fmt.Sprintf("%v", Values{App: app}),
	} {
		if strings.Contains(s, "p@ss") || strings.Contains(s, "秘密") {
			t.Fatalf("口令出现在了格式化输出里：%s", s)
		}
	}
	if !strings.Contains(fmt.Sprint(p), "password=***") {
		t.Errorf("有口令时应显示 ***：%s", p)
	}
	if strings.Contains(DefaultProxy().String(), "***") {
		t.Error("没有口令时不该显示 ***")
	}
}

func TestScenarioDefaultsNormalization(t *testing.T) {
	in := map[string]string{"summary": " m1 ", "qa": "", "bogus": "x", "ocr": "\u3000o\u00a0"}
	got := NormalizeScenarioDefaults(in)
	if len(got) != 2 || got["summary"] != "m1" || got["ocr"] != "o" {
		t.Fatalf("场景默认模型归一化不对：%v", got)
	}
	if got := NormalizeScenarioDefaults(nil); got == nil || len(got) != 0 {
		t.Errorf("nil 输入应得到空 map，而不是 nil：%v", got)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]Language{"zh": LangZh, "en": LangEn, "zh-CN": LangZh, "ZH-tw": LangZh, "en-GB": LangEn, "fr": LangEn, "": LangEn}
	for in, want := range cases {
		if got := NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q，期望 %q", in, got, want)
		}
	}
}
