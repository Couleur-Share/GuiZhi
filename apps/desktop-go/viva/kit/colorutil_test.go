package kit

import (
	"math"
	"testing"
)

// 本文件照搬渲染层 tests/unit/components/color-utils.test.ts 的全部用例：取色器的颜色换算必须与渲染层逐位一致。

func TestNormalizeHex(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"#F0A", "#ff00aa"}, // 三位简写补齐成六位，统一带 # 且小写
		{"abc", "#aabbcc"},
		{"  #3B82F6  ", "#3b82f6"},
	} {
		got, ok := NormalizeHex(c.in)
		if !ok || got != c.want {
			t.Errorf("NormalizeHex(%q) = %q, %v，期望 %q", c.in, got, ok, c.want)
		}
	}
	for _, in := range []string{"", "#12345", "rgb(0,0,0)", "#gggggg"} { // 认不出的输入返回失败，不猜
		if got, ok := NormalizeHex(in); ok {
			t.Errorf("NormalizeHex(%q) = %q，期望失败", in, got)
		}
	}
}

func TestHexHSVRoundTripDoesNotDrift(t *testing.T) {
	for _, hex := range []string{"#000000", "#ffffff", "#ff0000", "#00ff00", "#0000ff", "#3b82f6", "#7f7f7f"} {
		hsv, ok := HexToHSV(hex)
		if !ok {
			t.Fatalf("HexToHSV(%q) 失败", hex)
		}
		if got := HSVToHex(hsv); got != hex {
			t.Errorf("往返漂移：%s → %+v → %s", hex, hsv, got)
		}
	}
}

func TestHueFallsInItsSector(t *testing.T) {
	for hex, want := range map[string]float64{
		"#ff0000": 0, "#ffff00": 60, "#00ff00": 120, "#00ffff": 180, "#0000ff": 240, "#ff00ff": 300,
	} {
		hsv, _ := HexToHSV(hex)
		if got := jsRound(hsv.H); got != want {
			t.Errorf("%s 的色相 = %v，期望 %v", hex, got, want)
		}
	}
}

func TestGrayHasZeroSaturationAndBlackZeroValue(t *testing.T) {
	if got := rgbToHSV(rgb{128, 128, 128}).S; got != 0 {
		t.Errorf("灰阶饱和度 = %v，期望 0", got)
	}
	if got := rgbToHSV(rgb{0, 0, 0}); got != (HSV{}) {
		t.Errorf("黑色 = %+v，期望全 0", got)
	}
}

func TestOutOfRangeHSVIsNormalisedBeforeConversion(t *testing.T) {
	for _, c := range []struct {
		in   HSV
		want string
	}{
		{HSV{360, 1, 1}, "#ff0000"},
		{HSV{-120, 1, 1}, "#0000ff"},
		{HSV{0, 5, 5}, "#ff0000"},
	} {
		if got := HSVToHex(c.in); got != c.want {
			t.Errorf("HSVToHex(%+v) = %s，期望 %s", c.in, got, c.want)
		}
	}
}

func TestUnparseableHexIsNotConverted(t *testing.T) {
	if _, ok := HexToHSV("nope"); ok {
		t.Error("HexToHSV(\"nope\") 应当失败")
	}
}

// 渲染层 clamp：NaN 取下限。
func TestClampHandlesNaN(t *testing.T) {
	if got := clampUnit(math.NaN(), 0, 1); got != 0 {
		t.Errorf("clampUnit(NaN) = %v，期望下限 0", got)
	}
}
