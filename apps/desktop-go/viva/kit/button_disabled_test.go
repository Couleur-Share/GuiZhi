package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 本文件守住台账 C-008 / B-058：禁用按钮的文字按渲染层的「整层淡出」取色。渲染层把整枚按钮合成一层再以 N 淡出，
// 文字像素 = N × 文字色 + (1 − N) × 面板色；Viva 逐色淡出，文字会再叠在已淡出的底色上，比渲染层淡得多。

// disabledInk 的取值：用手算的数字写死期望，不复用被测公式。
func TestDisabledInk(t *testing.T) {
	white := tokens.RGBA(255, 255, 255, 1)
	primary := tokens.RGBA(59, 130, 246, 1)
	panel := tokens.RGBA(100, 100, 100, 1)
	cases := []struct {
		name       string
		fg, bg     tokens.Color
		opacity    float32
		surface    tokens.Color
		want       tokens.Color
		wantFaded  bool // 期望退回逐色淡出（alpha 乘 N、通道不变）
		fadedAlpha float32
	}{
		{name: "白字、白面板：0.4×白 + 0.6×白 仍是白（C-008 的证据数字）", fg: white, bg: primary, opacity: 0.4, surface: white, want: white},
		{name: "不透明的字与底：N×字 + (1−N)×面板", fg: tokens.RGBA(200, 100, 0, 1), bg: tokens.RGBA(0, 100, 200, 1), opacity: 0.5, surface: panel, want: tokens.RGBA(150, 100, 50, 1)},
		{name: "不透明的字、半透明的底：字形实心处与底色无关", fg: tokens.RGBA(200, 100, 0, 1), bg: tokens.RGBA(0, 100, 200, 0.5), opacity: 0.5, surface: panel, want: tokens.RGBA(150, 100, 50, 1)},
		{
			name: "半透明的字叠在不透明的底上：先在层内合成，再淡出",
			fg:   tokens.RGBA(200, 100, 0, 0.5), bg: tokens.RGBA(0, 100, 200, 1), opacity: 0.5, surface: tokens.RGBA(40, 80, 120, 1),
			want: tokens.RGBA(70, 90, 110, 1), // 层 = (100,100,100)，N×层 + (1−N)×面板
		},
		{
			name: "半透明的字与半透明的底：预乘合成",
			fg:   tokens.RGBA(200, 100, 0, 0.5), bg: tokens.RGBA(0, 100, 200, 0.5), opacity: 0.5, surface: panel,
			want: tokens.RGBA(113, 100, 88, 1), // 层 α = 0.75，预乘 (100,75,50)；112.5 与 87.5 四舍五入
		},
		{name: "没有底色：逐色淡出，不依赖面板", fg: white, bg: tokens.Transparent, opacity: 0.4, surface: panel, wantFaded: true, fadedAlpha: 0.4},
		{name: "不知道面板（零值）：逐色淡出", fg: white, bg: primary, opacity: 0.4, surface: tokens.Color{}, wantFaded: true, fadedAlpha: 0.4},
		{name: "半透明的面板（壁纸模式）：逐色淡出", fg: white, bg: primary, opacity: 0.4, surface: tokens.RGBA(0, 0, 0, 0.42), wantFaded: true, fadedAlpha: 0.4},
	}
	for _, c := range cases {
		got := disabledInk(c.fg, c.bg, c.opacity, c.surface)
		want := c.want
		if c.wantFaded {
			want = c.fg.WithAlpha(c.fg.A * c.fadedAlpha)
		}
		if got != want {
			t.Errorf("%s：得到 %+v，期望 %+v", c.name, got, want)
		}
	}
}

// disabledPage 在 spec 下画一枚按钮，返回无头会话。
func disabledPage(t *testing.T, spec Spec, button ButtonSpec, style func(l *Look) ButtonStyle) *harness {
	t.Helper()
	return openPage(t, spec, 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Button(c.Key("b"), button, style(l))
		})
	})
}

// solidStyle 是渲染层的 bg-primary text-primary-foreground 按钮；surface 由用例给。
func solidStyle(l *Look, surface tokens.Color) ButtonStyle {
	t := l.T
	return ButtonStyle{
		Height: 32, PadX: 12, Radius: 8, Text: TextSM, Weight: WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, DisabledOpacity: 0.4, Surface: surface,
	}
}

