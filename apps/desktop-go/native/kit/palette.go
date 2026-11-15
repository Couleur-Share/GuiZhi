package kit

import (
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

// 命令面板的尺寸：对应渲染层 GlobalCommandPalette 的 max-w-2xl、pt-[12vh]、max-h-[60vh]、h-12。
const (
	paletteWidth   = 672
	paletteTopFrac = 0.12
	paletteMaxFrac = 0.6
	paletteInputH  = 48
	paletteRowGap  = 12
)

// PaletteItem 是命令面板里的一条结果。
type PaletteItem struct {
	// Key 是稳定标识（如 "action:backup"、"knowledge:42"），同时是这一行的元素身份。
	Key string
	// Group 是分组标题。调用方把同组结果排在一起；相邻的同组结果合并成一段。
	Group string
	Title string
	// Subtitle 可空：标题下的第二行小字（摘要、状态…）。
	Subtitle string
	// Icon 空取 Command。
	Icon icons.Name
	// Run 在用户选中这一条后调用（UI 线程）；面板先关闭。
	Run func()
}

// PaletteSource 提供面板的内容。面板本身不认识知识库、Wiki 或设置：
// 把它们拼成一个列表的聚合器属于应用层（M3），这里只定下它与面板的约定。
type PaletteSource interface {
	// Items 返回当前查询下要显示的结果。面板打开期间每帧在 UI 线程调用，必须快速返回——
	// 慢的来源（知识库搜索）在 QueryChanged 里启动后台搜索，缓存结果，Items 只读缓存。
	Items(query string) []PaletteItem
	// QueryChanged 在查询文本变化后调用一次（打开时与关闭时是空串）：
	// 防抖的后台搜索从这里开始、在关闭时取消；结果到了，来源自己请求重绘。
	QueryChanged(query string)
}

// Palette 是全局命令面板，对应渲染层 GlobalCommandPalette：输入框 + 分组结果列表。
// 持有者每帧在视图最外层调用 View，用快捷键或按钮时 Open。
//
// 键盘与渲染层一致：焦点始终在输入框，↑↓ 移动高亮（到头不循环），Enter 执行高亮项，Esc 关闭；
// 指针移进某一行时高亮跟过去。打开前有焦点的元素在关闭后拿回焦点。
// 读屏上它是带 ActiveDescendant 的搜索框 + 列表（渲染层只是一排普通按钮）。
type Palette struct {
	src PaletteSource

	// 仅 UI 线程读写
	open     bool
	fresh    bool // 刚打开、尚未给输入框焦点
	query    string
	notified string // 上一次告知来源的查询
	active   int
	// 结果数或查询变化时高亮回到第一项（渲染层的 useEffect(() => setActive(0), [results.length, query])）。
	lastQuery string
	lastCount int
	hovered   int  // 上一帧指针所在的行，-1 没有：只在指针进入新行时才移动高亮，键盘移走的高亮不会被静止的指针抢回
	reveal    bool // 高亮由键盘移动，需滚进可视区
}

// NewPalette 创建命令面板。
func NewPalette(src PaletteSource) *Palette { return &Palette{src: src, hovered: -1} }

// Open 打开面板，清空查询（UI 线程）。
func (p *Palette) Open() {
	p.open, p.fresh = true, true
	p.query, p.active, p.hovered = "", 0, -1
	p.lastQuery, p.lastCount = "", -1
	p.notify()
}

// Close 关闭面板（UI 线程），并让来源取消还在进行的搜索。
func (p *Palette) Close() {
	if !p.open {
		return
	}
	p.open, p.query = false, ""
	p.notify()
}

// Visible 报告面板是否打开。
func (p *Palette) Visible() bool { return p.open }

// Query 返回当前的查询文本（测试与上层记录用）。
func (p *Palette) Query() string { return p.query }

// notify 把查询的变化告知来源（只在变了的时候）。
func (p *Palette) notify() {
	if p.query == p.notified {
		return
	}
	p.notified = p.query
	if p.src != nil {
		p.src.QueryChanged(p.query)
	}
}

// View 画面板；没打开时什么都不画。
func (p *Palette) View(c *ui.Context, l *Look) {
	if !p.open {
		return
	}
	_, winH := c.Size()

	var items []PaletteItem
	var runItem func()
	var input ui.Element
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(l.T.Background.Alpha(0.55)).Justify(ui.Start).Padding(winH*paletteTopFrac, 16, 16, 16)
		panel.Column().Width(paletteWidth).MaxWidthPercent(100).Radius(16).Clip().
			Border(1, l.Border).Background(l.T.Background).Label(locales.CommandPaletteTitle())
		panel.Shadow(0, 25, 50, -12, ui.RGBA(0, 0, 0, 0.25))

		input = p.searchRow(c, l)
		// 输入框先建：这一帧里它已把键入写进 p.query，随后取的结果就是新查询的，不落后一帧
		p.notify()
		items = p.src.Items(p.query)
		p.syncActive(len(items))

		if panel.Shortcut(0, ui.KeyDown) && len(items) > 0 {
			p.active, p.reveal = min(p.active+1, len(items)-1), true
		}
		if panel.Shortcut(0, ui.KeyUp) && len(items) > 0 {
			p.active, p.reveal = max(p.active-1, 0), true
		}
		// Enter 是单行输入框自己的 Submitted（它先于外层的 Shortcut 取走这个键）；输入法正在组字时的回车属于输入法，不会走到这里
		if input.Submitted() && p.active < len(items) {
			runItem = items[p.active].Run
		}
		if clicked := p.results(c, l, input, items, winH); clicked != nil {
			runItem = clicked
		}
	})
	if p.fresh { // 理由见 Confirm.focusInitial
		p.fresh = false
		input.Focus()
	}
	switch {
	case runItem != nil:
		p.Close()
		runItem()
	case !open:
		p.Close()
	}
}

