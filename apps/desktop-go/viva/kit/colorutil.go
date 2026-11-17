package kit

import (
	"math"
	"strconv"
	"strings"
)

// 取色器用的颜色换算（渲染层 components/ui/color-utils.ts 的逐步照搬）：
// HSV / RGB / 十六进制之间互转，边界（三位简写、越界分量、往返漂移）与渲染层一致，由 colorutil_test.go 里照搬的用例固定。

// HSV 是取色器的颜色表示：H 色相 0–360，S 饱和度 0–1，V 明度 0–1。
type HSV struct{ H, S, V float64 }

// rgb 的三个分量是 0–255 的浮点数（换算中间值不取整，只在写成十六进制时四舍五入）。
type rgb struct{ R, G, B float64 }

// clampUnit 把 value 夹在 [lo, hi] 内；NaN 取下限（渲染层 clamp）。
func clampUnit(value, lo, hi float64) float64 {
	if math.IsNaN(value) {
		return lo
	}
	return math.Min(math.Max(value, lo), hi)
}

// jsRound 是 JavaScript 的 Math.round：半数向正无穷取整。
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// NormalizeHex 把用户输入的十六进制色统一成 `#rrggbb`（小写）：先去掉首尾空白，可带 `#`，三位简写补齐成六位；
// 认不出的输入返回 false，不猜。
func NormalizeHex(input string) (string, bool) {
	body := strings.TrimPrefix(strings.TrimSpace(input), "#")
	if len(body) != 3 && len(body) != 6 {
		return "", false
	}
	for _, r := range body {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return "", false
		}
	}
	body = strings.ToLower(body)
	if len(body) == 3 {
		body = string([]byte{body[0], body[0], body[1], body[1], body[2], body[2]})
	}
	return "#" + body, true
}

// rgbToHex 把 RGB 写成 `#rrggbb`：每个分量夹到 0–255 后四舍五入。
func rgbToHex(c rgb) string {
	channel := func(v float64) string {
		s := strconv.FormatInt(int64(jsRound(clampUnit(v, 0, 255))), 16)
		if len(s) < 2 {
			s = "0" + s
		}
		return s
	}
	return "#" + channel(c.R) + channel(c.G) + channel(c.B)
}

// hexToRGB 解析十六进制色（接受三位简写）；认不出时 ok 为假。
func hexToRGB(hex string) (rgb, bool) {
	n, ok := NormalizeHex(hex)
	if !ok {
		return rgb{}, false
	}
	part := func(i int) float64 {
		v, _ := strconv.ParseUint(n[i:i+2], 16, 8)
		return float64(v)
	}
	return rgb{part(1), part(3), part(5)}, true
}

// rgbToHSV 把 RGB 换成 HSV；灰阶的色相取 0。
func rgbToHSV(c rgb) HSV {
	red, green, blue := clampUnit(c.R, 0, 255)/255, clampUnit(c.G, 0, 255)/255, clampUnit(c.B, 0, 255)/255
	hi, lo := math.Max(red, math.Max(green, blue)), math.Min(red, math.Min(green, blue))
	delta := hi - lo
	h := 0.0
	if delta != 0 {
		switch hi {
		case red:
			h = 60 * math.Mod((green-blue)/delta+6, 6)
		case green:
			h = 60 * ((blue-red)/delta + 2)
		default:
			h = 60 * ((red-green)/delta + 4)
		}
	}
	s := 0.0
	if hi != 0 {
		s = delta / hi
	}
	return HSV{H: h, S: s, V: hi}
}

// hsvToRGB 把 HSV 换成 RGB：色相先归一到 [0, 360)，饱和度与明度夹到 0–1，所以越界的输入也得到合法颜色。
func hsvToRGB(c HSV) rgb {
	hue := math.Mod(math.Mod(c.H, 360)+360, 360)
	saturation, value := clampUnit(c.S, 0, 1), clampUnit(c.V, 0, 1)

	chroma := value * saturation
	secondary := chroma * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	offset := value - chroma

	var base rgb
	switch int(math.Floor(hue/60)) % 6 {
	case 0:
		base = rgb{chroma, secondary, 0}
	case 1:
		base = rgb{secondary, chroma, 0}
	case 2:
		base = rgb{0, chroma, secondary}
	case 3:
		base = rgb{0, secondary, chroma}
	case 4:
		base = rgb{secondary, 0, chroma}
	default:
		base = rgb{chroma, 0, secondary}
	}
	return rgb{(base.R + offset) * 255, (base.G + offset) * 255, (base.B + offset) * 255}
}

// HexToHSV 解析十六进制色并换成 HSV；认不出时 ok 为假。
func HexToHSV(hex string) (HSV, bool) {
	c, ok := hexToRGB(hex)
	if !ok {
		return HSV{}, false
	}
	return rgbToHSV(c), true
}

// HSVToHex 把 HSV 写成 `#rrggbb`。
func HSVToHex(c HSV) string { return rgbToHex(hsvToRGB(c)) }