// 禁用且有底色、给了不透明的面板色：文字取整层淡出的合成色；其余情形（启用、没给面板、半透明的面板、背后透明的按钮）不变。
// 浅色与深色各走一遍：两个主题的主色、面板色都不同。
func TestDisabledButtonLabelFollowsTheLayerComposite(t *testing.T) {
	modes := []struct {
		name string
		mode tokens.Mode
	}{{"浅色", tokens.ModeLight}, {"深色", tokens.ModeDark}}
	for _, m := range modes {
		spec := still()
		spec.Mode = m.mode
		look := NewLook(spec)
		tk := look.T
		// 期望由「文字叠在面板上、不透明度 N」独立算出（Color.Over），不经过被测函数。
		layered := tk.PrimaryForeground.WithAlpha(0.4).Over(tk.Background)
		faded := tk.PrimaryForeground.WithAlpha(tk.PrimaryForeground.A * 0.4)

		label := func(t *testing.T, button ButtonSpec, style func(l *Look) ButtonStyle) gfx.Color {
			t.Helper()
			h := disabledPage(t, spec, button, style)
			got, ok := textColorOf(h, button.Label)
			if !ok {
				t.Fatalf("没有找到文字 %q", button.Label)
			}
			return got
		}
		t.Run(m.name+"：禁用、有底色、给了面板", func(t *testing.T) {
			got := label(t, ButtonSpec{Label: "添加", Disabled: true}, func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background) })
			if want := look.Col(layered); got != want {
				t.Errorf("文字色 %+v，期望整层淡出的合成色 %+v", got, want)
			}
		})
		t.Run(m.name+"：启用时文字不变", func(t *testing.T) {
			got := label(t, ButtonSpec{Label: "添加"}, func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background) })
			if want := look.Col(tk.PrimaryForeground); got != want {
				t.Errorf("文字色 %+v，期望 %+v", got, want)
			}
		})
		t.Run(m.name+"：没给面板，沿用逐色淡出", func(t *testing.T) {
			got := label(t, ButtonSpec{Label: "添加", Disabled: true}, func(l *Look) ButtonStyle { return solidStyle(l, tokens.Color{}) })
			if want := look.Col(faded); got != want {
				t.Errorf("文字色 %+v，期望逐色淡出 %+v", got, want)
			}
		})
		t.Run(m.name+"：面板是半透明的（壁纸模式），沿用逐色淡出", func(t *testing.T) {
			got := label(t, ButtonSpec{Label: "添加", Disabled: true}, func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background.WithAlpha(0.5)) })
			if want := look.Col(faded); got != want {
				t.Errorf("文字色 %+v，期望逐色淡出 %+v", got, want)
			}
		})
		t.Run(m.name+"：背后透明的按钮，给了面板也逐色淡出", func(t *testing.T) {
			got := label(t, ButtonSpec{Label: "添加", Disabled: true}, func(l *Look) ButtonStyle {
				st := solidStyle(l, l.T.Background)
				st.Bg = tokens.Transparent
				return st
			})
			if want := look.Col(faded); got != want {
				t.Errorf("文字色 %+v，期望逐色淡出 %+v", got, want)
			}
		})
	}
}

// 后缀文字（批量按钮的计数）与标签一样是按钮层的一部分，禁用时同样按整层淡出取色；启用时不变。
func TestDisabledButtonSuffixFollowsTheLayerComposite(t *testing.T) {
	spec := still()
	look := NewLook(spec)
	tk := look.T
	suffix := tokens.RGBA(220, 60, 40, 1)
	style := func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background) }
	read := func(disabled bool) gfx.Color {
		h := disabledPage(t, spec, ButtonSpec{Label: "添加", Suffix: "12", SuffixColor: suffix, Disabled: disabled}, style)
		got, ok := textColorOf(h, "12")
		if !ok {
			t.Fatal("没有找到后缀文字")
		}
		return got
	}
	if want, got := look.Col(suffix.WithAlpha(0.4).Over(tk.Background)), read(true); got != want {
		t.Errorf("禁用时后缀色 %+v，期望整层淡出的合成色 %+v", got, want)
	}
	if want, got := look.Col(suffix), read(false); got != want {
		t.Errorf("启用时后缀色 %+v，期望不变 %+v", got, want)
	}
}

// 确认框载入中（两枚按钮禁用）：文字按整层淡出、面板色取对话框面板的底（UiPanelStrongBg）；壁纸模式下面板是半透明的，退回逐色淡出。
func TestConfirmLoadingButtonsFollowTheLayerComposite(t *testing.T) {
	type tone struct {
		name  string
		mode  tokens.Mode
		glass bool
	}
	tones := []tone{{"浅色", tokens.ModeLight, false}, {"深色", tokens.ModeDark, false}, {"壁纸浅色", tokens.ModeLight, true}}
	for _, tn := range tones {
		spec := still()
		spec.Mode, spec.Glass = tn.mode, tn.glass
		look := NewLook(spec)
		tk := look.T
		// 取消：文字 foreground、禁用 0.5；确认：文字白、禁用 0.6。
		wantCancel := tk.Foreground.WithAlpha(tk.Foreground.A * 0.5)
		wantOK := tokens.White.WithAlpha(0.6)
		if tk.UiPanelStrongBg.A >= 1 {
			wantCancel = tk.Foreground.WithAlpha(0.5).Over(tk.UiPanelStrongBg)
			wantOK = tokens.White.WithAlpha(0.6).Over(tk.UiPanelStrongBg)
		}
		t.Run(tn.name, func(t *testing.T) {
			h, _ := openConfirmPage(t, spec, func(p *confirmPage) { p.loading = true })
			openConfirm(h)
			if got, ok := textColorOf(h, confirmCancel); !ok || got != look.Col(wantCancel) {
				t.Errorf("取消的文字色 %+v（找到=%v），期望 %+v", got, ok, look.Col(wantCancel))
			}
			if got, ok := textColorOf(h, confirmOK); !ok || got != look.Col(wantOK) {
				t.Errorf("确认的文字色 %+v（找到=%v），期望 %+v", got, ok, look.Col(wantOK))
			}
		})
	}
}
