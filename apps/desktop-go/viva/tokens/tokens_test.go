package tokens

import "testing"

func TestHSL(t *testing.T) {
	cases := []struct {
		h, s, l float64
		want    Color
	}{
		{0, 0, 100, Color{255, 255, 255, 1}},
		{0, 0, 0, Color{0, 0, 0, 1}},
		{220, 70, 50, Color{38, 98, 217, 1}}, // 手算：宝蓝预设的中间明度
		{0, 100, 50, Color{255, 0, 0, 1}},
		{120, 100, 25, Color{0, 128, 0, 1}},
	}
	for _, c := range cases {
		if got := HSL(c.h, c.s, c.l); got != c.want {
			t.Errorf("HSL(%v,%v,%v) = %+v，期望 %+v", c.h, c.s, c.l, got, c.want)
		}
	}
}

func TestColKeepsAlphaOnlyWhenTranslucent(t *testing.T) {
	if got := col(0, 0, 100, 1); got.A != 1 {
		t.Errorf("不透明颜色的 alpha 应为 1：%+v", got)
	}
	if got := col(0, 0, 100, 0.4); got.A != 0.4 || got.R != 255 {
		t.Errorf("半透明颜色应保留通道、只改 alpha：%+v", got)
	}
}

func TestOver(t *testing.T) {
	white := RGBA(255, 255, 255, 1)
	if got := RGBA(0, 0, 0, 0.5).Over(white); got != (Color{128, 128, 128, 1}) {
		t.Errorf("黑 50%% 叠在白上应为 128 灰：%+v", got)
	}
	if got := RGBA(10, 20, 30, 1).Over(white); got != (Color{10, 20, 30, 1}) {
		t.Errorf("不透明颜色叠上去不应变化：%+v", got)
	}
}

func TestPresets(t *testing.T) {
	if len(HuePresets) != 6 {
		t.Fatalf("色相预设应为 6 个（§2.1 的矩阵），得到 %d", len(HuePresets))
	}
	if len(FontSizes) != 3 {
		t.Fatalf("字号档应为 3 个，得到 %d", len(FontSizes))
	}
	if got := FindHuePreset("nope"); got.ID != DefaultHuePreset {
		t.Errorf("未知预设应回到默认：%+v", got)
	}
	if got := FindFontSize("nope"); got.ID != DefaultFontSize {
		t.Errorf("未知字号档应回到默认：%+v", got)
	}
	if FindFontSize("medium").Px != BaseFontPx {
		t.Errorf("「中」档根字号应等于 BaseFontPx")
	}
}

func TestModesDiffer(t *testing.T) {
	p := FindHuePreset(DefaultHuePreset)
	light, dark := For(false, false, p.Hue, p.Saturation), For(true, false, p.Hue, p.Saturation)
	if light.Background == dark.Background {
		t.Errorf("明暗两套令牌的背景不应相同：%+v", light.Background)
	}
	glass := For(false, true, p.Hue, p.Saturation)
	if glass.UiShellBg == light.UiShellBg {
		t.Errorf("壁纸模式的外壳底色应与普通模式不同")
	}
	// 主题色相应影响 primary，且不影响 destructive 之类的固定色
	a, b := Light(220, 70), Light(150, 30)
	if a.Primary == b.Primary {
		t.Errorf("不同色相的 primary 不应相同")
	}
	if a.Destructive != b.Destructive {
		t.Errorf("destructive 不随主题色相变化")
	}
}

func TestMotionDurations(t *testing.T) {
	if DurationMs(2, false) != 180 || DurationMs(2, true) != 110 {
		t.Errorf("base 档应为标准 180 ms、减弱 110 ms：%d/%d", DurationMs(2, false), DurationMs(2, true))
	}
}
