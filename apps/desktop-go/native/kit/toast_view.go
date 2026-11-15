package kit

import (
	"time"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

const (
	toastMaxWidth   = 416 // max-w-[26rem]
	toastEdge       = 24  // bottom-6 right-6
	toastGap        = 12  // gap-3
	toastDetailInd  = 32  // pl-8：详情与文案对齐，不钻到图标下面
	toastDetailMaxH = 160 // max-h-40
	toastCopiedFor  = 2 * time.Second
)

// toastTint 是一类提示的配色：渲染层写死的 Tailwind 色阶（见 tailwind.go）。
type toastTint struct {
	fill, border, icon ui.Color
	glyph              icons.Name
}

// toastTint 返回 kind 的配色。深色下底色是 *-900/20 叠在页面底色上；
// 没有背景模糊，所以预先合成成不透明色（见 over）。
func (l *Look) toastTint(kind ToastKind) toastTint {
	var hue string
	var glyph icons.Name
	switch kind {
	case ToastSuccess:
		hue, glyph = "green", icons.CircleCheck
	case ToastError:
		hue, glyph = "red", icons.CircleX
	case ToastWarning:
		hue, glyph = "yellow", icons.TriangleAlert
	default:
		hue, glyph = "blue", icons.Info
	}
	t := toastTint{icon: tw(hue + "-500"), glyph: glyph}
	if l.Dark {
		t.fill = over(tw(hue+"-900").Alpha(0.2), l.T.Background)
		t.border = tw(hue + "-800")
	} else {
		t.fill = tw(hue + "-50")
		t.border = tw(hue + "-200")
	}
	return t
}

// ToastViewport 画窗口的提示：在应用视图里每帧调用一次（放在最后，它是覆盖层）。
// 先领走 Notifier 里排队的操作，再按渲染层 Toast 的样子画：右下角、最新的在下、
// 错误/警告不自动消失、「查看详情」展开后可复制原文。
//
// 与渲染层不同的地方：最多同时显示最新 3 条（MyGo 的上限），更早的在队列里等着，
// 不会被挤掉——持久的错误关掉一条后下一条才露出来，所以批量失败时不会有哪条在读到之前就消失。
func ToastViewport(c *ui.Context, l *Look, n *Notifier) {
	n.flush(c)
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(toastEdge).AlignItems(ui.End).Gap(toastGap)
		for _, t := range toasts {
			n.card(c, l, t)
		}
	})
}

func (n *Notifier) card(c *ui.Context, l *Look, t ui.Toast) {
	meta, _ := t.Data.(toastMeta)
	kind := meta.kind
	if kind == "" {
		kind = ToastKind(t.Type)
	}
	tint := l.toastTint(kind)
	p := ui.ToastBase(c, t)
	p.Root.Column().MaxWidth(toastMaxWidth).Padding(12, 16).Radius(16).Border(1, tint.border).Background(tint.fill).
		Shadow(0, 25, 50, -12, ui.RGBA(0, 0, 0, 0.25))
	p.Root.Children(func() {
		// 主行垂直居中：关闭按钮比文字高，顶对齐会让图标和文案贴着上沿、下面多出一截空白
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			Icon(c, tint.glyph, 20).TextColor(tint.icon)
			ui.Text(c, t.Title).FontSize(14).FontWeight(600).LineHeight(1.375).TextColor(l.T.Foreground).Grow(1).Shrink(1)
			if t.Action != "" {
				// 不能给 ActionButton/CloseButton 再设 Key：它们在创建那一刻就判断了点击，之后改身份会错过这一帧的点击
				b := p.ActionButton()
				bg := ui.Transparent
				if b.Hovered() {
					bg = l.T.Primary.Alpha(0.1)
				}
				b.Padding(4, 8).Radius(8).Background(bg).Shrink(0).Children(func() {
					ui.Text(c, t.Action).FontSize(14).FontWeight(600).TextColor(l.T.Primary)
				})
			}
			b := p.CloseButton().Label(locales.CommonClose())
			bg := ui.Transparent
			if b.Hovered() {
				bg = l.wash(0.1, 0.1)
			}
			b.Size(24, 24).Radius(8).Background(bg).Shrink(0).Center().Children(func() {
				Icon(c, icons.X, 16).TextColor(l.T.MutedForeground)
			})
		})
		if meta.detail != "" {
			n.detail(c, l, t.ID, meta.detail)
		}
	})
}

// wash 返回淡淡盖上去的一层明暗相反色：浅色盖黑、深色盖白，两档强度各自指定
// （渲染层的 bg-black/5 dark:bg-white/10 一类写法）。
func (l *Look) wash(light, dark float32) ui.Color {
	if l.Dark {
		return ui.RGBA(255, 255, 255, dark)
	}
	return ui.RGBA(0, 0, 0, light)
}

// detail 是折叠在提示下面的报错原文：展开而不是直接铺开——提示语要短才有人读，但原因不能因此丢掉。
// 「复制」是这一块的重点：用户拿到原文才谈得上去搜或者提 issue。
func (n *Notifier) detail(c *ui.Context, l *Look, id, text string) {
	v := n.view(id)
	ui.Column(c).Margin(6, 0, 0, toastDetailInd).AlignItems(ui.Start).Children(func() {
		label := locales.CommonViewDetail()
		glyph := icons.ChevronRight
		if v.expanded {
			label, glyph = locales.CommonCollapse(), icons.ChevronDown
		}
		tg := ui.ButtonBase(c).Key("detail-toggle").Expanded(v.expanded)
		fg := l.T.MutedForeground
		if tg.Hovered() {
			fg = l.T.Foreground
		}
		tg.Gap(2).Radius(6).Children(func() {
			Icon(c, glyph, 14).TextColor(fg)
			ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(fg)
		})
		if tg.Clicked() {
			v.expanded = !v.expanded
		}
		if !v.expanded {
			return
		}
		ui.Column(c).Margin(6, 0, 0, 0).Padding(8).Radius(8).Background(l.wash(0.05, 0.05)).AlignItems(ui.Start).Children(func() {
			ui.Scroll(c).MaxHeight(toastDetailMaxH).Children(func() {
				ui.Text(c, text).Font("monospace").FontSize(11).LineHeight(1.625).TextColor(l.T.MutedForeground).Selectable()
			})
			now := c.Now()
			if v.justCopied {
				// 复制结果在帧外回来（拿不到这一帧的时间），到这里才开始计「已复制」的 2 秒
				v.justCopied, v.copiedUntil = false, now.Add(toastCopiedFor)
			}
			copied := now.Before(v.copiedUntil)
			if copied {
				c.After(v.copiedUntil.Sub(now)) // 到点再画一帧，把「已复制」换回「复制」
			}
			cp := ui.ButtonBase(c).Key("detail-copy")
			fgc, bg := l.T.MutedForeground, ui.Transparent
			if cp.Hovered() {
				fgc, bg = l.T.Foreground, l.wash(0.05, 0.1)
			}
			cp.Margin(6, 0, 0, 0).Padding(2, 6).Gap(4).Radius(6).Background(bg).Children(func() {
				Icon(c, icons.Copy, 12).TextColor(fgc)
				name := locales.CommonCopy()
				if copied {
					name = locales.ToastCopied()
				}
				ui.Text(c, name).FontSize(11).FontWeight(500).TextColor(fgc)
			})
			if cp.Clicked() {
				// 复制不了也没关系：原文就摊在上面，用户照样读得到
				c.WriteClipboardThen(text, func(err error) { v.justCopied = err == nil })
			}
		})
	})
}
