package main

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/screens/shell"
	"guizhi-mygo-shell/viva/tokens"
)

// Viva 是产品默认：不带任何开关就是 Viva；显式 -native / -shot 走各自的旧路径；显式 -viva=false 回退旧渲染层。
func TestVivaActive(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name           string
		viva           *bool
		nativeOn, shot bool
		want           bool
	}{
		{"缺省（产品默认）", &on, false, false, true},
		{"-bench 等自动化也是 Viva（缺省开）", &on, false, false, true},
		{"显式 -viva=false 回退旧渲染层", &off, false, false, false},
		{"显式 -native 走原生开发路径（pnpm shot:native）", &on, true, false, false},
		{"显式 -shot 走网页截图（旧渲染层）", &on, false, true, false},
		{"-native -viva=false 也不是 Viva", &off, true, false, false},
		{"flags 未声明（nil）按关闭处理", nil, false, false, false},
	}
	for _, c := range cases {
		if got := (vivaFlags{on: c.viva}).active(c.nativeOn, c.shot); got != c.want {
			t.Errorf("%s：active=%v，期望 %v", c.name, got, c.want)
		}
	}
}

// 开关组合冲突要在碰数据之前拒绝；只有「显式给了 -viva（为真）」才算冲突，缺省的 -viva=true 不与 -native / -shot 冲突。
func TestCheckVivaLaunch(t *testing.T) {
	cases := []struct {
		name                                    string
		vivaOn, vivaExplicit, nativeOn, hasShot bool
		wantErr                                 string
	}{
		{"缺省启动", true, false, false, false, ""},
		{"自动化 -bench 缺省", true, false, false, false, ""},
		{"显式 -viva 单独使用", true, true, false, false, ""},
		{"显式 -viva=false", false, true, false, false, ""},
		{"缺省 + -native：走 native，不冲突", true, false, true, false, ""},
		{"缺省 + -shot：走网页截图，不冲突", true, false, false, true, ""},
		{"-viva=false + -native", false, true, true, false, ""},
		{"-viva=false + -shot", false, true, false, true, ""},
		{"显式 -viva 与 -native", true, true, true, false, "-native 与 -viva 不能同时使用"},
		{"显式 -viva 与 -shot", true, true, false, true, "-viva 不使用 -shot 网页截图参数（无头截图请用 vivaparity）"},
	}
	for _, c := range cases {
		err := checkVivaLaunch(c.vivaOn, c.vivaExplicit, c.nativeOn, c.hasShot)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s：不该出错，实际 %v", c.name, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%s：err=%v，期望 %q", c.name, err, c.wantErr)
		}
	}
}

// 外壳生命周期：自动化实例只在显式 -viva-shell 时接（保持 measure-idle 与 smoke-viva-shell 的语义）；真实启动默认接，显式 =false 才关。
func TestVivaShellActive(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name       string
		shell      *bool
		automation bool
		given      bool
		want       bool
	}{
		{"自动化 + 未给 -viva-shell：不接", &off, true, false, false},
		{"自动化 + 显式 -viva-shell：接", &on, true, true, true},
		{"自动化 + 显式 -viva-shell=false：不接", &off, true, true, false},
		{"真实启动 + 未给：默认接", &off, false, false, true},
		{"真实启动 + 显式 -viva-shell：接", &on, false, true, true},
		{"真实启动 + 显式 -viva-shell=false：关闭", &off, false, true, false},
		{"flags 未声明（nil）+ 自动化：不接", nil, true, false, false},
	}
	for _, c := range cases {
		if got := (vivaFlags{shell: c.shell}).shellActiveGiven(c.automation, c.given); got != c.want {
			t.Errorf("%s：shellActive=%v，期望 %v", c.name, got, c.want)
		}
	}
}

// 窗口类名是正式产品名，且必须与旧版 React 壳的 WebView2 目录（%LOCALAPPDATA%\GuiZhi\WebView2）隔离：
// 隔离既保护首次启动只读导入旧 localStorage，也支持回退。
func TestVivaWindowClassIsolatedFromLegacyProfile(t *testing.T) {
	if vivaWindowClass != "guizhi-viva" {
		t.Fatalf("窗口类名 = %q，必须保持 guizhi-viva", vivaWindowClass)
	}
	if strings.EqualFold(vivaWindowClass, webview2AppName) {
		t.Fatalf("窗口类名不能等于旧版 WebView2 应用名 %q（会指向旧渲染层的配置）", webview2AppName)
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