// syncActive 在结果数或查询变化时让高亮回到第一项，并保证它落在结果里。
func (p *Palette) syncActive(count int) {
	if p.query != p.lastQuery || count != p.lastCount {
		p.active, p.lastQuery, p.lastCount = 0, p.query, count
	}
	p.active = max(min(p.active, count-1), 0)
}

// searchRow 画顶部的搜索行：放大镜 + 无外观的输入框，下沿一条分隔线。
func (p *Palette) searchRow(c *ui.Context, l *Look) ui.Element {
	var in ui.Element
	ui.Row(c).Height(paletteInputH).Padding(0, 16).Gap(paletteRowGap).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(l.Border).Children(func() {
		Icon(c, icons.Search, 16).TextColor(l.T.MutedForeground)
		in = ui.TextInputBase(c, &p.query).Grow(1).FontSize(14).TextColor(l.T.Foreground).
			Label(locales.CommandPalettePlaceholder()).Placeholder(locales.CommandPalettePlaceholder()).
			AutoComplete(ui.AutoCompleteList).FocusRing(false)
	})
	return in
}

// results 画结果列表（或「没有匹配结果」），返回被点击的那一项的 Run（没有则 nil）。
func (p *Palette) results(c *ui.Context, l *Look, input ui.Element, items []PaletteItem, winH float32) (clicked func()) {
	scroll := ui.Scroll(c).MaxHeight(winH * paletteMaxFrac).Padding(8).Role(ui.RoleListBox).
		Label(locales.CommandPaletteTitle())
	hover, active := -1, ui.Element{}
	scroll.Children(func() {
		if len(items) == 0 {
			ui.Text(c, locales.CommandPaletteNoResults()).FontSize(14).TextAlign(ui.Center).
				TextColor(l.T.MutedForeground).Padding(32, 12)
			return
		}
		group := ""
		for i, it := range items {
			if i == 0 || it.Group != group {
				group = it.Group
				ui.Text(c, it.Group).FontSize(11).FontWeight(500).TextColor(l.T.MutedForeground).Padding(8, 12, 4, 12)
			}
			row := p.row(c, l, it, i == p.active)
			if i == p.active {
				active = row
				if p.reveal {
					row.ScrollIntoView()
				}
			}
			if row.Hovered() {
				hover = i
			}
			if row.Clicked() {
				clicked = it.Run
			}
		}
	})
	p.reveal = false
	if active.Valid() {
		input.ActiveDescendant(active)
		input.Controls(scroll)
	}
	// 指针进入新行：高亮跟过去。本帧的行已经画过，补一帧让它显出来
	if hover != p.hovered {
		p.hovered = hover
		if hover >= 0 && hover != p.active {
			p.active = hover
			c.Invalidate()
		}
	}
	return clicked
}

// row 画一条结果：图标 + 标题（+ 副标题），高亮项垫 accent 色。
func (p *Palette) row(c *ui.Context, l *Look, it PaletteItem, active bool) ui.Element {
	bg := ui.Transparent
	if active {
		bg = l.T.Accent
	}
	icon := it.Icon
	if icon == "" {
		icon = icons.Command
	}
	row := ui.Row(c).Key(it.Key).Role(ui.RoleOption).Label(it.Title).AccessSelected(active).
		Padding(8, 12).Gap(paletteRowGap).AlignItems(ui.Center).Radius(8).Background(bg)
	if it.Subtitle != "" {
		row.Description(it.Subtitle)
	}
	row.Children(func() {
		Icon(c, icon, 16).TextColor(l.T.MutedForeground).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, it.Title).FontSize(14).TextColor(l.T.Foreground).MaxLines(1).Ellipsis("…")
			if it.Subtitle != "" {
				ui.Text(c, it.Subtitle).FontSize(12).TextColor(l.T.MutedForeground).MaxLines(1).Ellipsis("…")
			}
		})
	})
	return row
}
