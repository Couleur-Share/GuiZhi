package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 来源平台的图标与名称（渲染层 components/library/platform-meta.tsx）。
//
// 平台表（platformTable）与品牌 logo（icons.Brand*）由 scripts/gen-viva-brand-icons.mjs 从渲染层生成，
// 这里只放着色规则与查表：渲染层的着色分四类，对应 colorKind。

// colorKind 是平台图标着色的方式（SOURCE_PLATFORM_META 里的 colorClass）。
type colorKind uint8

const (
	// colorForeground 是 text-foreground：抖音与 V2EX 的标准色是近黑，写死会在深色主题下消失，所以跟主题前景色走。
	colorForeground colorKind = iota
	// colorCurrent 是 text-current：继承所在位置的文字色（网页、本地文件这类兜底桶；2Libra 的 logo 自带五色，不受它影响）。
	colorCurrent
	// colorFixed 是 text-[#RRGGBB]：品牌色，不是主题的一部分，浅色与深色相同。
	colorFixed
	// colorPair 是 text-xxx dark:text-yyy：浅色与深色各取一档（微信公众号的绿）。
	colorPair
)

// brandColor 是一个平台的着色。
type brandColor struct {
	kind        colorKind
	light, dark tokens.Color
}

// resolve 求某个主题下图标的着色。current 是图标所在位置的文字色（text-current 取它）。
func (b brandColor) resolve(t tokens.Tokens, current tokens.Color, dark bool) tokens.Color {
	switch b.kind {
	case colorForeground:
		return t.Foreground
	case colorFixed:
		return b.light
	case colorPair:
		if dark {
			return b.dark
		}
		return b.light
	}
	return current
}

// platformMeta 是一个平台的显示信息；label 取当前界面语言的名称。
type platformMeta struct {
	label func() string
	icon  gfx.Icon
	color brandColor
}

// lookupPlatform 是平台查表（getSourcePlatformMeta）。形参写成 string 而不是枚举，理由与渲染层相同：
// 值直接来自数据库，新版本写入的平台在旧版本里查不到，这里不能因此出错，退回「未知来源」。
func lookupPlatform(platform string) platformMeta {
	if meta, ok := platformTable[platform]; ok {
		return meta
	}
	return unknownPlatform
}

// PlatformLabel 是平台的显示名（随界面语言）。
func PlatformLabel(platform string) string { return lookupPlatform(platform).label() }

// PlatformIcon 画平台图标，边长 size；current 是图标所在位置的文字色（text-current 的平台用它）。
func (l *Look) PlatformIcon(c ui.C, platform string, size float32, current tokens.Color) ui.Box {
	meta := lookupPlatform(platform)
	return l.Icon(c, meta.icon, size, meta.color.resolve(l.T, current, l.Dark))
}
