package settings

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// 背景图预览舞台里有图时的内容（渲染层 AppearanceSettings.tsx 的 BackgroundPreviewStage，台账 B-020）：
//
//	<div class="background-preview-stage pointer-events-none relative h-full w-full select-none overflow-hidden rounded-xl bg-background text-foreground app-background-mode-image">
//	  <BackgroundImageBackdrop … />   ← 壁纸位图加毛玻璃遮罩
//	  <div class="… app-wallpaper-shell"> 顶栏 + 左栏 + 列表栏 + 详情栏的缩微示意 </div>
//	</div>
//
// 关键点：舞台外层固定带 app-background-mode-image，所以不论「开启 / 关闭」开关是什么状态，只要选了图，
// 舞台里的缩微界面一律用壁纸（玻璃）模式的令牌画——预览展示的是「开启后」的样子。
//
// 整块是 pointer-events-none 的纯绘制与布局：不登记命中区、不出文字，不进可访问树。
// 没有复刻的两处（Viva 没有对应原语）：① 圆角裁剪——位图与缩微界面只能按矩形裁，四个角比渲染层的 rounded-xl 多出约 3 px 的方角；
// ② backdrop-filter 的磨砂模糊（--app-ui-*-backdrop）——半透明面板只叠色，不把背后的壁纸再糊一层。

// previewShell 画缩微外壳；gt 是壁纸模式的令牌。
func (a *Appearance) previewShell(c ui.C, gt tokens.Tokens) {
	l := a.Look
	md := l.Rem(0.75) - 2 // rounded-md：--radius 减 2px
	lg := l.Rem(0.75)     // rounded-lg 与 rounded-xl：本应用两者都是 0.75rem
	sm := l.Rem(0.25)     // rounded
	paint := func(b ui.Box, s kit.Surface) ui.Box {
		return b.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, s) })
	}
	// 带单边边框的整条（border-b / border-r）：背景与阴影按整块画，边框是贴边的一条线。
	bar := func(b ui.Box, s kit.Surface, edge kit.Edge, edgeColor tokens.Color) ui.Box {
		return b.Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, s)
			l.PaintEdge(d, edge, 1, edgeColor)
		})
	}
	surface := func(bg, border tokens.Color, shadow tokens.Shadow, radius float32) kit.Surface {
		return kit.Surface{Bg: bg, Border: border, BorderW: 0, Radius: radius, Shadow: shadow}
	}
	// withBorder 是 border border-border 的元素：边框色被 app-wallpaper-* 的 !important 换成对应令牌。
	withBorder := func(bg, border tokens.Color, shadow tokens.Shadow, radius float32) kit.Surface {
		s := surface(bg, border, shadow, radius)
		s.BorderW = 1
		return s
	}
	surfaceBox := func(radius float32, strong bool) kit.Surface {
		if strong {
			return surface(gt.UiSurfaceStrongBg, gt.UiSurfaceStrongBorder, gt.UiSurfaceStrongShadow, radius)
		}
		return surface(gt.UiSurfaceBg, gt.UiSurfaceBorder, gt.UiSurfaceShadow, radius)
	}
	panelBox := func() kit.Surface { return withBorder(gt.UiPanelBg, gt.UiPanelBorder, gt.UiPanelShadow, lg) }

	// app-wallpaper-shell：壁纸模式下背景透明（令牌里 alpha 为 0，PaintSurface 不画）。
	paint(ui.Column(c.Key("shell")).Fill(), kit.Surface{Bg: gt.UiShellBg}).Children(func() {
		// 顶栏 h-9 border-b px-2.5 gap-2：两枚 h-5 w-5 的表面夹一条 flex-1 的搜索条。
		bar(ui.Row(c.Key("top")).FillWidth().Height(l.Space(9)).Shrink(0).Items(ui.Center).Gap(l.Space(2)).
			PadSides(0, l.Space(2.5), 1, l.Space(2.5)),
			kit.Surface{Bg: gt.UiToolbarBg, Shadow: gt.UiToolbarShadow}, kit.EdgeBottom, gt.UiToolbarBorder).Children(func() {
			paint(ui.Row(c.Key("left")).Size(l.Space(5), l.Space(5)).Shrink(0), surfaceBox(md, false))
			ui.Row(c.Key("search")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				paint(ui.Row(c).FillWidth().Height(l.Space(5)), withBorder(gt.UiSearchBg, gt.UiSearchBorder, gt.UiSearchShadow, md))
			})
			paint(ui.Row(c.Key("right")).Size(l.Space(5), l.Space(5)).Shrink(0), surfaceBox(md, false))
		})

		ui.Row(c.Key("body")).FillWidth().Grow(1).Basis(0).MinHeight(0).Clip().Children(func() {
			a.previewRail(c, gt, paint, bar, surfaceBox, md, lg)
			a.previewSection(c, gt, paint, bar, surfaceBox, withBorder, panelBox, md, lg, sm)
		})
	})
}

