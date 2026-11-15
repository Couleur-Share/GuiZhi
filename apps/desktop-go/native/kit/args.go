package kit

import (
	"fmt"
	"strconv"
	"strings"
)

// 以下是命令行与测试用的外观实参解析（-native-theme、-native-hue 等）：
// 与设置里的取值集合一致，写错时报出可选项，而不是静默落回默认值。

// ParseMode 解析明暗模式。
func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.TrimSpace(s)); m {
	case ModeSystem, ModeLight, ModeDark:
		return m, nil
	}
	return "", fmt.Errorf("未知的明暗模式 %q（可选：system、light、dark）", s)
}

// ParseMotion 解析动效偏好。
func ParseMotion(s string) (MotionPref, error) {
	switch m := MotionPref(strings.TrimSpace(s)); m {
	case MotionSystem, MotionOff, MotionReduced, MotionStandard:
		return m, nil
	}
	return "", fmt.Errorf("未知的动效偏好 %q（可选：system、off、reduced、standard）", s)
}

// ParseFontSize 解析字号档 ID。
func ParseFontSize(s string) (string, error) {
	id := strings.TrimSpace(s)
	ids := make([]string, 0, len(FontSizes))
	for _, f := range FontSizes {
		if f.ID == id {
			return id, nil
		}
		ids = append(ids, f.ID)
	}
	return "", fmt.Errorf("未知的字号档 %q（可选：%s）", s, strings.Join(ids, "、"))
}

// ParseHue 解析主题色实参：预设 ID（如 royal-blue），或自定义的「色相,饱和度」（如 150,30，对应设置里的自定义色）。
func ParseHue(s string) (hue, sat float64, err error) {
	s = strings.TrimSpace(s)
	ids := make([]string, 0, len(HuePresets))
	for _, p := range HuePresets {
		if p.ID == s {
			return p.Hue, p.Saturation, nil
		}
		ids = append(ids, p.ID)
	}
	hs, ss, ok := strings.Cut(s, ",")
	if ok {
		h, e1 := strconv.ParseFloat(strings.TrimSpace(hs), 64)
		sv, e2 := strconv.ParseFloat(strings.TrimSpace(ss), 64)
		if e1 == nil && e2 == nil && h >= 0 && h <= 360 && sv >= 0 && sv <= 100 {
			return h, sv, nil
		}
	}
	return 0, 0, fmt.Errorf("未知的主题色 %q（可选预设：%s；或「色相,饱和度」，色相 0–360、饱和度 0–100）", s, strings.Join(ids, "、"))
}
