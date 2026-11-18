package library

import (
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// SourcePlatforms 是侧栏「平台」分区的固定顺序（packages/shared/utils/source-platforms.ts 的 SOURCE_PLATFORMS）：
// 不按数量排，免得采集一条就重排一次。catalog_test.go 把它与源文件逐项对拍。
var SourcePlatforms = []string{
	"douyin", "bilibili", "xiaohongshu", "youtube", "v2ex", "nga", "linuxdo",
	"nodeseek", "appinn", "twolibra", "wechat", "web", "local",
}

// tagColor 是一个标签颜色：键（packages/shared/types/knowledge.ts 的 TAG_COLOR_KEYS 顺序）、名称与色点的颜色。
type tagColor struct {
	Key   string
	Label func() string
	// Dot 是色点的颜色（type-meta.tsx 的 TAG_DOT_CLASSES：bg-*-500，灰色是 bg-gray-400）。
	Dot tokens.Color
}

// tagColors 按渲染层的顺序给出十个标签颜色。
func tagColors() []tagColor {
	return []tagColor{
		{"red", locales.LibraryColorRed, tokens.Tailwind("red-500")},
		{"orange", locales.LibraryColorOrange, tokens.Tailwind("orange-500")},
		{"amber", locales.LibraryColorAmber, tokens.Tailwind("amber-500")},
		{"green", locales.LibraryColorGreen, tokens.Tailwind("green-500")},
		{"teal", locales.LibraryColorTeal, tokens.Tailwind("teal-500")},
		{"blue", locales.LibraryColorBlue, tokens.Tailwind("blue-500")},
		{"indigo", locales.LibraryColorIndigo, tokens.Tailwind("indigo-500")},
		{"purple", locales.LibraryColorPurple, tokens.Tailwind("purple-500")},
		{"pink", locales.LibraryColorPink, tokens.Tailwind("pink-500")},
		{"gray", locales.LibraryColorGray, tokens.Tailwind("gray-400")},
	}
}

// tagDotColor 是标签色点的颜色。键不在十个之内（数据库里的值来自别的版本）时返回零值：渲染层
// `TAG_DOT_CLASSES[colorKey]` 取到 undefined，色点没有底色，是个看不见的圆。
func tagDotColor(key string) tokens.Color {
	for _, c := range tagColors() {
		if c.Key == key {
			return c.Dot
		}
	}
	return tokens.Color{}
}

// iconGroup 是合集图标目录的一组（components/library/collection-icons.ts 的 COLLECTION_ICON_GROUPS）。
type iconGroup struct {
	ID    string
	Label func() string
	Icons []string
}

// collectionIconGroups 分八组、每组十个，一组正好占一行。只收广为流传的老 emoji：新码位在 Windows 10 上会渲染成豆腐块。
// catalog_test.go 把它与渲染层的源文件逐项对拍。
func collectionIconGroups() []iconGroup {
	return []iconGroup{
		{"learning", locales.LibraryIconGroupLearning, []string{"📚", "📖", "🎓", "🧠", "💡", "✏️", "📝", "🔬", "🧪", "🔭"}},
		{"work", locales.LibraryIconGroupWork, []string{"💼", "📊", "📈", "🗓️", "✅", "📌", "🎯", "🏢", "🤝", "⏰"}},
		{"tech", locales.LibraryIconGroupTech, []string{"💻", "🖥️", "⌨️", "🧩", "⚙️", "🔧", "🐛", "🤖", "🔐", "🌐"}},
		{"creative", locales.LibraryIconGroupCreative, []string{"🎨", "🖌️", "✍️", "🎬", "📷", "🎵", "🎤", "🖼️", "🎭", "✨"}},
		{"life", locales.LibraryIconGroupLife, []string{"🏠", "☕", "🍳", "🌿", "🌱", "💪", "🏃", "🧘", "🛏️", "🐾"}},
		{"business", locales.LibraryIconGroupBusiness, []string{"💰", "💵", "💳", "🏦", "🧾", "🛒", "🏷️", "📦", "💹", "🎁"}},
		{"travel", locales.LibraryIconGroupTravel, []string{"✈️", "🗺️", "🧭", "🏔️", "🌊", "🌍", "🏕️", "🚗", "🌸", "☀️"}},
		{"marks", locales.LibraryIconGroupMarks, []string{"⭐", "❤️", "🔥", "⚡", "🔖", "🏆", "🗂️", "🗃️", "🔗", "❗"}},
	}
}

// isCatalogIcon 报告图标在不在目录里（isCatalogIcon）：目录外的取值多半来自旧 .NET 库的迁移，选择器把它单独摆一格。
func isCatalogIcon(icon string) bool {
	for _, g := range collectionIconGroups() {
		for _, candidate := range g.Icons {
			if candidate == icon {
				return true
			}
		}
	}
	return false
}