// previewRail 是左栏：w-20 p-2 gap-2，面板加强底、右侧 1px 边线；上面一组条目，tag 区靠底（mt-auto h-8）。
func (a *Appearance) previewRail(c ui.C, gt tokens.Tokens,
	paint func(ui.Box, kit.Surface) ui.Box,
	bar func(ui.Box, kit.Surface, kit.Edge, tokens.Color) ui.Box,
	surfaceBox func(float32, bool) kit.Surface, md, lg float32) {
	l := a.Look
	p := l.Space(2)
	bar(ui.Column(c.Key("rail")).Width(l.Space(20)).Shrink(0).FillHeight().Justify(ui.Between).Gap(l.Space(2)).PadSides(p, p+1, p, p),
		kit.Surface{Bg: gt.UiPanelStrongBg, Shadow: gt.UiPanelStrongShadow}, kit.EdgeRight, gt.UiPanelStrongBorder).Children(func() {
		ui.Column(c.Key("items")).FillWidth().Gap(l.Space(2)).Children(func() {
			// 左栏里的条目带 backdrop-filter（.app-left-rail-glass 把 --app-ui-surface-backdrop 设成模糊，别处的表面是 none）：
			// 渲染层里条目背后的「左栏底色」被合成两遍——一遍是左栏自己的底，一遍在条目的 backdrop 图像里再叠回来，
			// 条目的半透明底色再压在上面（实测：条目中心 = 左栏底色再叠一层同样的白 42% 后，再压条目的白 40% / 30%，
			// 逐通道与渲染层一致；去掉所有 backdrop-filter 后这三条的差异消失）。Viva 没有 backdrop-filter，按这个结果把左栏底色在条目下多垫一遍。
			pill := func(strong bool) kit.Surface {
				s := surfaceBox(md, strong)
				s.Backdrop = gt.UiPanelStrongBg
				return s
			}
			paint(ui.Row(c.Key("strong")).FillWidth().Height(l.Space(5)), pill(true))
			paint(ui.Row(c.Key("one")).FillWidth().Height(l.Space(4)), pill(false))
			paint(ui.Row(c.Key("two")).FillWidth().Height(l.Space(4)), pill(false))
		})
		// .sidebar-tag-section 在壁纸模式下盖过 app-wallpaper-panel：底色是区块令牌、没有阴影。
		paint(ui.Row(c.Key("tags")).FillWidth().Height(l.Space(8)), kit.Surface{Bg: gt.UiSectionBg, Radius: lg})
	})
}

