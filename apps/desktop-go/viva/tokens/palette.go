package tokens

import (
	"fmt"
	"strconv"
)

// tailwindPalette 是渲染层写死的 Tailwind 3.4 色阶：Toast、确认框、未保存对话框、徽标直接用了
// green / red / yellow / blue / amber-*，它们不属于主题令牌（globals.css 没有对应的 CSS 变量）。
// 集中放在这里，控件里不散落十六进制字面量；
// scripts/tests/gen-native.test.mjs 用 tailwindcss/colors 逐项对拍，抄错或版本变了会失败。
// 用到哪一档加哪一档：渲染层多用一档色阶，这里就补一行，对拍测试同时守住新加的值。
var tailwindPalette = map[string]string{
	"green-50":    "#f0fdf4",
	"green-100":   "#dcfce7",
	"green-200":   "#bbf7d0",
	"green-400":   "#4ade80",
	"green-500":   "#22c55e",
	"green-600":   "#16a34a",
	"green-800":   "#166534",
	"green-900":   "#14532d",
	"red-50":      "#fef2f2",
	"red-100":     "#fee2e2",
	"red-200":     "#fecaca",
	"red-400":     "#f87171",
	"red-500":     "#ef4444",
	"red-600":     "#dc2626",
	"red-700":     "#b91c1c",
	"red-800":     "#991b1b",
	"red-900":     "#7f1d1d",
	"yellow-50":   "#fefce8",
	"yellow-200":  "#fef08a",
	"yellow-500":  "#eab308",
	"yellow-800":  "#854d0e",
	"yellow-900":  "#713f12",
	"blue-50":     "#eff6ff",
	"blue-200":    "#bfdbfe",
	"blue-500":    "#3b82f6",
	"blue-800":    "#1e40af",
	"blue-900":    "#1e3a8a",
	"amber-100":   "#fef3c7",
	"amber-400":   "#fbbf24",
	"amber-500":   "#f59e0b",
	"amber-600":   "#d97706",
	"amber-900":   "#78350f",
	"amber-950":   "#451a03", // 搜索命中词（mark）的字色：highlight-text.tsx 的 text-amber-950
	"emerald-500": "#10b981",
	// 「恢复前」备份徽标（DataSettings 的 BackupKindBadge）：bg-violet-500/15 text-violet-600 dark:text-violet-400。
	"violet-500": "#8b5cf6",
	"violet-600": "#7c3aed",
	"violet-400": "#a78bfa",
	// 标签色点（type-meta.tsx 的 TAG_DOT_CLASSES）：十个颜色里除上面已有的 red / amber / green / blue 的 500 档之外的六个。
	"orange-500": "#f97316",
	"teal-500":   "#14b8a6",
	"indigo-500": "#6366f1",
	"purple-500": "#a855f7",
	"pink-500":   "#ec4899",
	"gray-400":   "#9ca3af",
	// 标签小块的文字色（type-meta.tsx 的 TAG_COLOR_CLASSES：浅色 text-*-700、深色 text-*-300）：九个彩色的 300 档，
	// 以及 700 档里上面还没有的（red-700 已有）。
	"red-300":    "#fca5a5",
	"orange-300": "#fdba74",
	"orange-700": "#c2410c",
	"amber-300":  "#fcd34d",
	"amber-700":  "#b45309",
	"green-300":  "#86efac",
	// 完整备份密钥环状态条（RepositoryBackupSection）的文字色：text-emerald-700 / dark:text-emerald-300。
	"emerald-700": "#047857",
	"emerald-300": "#6ee7b7",
	"green-700":   "#15803d",
	"teal-300":    "#5eead4",
	"teal-700":    "#0f766e",
	"blue-300":    "#93c5fd",
	"blue-700":    "#1d4ed8",
	"indigo-300":  "#a5b4fc",
	"indigo-700":  "#4338ca",
	"purple-300":  "#d8b4fe",
	"purple-700":  "#7e22ce",
	"pink-300":    "#f9a8d4",
	"pink-700":    "#be185d",
}

// White 是 Tailwind 的 text-white / bg-white。
var White = RGBA(255, 255, 255, 1)

// Tailwind 取调色板里的一个不透明色值（如 "green-500"）。名字写错是程序错误，直接 panic，测试会第一时间发现。
func Tailwind(name string) Color {
	hex, ok := tailwindPalette[name]
	if !ok {
		panic(fmt.Sprintf("tokens: 调色板里没有 %s", name))
	}
	return hexColor(hex)
}

// hexColor 解析 "#rrggbb"。
func hexColor(hex string) Color {
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil || len(hex) != 7 {
		panic(fmt.Sprintf("tokens: 色值 %q 不是 #rrggbb", hex))
	}
	return RGBA(uint8(v>>16), uint8(v>>8), uint8(v), 1)
}

// TailwindPalette 返回调色板的名字到十六进制的拷贝（测试对拍用）。
func TailwindPalette() map[string]string {
	out := make(map[string]string, len(tailwindPalette))
	for k, v := range tailwindPalette {
		out[k] = v
	}
	return out
}
