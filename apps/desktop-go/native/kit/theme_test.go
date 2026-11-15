package kit

import (
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

func TestThemeDefaultsAndDarkness(t *testing.T) {
	light, dark := Theme(ThemeSpec{}), Theme(ThemeSpec{Dark: true})
	if light.Dark || !dark.Dark {
		t.Fatal("Dark 标志不对")
	}
	if light.Accent == dark.Accent || light.Background == dark.Background {
		t.Fatal("明暗两套应不同")
	}
	// 自定义色相只改强调色，不改中性色
	custom := Theme(ThemeSpec{Hue: 150, Saturation: 50})
	if custom.Accent == light.Accent || custom.Background != light.Background {
		t.Fatal("色相应只影响强调色族")
	}
}
