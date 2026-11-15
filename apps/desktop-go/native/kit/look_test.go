package kit

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHSLKnownTokens(t *testing.T) {
	// 与 CSS hsl() 的标准换算一致：白、黑、纯红，以及 globals.css 的 --background（220 14% 96%）。
	// 最后一项手算：C=(1-|2L-1|)s=0.0112，m=L-C/2=0.9544，色相落在 180–240° 扇区，
	// R=m→243.4、G=m+X(=C/3)→244.3、B=m+C→246.2，四舍五入得 (243,244,246)。
	cases := []struct {
		h, s, l float64
		want    ui.Color
	}{
		{0, 0, 100, ui.Color{R: 255, G: 255, B: 255, A: 255}},
		{0, 0, 0, ui.Color{A: 255}},
		{0, 100, 50, ui.Color{R: 255, A: 255}},
		{220, 14, 96, ui.Color{R: 243, G: 244, B: 246, A: 255}},
	}
	for _, c := range cases {
		if got := HSL(c.h, c.s, c.l); got != c.want {
			t.Errorf("HSL(%v,%v,%v) = %+v，期望 %+v", c.h, c.s, c.l, got, c.want)
		}
	}
}

func TestColAlphaIsStraight(t *testing.T) {
	// hsl(0 0% 100% / 0.36)：RGB 保持白色，只缩放 A（非预乘）
	got := col(0, 0, 100, 0.36)
	if got.R != 255 || got.G != 255 || got.B != 255 || got.A != 92 { // 0.36*255=91.8→92
		t.Fatalf("col(0,0,100,0.36) = %+v，期望白色 A=92", got)
	}
	if col(0, 0, 100, 1).A != 255 {
		t.Fatal("alpha 为 1 应不透明")
	}
}

