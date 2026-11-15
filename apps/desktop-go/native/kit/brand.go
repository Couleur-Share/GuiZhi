package kit

import (
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
)

// 来源平台图标与模型分类图标：数据表由 gen-native-brand-icons.mjs 从渲染层生成（brand_gen.go），
// 这里是类型与绘制。绘制规则对应渲染层的 PlatformIcon（platform-meta.tsx）与 getCategoryIcon（ModelIcons.tsx）。

type colorKind uint8

const (
	colorForeground colorKind = iota // text-foreground：跟主题的前景色走
	colorCurrent                     // text-current：继承调用方给的文字色
	colorFixed                       // text-[#xxxxxx]：品牌色，明暗主题不变
	colorPair                        // text-x-600 dark:text-x-400：明暗各一个色
)

// brandColor 是平台图标的着色，由渲染层的 colorClass 解析而来。
type brandColor struct {
	kind        colorKind
	light, dark ui.Color
}

func (b brandColor) resolve(l *Look, current ui.Color) ui.Color {
	switch b.kind {
	case colorForeground:
		return l.T.Foreground
	case colorFixed:
		return b.light
	case colorPair:
		if l.Dark {
			return b.dark
		}
		return b.light
	default:
		return current
	}
}

// platformMeta 是一个来源平台的名称、图标与着色：图标要么是品牌 logo（brand），要么是 lucide 通用图标（lucide）。
type platformMeta struct {
	label  func() string
	brand  icons.Brand
	lucide icons.Name
	color  brandColor
}

// gradientBox 是 CSS 的 linear-gradient(angle, from 0%, to 100%)。
type gradientBox struct {
	angle    float32
	from, to ui.Color
}

func (g gradientBox) apply(e ui.Element) ui.Element { return e.Gradient(g.from, g.to, g.angle) }

// modelSpecial 是「自定义」「其他」这类没有厂商图标的特殊分类：渐变圆角盒里放一枚 lucide 图标。
type modelSpecial struct {
	icon   icons.Name
	box    gradientBox
	color  ui.Color
	border ui.Color
}

func platformFor(platform string) platformMeta {
	if m, ok := platformTable[platform]; ok {
		return m
	}
	return unknownPlatform
}

// PlatformLabel 返回来源平台的显示名（随界面语言）；数据库里本版本不认识的平台值返回「未知来源」，不报错。
func PlatformLabel(platform string) string { return platformFor(platform).label() }

// PlatformIcon 画来源平台的图标（侧栏「平台」分区、表格「来源」列）。size 是边长（DIP）；
// current 是 text-current 类平台（网页、本地文件、未知来源）取的颜色，即所在行的文字色。
// 品牌自带多色的 logo（2Libra）不受着色影响。图标是装饰性的：旁边必须有可见的平台名。
func PlatformIcon(c *ui.Context, l *Look, platform string, size float32, current ui.Color) ui.Element {
	m := platformFor(platform)
	color := m.color.resolve(l, current)
	if m.brand == "" {
		return Icon(c, m.lucide, size).TextColor(color)
	}
	svg := icons.BrandSVG(m.brand)
	switch {
	case svg == nil:
		return ui.Box(c).Size(size, size) // 解析失败：占位，版面不跳（失败原因已记日志）
	case icons.BrandMono(m.brand):
		return ui.Icon(c, svg).Size(size, size).TextColor(color)
	default:
		return ui.Image(c, svg).Size(size, size)
	}
}

// HasModelIcon 报告分类是否有专属图标（厂商图标或特殊分类）；没有的分类由 ModelIcon 画首字母兜底。
func HasModelIcon(category string) bool {
	if _, ok := modelBrandByCategory[category]; ok {
		return true
	}
	_, ok := modelSpecialByCategory[category]
	return ok
}

// ModelIcon 画模型分类（厂商）的图标，size 是边长（DIP）。优先级与渲染层一致：
// 香蕉分类 → 厂商品牌图标 → 特殊分类（自定义、其他）→ 首字母圆章。
func ModelIcon(c *ui.Context, l *Look, category string, size float32) ui.Element {
	if category == modelBananaCategory {
		return modelBananaIcon(c, size)
	}
	if brand, ok := modelBrandByCategory[category]; ok {
		return brandImage(c, brand, size, modelImageRadius)
	}
	if sp, ok := modelSpecialByCategory[category]; ok {
		return modelSpecialIcon(c, sp, size)
	}
	return modelLetterIcon(c, category, size)
}

// brandImage 按品牌自带的颜色画一个 size×size 的图标，圆角裁切（渲染层 <img> 的 borderRadius）。
func brandImage(c *ui.Context, b icons.Brand, size, radius float32) ui.Element {
	if svg := icons.BrandSVG(b); svg != nil {
		return ui.Image(c, svg).Size(size, size).Radius(radius).Clip()
	}
	if bm, frac := icons.BrandBitmap(b); bm != nil {
		inner := size * frac
		box := ui.Column(c).Size(size, size).Center().Radius(radius).Clip()
		box.Children(func() { ui.Image(c, bm).Size(inner, inner) })
		return box
	}
	return ui.Box(c).Size(size, size)
}

func modelSpecialIcon(c *ui.Context, sp modelSpecial, size float32) ui.Element {
	box := ui.Column(c).Size(size, size).Center().Radius(modelSpecialRadius).Border(1, sp.border)
	box = sp.box.apply(box)
	box.Children(func() {
		Icon(c, sp.icon, size*modelSpecialIconScale).TextColor(sp.color)
	})
	return box
}

func modelBananaIcon(c *ui.Context, size float32) ui.Element {
	box := ui.Column(c).Size(size, size).Center().Radius(modelBananaRadius).Border(1, modelBananaBorder)
	box = modelBananaBox.apply(box)
	box.Children(func() {
		ui.Text(c, modelBananaGlyph).FontSize(size * modelBananaFontScale)
	})
	return box
}

// modelLetterIcon 是没有任何专属图标时的兜底：灰色渐变圆章加分类名的首字母。
func modelLetterIcon(c *ui.Context, category string, size float32) ui.Element {
	letter := "?"
	if rs := []rune(category); len(rs) > 0 {
		letter = string(rs[0])
	}
	box := ui.Column(c).Size(size, size).Center().Radius(size / 2)
	box = gradientBox{angle: modelLetterAngle, from: modelLetterFrom, to: modelLetterTo}.apply(box)
	box.Children(func() {
		ui.Text(c, letter).FontSize(size * modelLetterFontScale).FontWeight(modelLetterFontWeight).TextColor(modelLetterColor)
	})
	return box
}
