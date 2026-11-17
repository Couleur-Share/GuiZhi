package settings

import (
	"math"
	"strconv"
	"strings"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// hexDigits 取 hex 里的六位十六进制数：渲染层 hexToHs 的校验是「去掉空白与一个前导 #，恰好六位十六进制」，
// 不接受三位简写；不合格时 ok 为假。
func hexDigits(hex string) (digits string, ok bool) {
	s := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	return s, isSixHex(s)
}

// isSixHex 报告 s 是否恰好是六位十六进制数字。
func isSixHex(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// HexToHS 把 `#rrggbb` 换成主题色相与饱和度（渲染层 settings-appearance.ts 的 hexToHs，逐步照搬）：
// 色相四舍五入到整度（负值加 360），饱和度是 HSL 的饱和度四舍五入到整数百分比。ok 为假表示不是合法十六进制。
func HexToHS(hex string) (hue, saturation float64, ok bool) {
	digits, ok := hexDigits(hex)
	if !ok {
		return 0, 0, false
	}
	channel := func(i int) float64 {
		v, _ := strconv.ParseUint(digits[i:i+2], 16, 8)
		return float64(v) / 255
	}
	red, green, blue := channel(0), channel(2), channel(4)
	hi, lo := math.Max(red, math.Max(green, blue)), math.Min(red, math.Min(green, blue))
	delta := hi - lo

	if delta != 0 {
		switch hi {
		case red:
			hue = math.Mod((green-blue)/delta, 6) // JS 的 % 保留符号，math.Mod 同样
		case green:
			hue = (blue-red)/delta + 2
		default:
			hue = (red-green)/delta + 4
		}
		hue = jsRound(hue * 60)
		if hue < 0 {
			hue += 360
		}
	}
	lightness := (hi + lo) / 2
	if delta != 0 {
		saturation = delta / (1 - math.Abs(2*lightness-1))
	}
	return clampFloat(hue, 0, 360), clampFloat(jsRound(saturation*100), 0, 100), true
}

// jsRound 是 JavaScript 的 Math.round：半数向正无穷取整。
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

func clampFloat(x, lo, hi float64) float64 { return math.Min(math.Max(x, lo), hi) }

// hexColor 把 `#rrggbb` 换成不透明颜色（自定义色板的底色）；渲染层把它原样交给 CSS background-color，
// 三位简写 CSS 也认，所以这里也认；其余写法 CSS 当作无效、不画底色（ok 为假）。
func hexColor(hex string) (tokens.Color, bool) {
	s := strings.TrimSpace(hex)
	if !strings.HasPrefix(s, "#") {
		return tokens.Color{}, false
	}
	s = s[1:]
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if !isSixHex(s) {
		return tokens.Color{}, false
	}
	n, _ := strconv.ParseUint(s, 16, 32)
	return tokens.RGBA(uint8(n>>16), uint8(n>>8), uint8(n), 1), true
}

// presetName 是预设主题色的显示名（渲染层按 id 拼出 settings.color* 键）；不认识的 id 返回空串。
func presetName(id string) string {
	switch id {
	case "royal-blue":
		return locales.SettingsColorRoyalBlue()
	case "blue":
		return locales.SettingsColorBlue()
	case "purple":
		return locales.SettingsColorPurple()
	case "green":
		return locales.SettingsColorGreen()
	case "orange":
		return locales.SettingsColorOrange()
	case "teal":
		return locales.SettingsColorTeal()
	}
	return ""
}