// luminance 与 contrast 是 WCAG 2.x 的相对亮度与对比度公式。
func luminance(c ui.Color) float64 {
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func specFor(p HuePreset, mode Mode) ThemeSpec {
	s := DefaultThemeSpec()
	s.Hue, s.Saturation, s.Mode = p.Hue, p.Saturation, mode
	return s
}

// knownContrastDebt 是渲染层令牌本身就不达 WCAG AA 的组合及其当前对比度的下限（实测值向下取整到 0.1）。
// 令牌由 globals.css 生成，原生侧不偷偷修正以免与渲染层不一致；这里把债务显式登记，保证
// ① 新增的不达标组合会让测试失败；② 已登记的组合不会变得更差；③ 将来改了 globals.css 提高对比度后，
// 登记项变成「已达标」，测试会提示删掉这条登记（见 TestPresetsMeetContrast 的末尾检查）。
// 键为 "预设/明暗/项目"，预设可写 * 表示全部。
var knownContrastDebt = map[string]float64{
	"*/dark/危险按钮文字":        4.3, // 白字压在 0 70% 55% 的红上：4.39
	"green/light/强调背景上的文字": 3.8, // 悬停态：hsl(150 30% 40%) 压在 hsl(150 15% 94%) 上：3.85
	"teal/light/强调背景上的文字":  3.7, // 同上：3.77

	// 对话框按钮（渲染层 ConfirmDialog / UnsavedChangesDialog 的写法，原样照搬）：
	"green/light/对话框确认钮悬停文字": 4.4, // bg-primary/90 压在面板上：4.42
	"teal/light/对话框确认钮悬停文字":  4.3, // 4.34
	"*/dark/对话框不保存钮文字":       4.0, // text-destructive（暗色下是深红）压在暗色面板上：4.06
	"*/dark/对话框不保存钮悬停文字":     3.7, // 3.74
}

// 6 个主题色预设 × 明暗：主按钮（主题色底、PrimaryForeground 字）与正文都要达到 WCAG AA。
// 这些对比度是渲染层令牌本身的属性，见 knownContrastDebt。
func TestPresetsMeetContrast(t *testing.T) {
	if len(HuePresets) != 6 {
		t.Fatalf("预设数 = %d，期望 6（MORANDI_THEMES）", len(HuePresets))
	}
	used := map[string]bool{}
	for _, p := range HuePresets {
		for _, mode := range []Mode{ModeLight, ModeDark} {
			l := NewLook(specFor(p, mode), Env{})
			type check struct {
				what      string
				fg, bg    ui.Color
				threshold float64
			}
			panel := l.solid(l.T.UiPanelStrongBg)
			checks := []check{
				{"主按钮文字", l.T.PrimaryForeground, l.T.Primary, 4.5},
				{"正文", l.T.Foreground, l.T.Background, 7},
				{"卡片正文", l.T.CardForeground, l.T.Card, 7},
				{"次要文字", l.T.MutedForeground, l.T.Background, 4.5},
				{"强调背景上的文字", l.T.AccentForeground, l.T.Accent, 4.5},
				{"危险按钮文字", l.T.DestructiveForeground, l.T.Destructive, 4.5},
				{"对话框说明文字", l.T.MutedForeground, panel, 4.5},
				{"对话框标题文字", l.T.Foreground, panel, 7},
			}
			// 对话框的四种按钮，常态与悬停态各查一次
			for _, d := range []struct {
				what string
				kind dialogButtonKind
			}{{"对话框确认钮", btnPrimary}, {"对话框危险钮", btnDestructive}, {"对话框取消钮", btnSecondary}, {"对话框不保存钮", btnDiscard}} {
				for _, hover := range []bool{false, true} {
					bg, fg, _ := l.dialogButtonColors(d.kind, hover)
					what := d.what + "文字"
					if hover {
						what = d.what + "悬停文字"
					}
					checks = append(checks, check{what, fg, l.solid(bg), 4.5})
				}
			}
			for _, c := range checks {
				got := contrast(c.fg, c.bg)
				if got >= c.threshold {
					continue
				}
				key, floor, ok := debtFor(p.ID, string(mode), c.what)
				if !ok {
					t.Errorf("%s/%s %s 对比度 %.2f < %.1f，且未登记为已知债务", p.ID, mode, c.what, got, c.threshold)
					continue
				}
				used[key] = true
				if got < floor {
					t.Errorf("%s/%s %s 对比度 %.2f 低于登记的下限 %.1f，变得更差了", p.ID, mode, c.what, got, floor)
				}
			}
		}
	}
	for key := range knownContrastDebt {
		if !used[key] {
			t.Errorf("债务登记 %q 已不再触发：对比度已达标，请从 knownContrastDebt 里删掉它", key)
		}
	}
}

func debtFor(preset, mode, what string) (key string, floor float64, ok bool) {
	for _, k := range []string{preset + "/" + mode + "/" + what, "*/" + mode + "/" + what} {
		if v, found := knownContrastDebt[k]; found {
			return k, v, true
		}
	}
	return "", 0, false
}

func TestModeResolution(t *testing.T) {
	spec := DefaultThemeSpec()
	cases := []struct {
		mode       Mode
		systemDark bool
		wantDark   bool
	}{
		{ModeSystem, false, false},
		{ModeSystem, true, true},
		{ModeLight, true, false}, // 显式浅色不受系统影响
		{ModeDark, false, true},
		{"", true, true}, // 未知值按跟随系统处理
	}
	for _, c := range cases {
		spec.Mode = c.mode
		l := NewLook(spec, Env{SystemDark: c.systemDark})
		if l.Dark != c.wantDark || l.Theme.Dark != c.wantDark {
			t.Errorf("mode=%q systemDark=%v：Dark=%v，期望 %v", c.mode, c.systemDark, l.Dark, c.wantDark)
		}
	}
}

func TestHueOnlyMovesAccentFamily(t *testing.T) {
	a := NewLook(specFor(FindHuePreset("royal-blue"), ModeLight), Env{})
	b := NewLook(specFor(FindHuePreset("green"), ModeLight), Env{})
	if a.Accent == b.Accent || a.T.Primary == b.T.Primary || a.T.Ring == b.T.Ring {
		t.Fatal("不同色相的强调色应不同")
	}
	if a.Background != b.Background || a.Text != b.Text || a.Border != b.Border || a.T.Card != b.T.Card {
		t.Fatal("色相只应影响强调色族，不影响中性色")
	}
}

func TestSystemAccentDoesNotOverrideAppHue(t *testing.T) {
	// 系统强调色是红色，应用主题色是蓝色：以应用为准
	spec := specFor(FindHuePreset("royal-blue"), ModeLight)
	plain := NewLook(spec, Env{})
	withSys := NewLook(spec, Env{Prefs: ui.Preferences{Accent: ui.RGB(255, 0, 0)}})
	if withSys.Accent != plain.Accent || withSys.Accent != plain.T.Primary {
		t.Fatalf("系统强调色覆盖了应用主题色：%+v vs %+v", withSys.Accent, plain.Accent)
	}
}

func TestPreferencesAreFollowed(t *testing.T) {
	spec := DefaultThemeSpec()
	plain := NewLook(spec, Env{})
	hc := NewLook(spec, Env{Prefs: ui.Preferences{HighContrast: true}})
	if contrast(hc.Border, hc.Background) <= contrast(plain.Border, plain.Background) {
		t.Fatal("高对比度下边框应更醒目")
	}
	if contrast(hc.TextMuted, hc.Background) <= contrast(plain.TextMuted, plain.Background) {
		t.Fatal("高对比度下次要文字应更醒目")
	}
	big := NewLook(spec, Env{Prefs: ui.Preferences{TextScale: 1.5}})
	if want := plain.FontSize * 1.5; math.Abs(float64(big.FontSize-want)) > 0.01 {
		t.Fatalf("文字缩放 1.5 后 FontSize = %v，期望 %v", big.FontSize, want)
	}
}

func TestFontSizeZoom(t *testing.T) {
	want := map[string]float32{"small": 14.0 / 16, "medium": 1, "large": 18.0 / 16}
	for id, z := range want {
		spec := DefaultThemeSpec()
		spec.FontSize = id
		if got := NewLook(spec, Env{}).Zoom; math.Abs(float64(got-z)) > 1e-6 {
			t.Errorf("%s 档 Zoom = %v，期望 %v", id, got, z)
		}
	}
	spec := DefaultThemeSpec()
	spec.FontSize = "no-such-size"
	if got := NewLook(spec, Env{}).Zoom; got != 1 {
		t.Errorf("未知字号档应回落默认档，Zoom = %v", got)
	}
}

func TestGlassModeTokens(t *testing.T) {
	spec := DefaultThemeSpec()
	plain := NewLook(spec, Env{})
	if plain.Glass || plain.T.FillCard.A != 255 {
		t.Fatalf("无壁纸时面板底色应不透明：Glass=%v A=%d", plain.Glass, plain.T.FillCard.A)
	}
	spec.Wallpaper = &Wallpaper{Opacity: 1}
	light := NewLook(spec, Env{})
	if !light.Glass {
		t.Fatal("有壁纸应进入玻璃模式")
	}
	// globals.css：.app-background-mode-image .bg-card { background-color: hsl(var(--card) / 0.22) }
	if light.T.FillCard.A != 56 { // 0.22*255=56.1
		t.Errorf("浅色玻璃 FillCard.A = %d，期望 56", light.T.FillCard.A)
	}
	spec.Mode = ModeDark
	dark := NewLook(spec, Env{})
	if dark.T.FillCard.A != 77 { // 0.30*255=76.5→77
		t.Errorf("深色玻璃 FillCard.A = %d，期望 77", dark.T.FillCard.A)
	}
	// 玻璃模式下描边是半透明白线，不是实色
	if light.T.UiPanelBorder.A == 255 {
		t.Error("玻璃描边应半透明")
	}
}

func TestShadowToken(t *testing.T) {
	var zero ShadowToken
	if !zero.IsZero() {
		t.Fatal("零值阴影应为无")
	}
	l := NewLook(DefaultThemeSpec(), Env{})
	if l.T.ShadowSm.IsZero() || l.T.ShadowSm.Blur != 2 || l.T.ShadowSm.Y != 1 {
		t.Fatalf("浅色 --shadow-sm 应为 0 1px 2px：%+v", l.T.ShadowSm)
	}
}

func TestDefaultsComeFromSettings(t *testing.T) {
	spec := DefaultThemeSpec()
	p := FindHuePreset(DefaultHuePreset)
	if spec.Hue != p.Hue || spec.Saturation != p.Saturation || spec.FontSize != DefaultFontSize || spec.Mode != ModeSystem || spec.Motion != MotionSystem {
		t.Fatalf("默认外观不对：%+v", spec)
	}
	if FindHuePreset("custom-unknown").ID != DefaultHuePreset {
		t.Fatal("未知预设应回落默认预设")
	}
}