// previewSection 是右边的区块（flex-1，区块底色）：w-28 的列表栏加 flex-1 的详情栏。
func (a *Appearance) previewSection(c ui.C, gt tokens.Tokens,
	paint func(ui.Box, kit.Surface) ui.Box,
	bar func(ui.Box, kit.Surface, kit.Edge, tokens.Color) ui.Box,
	surfaceBox func(float32, bool) kit.Surface,
	withBorder func(bg, border tokens.Color, shadow tokens.Shadow, radius float32) kit.Surface,
	panelBox func() kit.Surface, md, lg, sm float32) {
	l := a.Look
	paint(ui.Row(c.Key("section")).Grow(1).Basis(0).MinWidth(0).FillHeight().Clip(), kit.Surface{Bg: gt.UiSectionBg}).Children(func() {
		// 列表栏 library-list-pane：底色与右边线用列表栏自己的令牌。
		bar(ui.Column(c.Key("pane")).Width(l.Space(28)).Shrink(0).FillHeight().PadRight(1),
			kit.Surface{Bg: gt.LibraryListPaneBg}, kit.EdgeRight, gt.LibraryListPaneBorder).Children(func() {
			// 工具条 h-8 border-b px-2：左边一条 h-2 w-8 的灰块，右边一枚 h-5 w-10 的带边框表面。
			bar(ui.Row(c.Key("bar")).FillWidth().Height(l.Space(8)).Shrink(0).Items(ui.Center).Justify(ui.Between).Gap(l.Space(2)).
				PadSides(0, l.Space(2), 1, l.Space(2)),
				kit.Surface{Bg: gt.UiToolbarBg, Shadow: gt.UiToolbarShadow}, kit.EdgeBottom, gt.UiToolbarBorder).Children(func() {
				paint(ui.Row(c.Key("title")).Size(l.Space(8), l.Space(2)).Shrink(0), kit.Surface{Bg: gt.Foreground.WithAlpha(0.15), Radius: sm})
				paint(ui.Row(c.Key("toggle")).Size(l.Space(10), l.Space(5)).Shrink(0), withBorder(gt.UiSurfaceBg, gt.UiSurfaceBorder, gt.UiSurfaceShadow, md))
			})
			// 三张 h-10 的卡片：第一张是加强表面，后两张是普通表面，都带 1px 边框。
			ui.Column(c.Key("cards")).FillWidth().Grow(1).Basis(0).Pad(l.Space(2)).Gap(l.Space(2)).Children(func() {
				for i := 0; i < 3; i++ {
					s := withBorder(gt.UiSurfaceBg, gt.UiSurfaceBorder, gt.UiSurfaceShadow, lg)
					if i == 0 {
						s = withBorder(gt.UiSurfaceStrongBg, gt.UiSurfaceStrongBorder, gt.UiSurfaceStrongShadow, lg)
					}
					paint(ui.Row(c.Key("card"+string(rune('0'+i)))).FillWidth().Height(l.Space(10)).Shrink(0), s)
				}
			})
		})
		// 详情栏 p-2 gap-2：一条 h-8 w-24 的表面，一块 h-12 与一块撑满的面板。
		ui.Column(c.Key("detail")).Grow(1).Basis(0).MinWidth(0).FillHeight().Pad(l.Space(2)).Gap(l.Space(2)).Children(func() {
			paint(ui.Row(c.Key("head")).Width(l.Space(24)).Height(l.Space(8)).Shrink(0).Self(ui.Start), surfaceBox(lg, false))
			paint(ui.Row(c.Key("summary")).FillWidth().Height(l.Space(12)).Shrink(0), panelBox())
			paint(ui.Row(c.Key("body")).FillWidth().Grow(1).Basis(0).MinHeight(0), panelBox())
		})
	})
}

// stageBase 是舞台自己的底色（舞台根元素的 bg-background）。窗口没开壁纸时是不透明的背景色；窗口开了壁纸时，
// 根上的 app-background-mode-image 让它命中 `.app-background-mode-image .bg-background`（后代选择器：舞台自己带的同名类不算，
// 要祖先带）：浅色 hsl(var(--background) / 0.22)、深色 / 0.3，透出的是窗口自己的壁纸与卡片
// （M3-6c 实测：开着壁纸时舞台内部浅色深色各差 4–8 个灰阶，关着壁纸时逐值相同）。
func (a *Appearance) stageBase(gt tokens.Tokens) tokens.Color {
	if !a.Look.Glass {
		return gt.Background
	}
	if a.Look.Dark {
		return gt.Background.WithAlpha(0.3)
	}
	return gt.Background.WithAlpha(0.22)
}

// previewStageContent 画有图时的舞台内容（在 1px 边框之内，铺满 overflow-hidden 的内框）。
func (a *Appearance) previewStageContent(c ui.C, cur settings.UI) {
	l := a.Look
	gt := tokens.For(l.Dark, true, l.Spec.Hue, l.Spec.Saturation) // 舞台外层固定是壁纸模式
	if a.preview == nil {
		// 缓存的读取在后台线程里跑：只用此刻拷好的局部值，Shell 每帧会改写这两个字段。
		a.preview = kit.NewWallpaperCache(a.LoadBackground, a.Wake)
	}
	opacity := settings.ClampOpacity(cur.BackgroundImageOpacity)
	blur := settings.ClampBlur(cur.BackgroundImageBlur)
	layer := kit.WallpaperLayer{Cache: a.preview, File: cur.BackgroundImageFileName, Opacity: opacity, Blur: blur, Tokens: gt}
	ui.Column(c.Key("mini")).Fill().Pad(1).Children(func() { // 1px：app-settings-input 的边框
		ui.Column(c.Key("canvas")).Fill().Clip().Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: a.stageBase(gt)}) // bg-background：先铺底，壁纸淡出时透出的是它
			l.PaintWallpaperLayer(d, layer)                     // BackgroundImageBackdrop 的位图，或读不出来时损坏的回退图标
			l.PaintSurface(d, kit.Surface{Bg: gt.UiBlanketBg})  // app-wallpaper-blanket 遮罩
		}).Children(func() { a.previewShell(c, gt) })
	})
}
