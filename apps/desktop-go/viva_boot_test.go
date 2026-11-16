package main

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/screens/shell"
	"guizhi-mygo-shell/viva/tokens"
)

// -viva 在切换为产品默认之前必须被锁在隔离验证里：缺任何一项都要在碰数据之前拒绝。
func TestCheckVivaLaunch(t *testing.T) {
	cases := []struct {
		name                                  string
		nativeOn, bench, dbGiven, userDataGiv bool
		wantErr                               bool
	}{
		{"隔离验证：bench + 显式 db + 显式 userdata", false, true, true, true, false},
		{"缺 -bench", false, false, true, true, true},
		{"缺 -db（会按正式安装规则推断真实数据库）", false, true, false, true, true},
		{"缺 -userdata", false, true, true, false, true},
		{"什么都没给", false, false, false, false, true},
		{"与 -native 同用", true, true, true, true, true},
	}
	for _, c := range cases {
		err := checkVivaLaunch(c.nativeOn, c.bench, c.dbGiven, c.userDataGiv)
		if (err != nil) != c.wantErr {
			t.Errorf("%s：err=%v，期望出错=%v", c.name, err, c.wantErr)
		}
	}
}

// vivaFlagsOf 造一组命令行开关的取值（不经过全局 flag，测试之间互不影响）。
func vivaFlagsOf(module, theme, hue, font string) vivaFlags {
	return vivaFlags{module: &module, theme: &theme, hue: &hue, font: &font}
}

func TestVivaStartPage(t *testing.T) {
	cases := []struct {
		name       string
		module     string
		wantModule shell.Module
		wantPage   shell.Page
		wantErr    string
	}{
		{"知识库", "library", shell.ModuleLibrary, shell.PageHome, ""},
		{"导入", "imports", shell.ModuleImports, shell.PageHome, ""},
		{"前后空白被忽略", " wiki ", shell.ModuleWiki, shell.PageHome, ""},
		{"设置不是模块：落在设置页，rail 仍选知识库", "settings", shell.ModuleLibrary, shell.PageSettings, ""},
		{"写错时带可选值报错，不静默落回默认", "librarry", "", shell.PageHome, "library|inbox|ask|wiki|imports|settings"},
		{"空串也是错误", "", "", shell.PageHome, "未知的 -viva-module"},
	}
	for _, c := range cases {
		m, p, err := vivaFlagsOf(c.module, "", "", "").startPage()
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s：err=%v，期望包含 %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || m != c.wantModule || p != c.wantPage {
			t.Errorf("%s：得到 (%q, %v, %v)，期望 (%q, %v)", c.name, m, p, err, c.wantModule, c.wantPage)
		}
	}
}

func TestVivaAppearanceOverride(t *testing.T) {
	base := kit.DefaultSpec()
	royal := tokens.FindHuePreset("royal-blue")
	teal := tokens.FindHuePreset("teal")
	if royal.Hue == teal.Hue {
		t.Fatal("前提不成立：两个预设的色相应不同")
	}

	if fn, err := vivaFlagsOf("library", "", "", "").appearanceOverride(); fn != nil || err != nil {
		t.Errorf("都没给时应返回 (nil, nil)，得到 (%v, %v)", fn != nil, err)
	}

	cases := []struct {
		name             string
		theme, hue, font string
		want             func(kit.Spec) kit.Spec
	}{
		{"只改明暗", "dark", "", "", func(s kit.Spec) kit.Spec { s.Mode = tokens.ModeDark; return s }},
		{"跟随系统", "system", "", "", func(s kit.Spec) kit.Spec { s.Mode = tokens.ModeSystem; return s }},
		{"预设 ID 取预设的色相与饱和度", "", "teal", "", func(s kit.Spec) kit.Spec { s.Hue, s.Saturation = teal.Hue, teal.Saturation; return s }},
		{"色相,饱和度", "", "120, 55", "", func(s kit.Spec) kit.Spec { s.Hue, s.Saturation = 120, 55; return s }},
		{"只改字号档", "", "", "large", func(s kit.Spec) kit.Spec { s.FontSize = "large"; return s }},
		{"三项叠加", "light", "teal", "small", func(s kit.Spec) kit.Spec {
			s.Mode, s.Hue, s.Saturation, s.FontSize = tokens.ModeLight, teal.Hue, teal.Saturation, "small"
			return s
		}},
	}
	for _, c := range cases {
		fn, err := vivaFlagsOf("library", c.theme, c.hue, c.font).appearanceOverride()
		if err != nil || fn == nil {
			t.Errorf("%s：err=%v fn=%v", c.name, err, fn != nil)
			continue
		}
		if got, want := fn(base), c.want(base); got != want {
			t.Errorf("%s：得到 %+v，期望 %+v", c.name, got, want)
		}
	}

	bad := []struct{ name, theme, hue, font, wantErr string }{
		{"未知明暗", "sepia", "", "", "-viva-theme"},
		{"未知色相预设", "", "magenta", "", "-viva-hue"},
		{"色相缺饱和度", "", "120", "", "-viva-hue"},
		{"色相不是数字", "", "a,b", "", "-viva-hue"},
		{"未知字号档", "", "", "huge", "small|medium|large"},
	}
	for _, c := range bad {
		fn, err := vivaFlagsOf("library", c.theme, c.hue, c.font).appearanceOverride()
		if fn != nil || err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s：期望带 %q 的错误，得到 (%v, %v)", c.name, c.wantErr, fn != nil, err)
		}
	}
}

func TestFormatShortcutLabel(t *testing.T) {
	cases := map[string]string{
		"Alt+Shift+F":            "Alt+Shift+F",
		"CommandOrControl+K":     "Ctrl+K",
		"Control+Shift+Space":    "Ctrl+Shift+Space",
		"CommandOrControl+Alt+Q": "Ctrl+Alt+Q",
	}
	for in, want := range cases {
		if got := formatShortcutLabel(in); got != want {
			t.Errorf("formatShortcutLabel(%q) = %q，期望 %q", in, got, want)
		}
	}
}
