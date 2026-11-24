package settings

import (
	"strconv"
	"sync"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// Appearance 是「显示设置」页（渲染层 AppearanceSettings）：主题模式、主题颜色、字体大小、动画、背景图五个分区。
//
// 页面自己不存设置：每一帧从 Store 读，改动交给 Store 写回；Store 变更后外壳按新设置重装外观（kit.Look），
// 所以点主题模式、色板、字号，整个界面马上换成新外观——与渲染层「改 store → 重新渲染」同一条路径。
type Appearance struct {
	Look  *kit.Look
	Store Store
	// Guard 接住写入失败：弹出带原因的错误提示并记日志（B-002）；零值时失败只会静默回到原值。
	Guard kit.Guard
	// PickBackground 打开系统图片选择框，把选中的图片存进图片目录，返回存下的文件名；用户取消时返回空串。
	// 在后台线程里调用，完成后外壳用 Wake 请求重绘。nil 表示宿主还没接上选图。
	PickBackground func() (fileName string, err error)
	// LoadBackground 按文件名读出应用图片目录里的背景图字节（预览舞台用）；在后台线程里调用，可以阻塞。
	// nil 表示宿主没接上读图，预览按「读取失败」画损坏的回退图标。预览缓存在第一次需要时按此刻的值建好，之后换这个字段不会生效。
	LoadBackground func(fileName string) ([]byte, error)
	// Wake 请求界面重绘（后台选图与读图完成时调用，任意线程）。
	Wake func()

	picking     bool           // 正在选图（渲染层 isPickingBackground）：按钮禁用，防重入
	pickMu      sync.Mutex     // 保护 pickResult
	pickResult  *pickOutcome   // 后台选图的结果，由下一帧取走
	opacity     kit.RangeState // 背景可见度滑块
	blur        kit.RangeState // 虚化强度滑块
	opacitySeen float64        // 上次灌进可见度滑块的设置值（百分比），见 syncSlider
	blurSeen    float64        // 上次灌进虚化滑块的设置值
	hexField    ui.Field
	picker      kit.ColorPickerState // 自定义颜色的十六进制文本框
	sideBySide  bool                 // 两个滑块是否并排（md 断点以上），每帧由背景图分区算出
	swatchRow   kit.EqualCells       // 主题色带的版面（跨帧保留；Center 在 themeColor 里每帧置位）
	// preview 是预览舞台的壁纸位图缓存：第一次画有图的舞台时才建，离开分区或设置页时 Close 并丢弃（无图时不占内存）。
	preview *kit.WallpaperCache
}

// Close 释放预览舞台的壁纸位图缓存；可重复调用，之后再画有图的舞台会重新建缓存、重新读取。
func (a *Appearance) Close() {
	if a.preview != nil {
		a.preview.Close()
		a.preview = nil
	}
}

// PreviewRetained 返回预览舞台缓存当前占用的位图字节数；没有缓存（无图、已离开分区）时为 0。供测试与内存排查。
func (a *Appearance) PreviewRetained() int {
	if a.preview == nil {
		return 0
	}
	return a.preview.Retained()
}

// pickOutcome 是后台选图的结果。
type pickOutcome struct {
	fileName string
	err      error
}

// save 把一次修改交给 Store，返回是否写入成功（runGuardedMutation 的语义）。
func (a *Appearance) save(mutate func(*settings.Values)) bool {
	return save(a.Guard, a.Store, mutate)
}

// Page 声明整页：标题加五个分区。
func (a *Appearance) Page(c ui.C) {
	l := a.Look
	a.finishPick()
	l.SettingsContent(c, locales.SettingsAppearance(), func() {
		l.SettingSections(c, func() {
			a.themeMode(c)
			a.themeColor(c)
			a.fontSize(c)
			a.motion(c)
			a.backgroundImage(c)
		})
	})
}

// themeMode 是「主题模式」分区：浅色、深色、跟随系统三个等宽分段按钮。
func (a *Appearance) themeMode(c ui.C) {
	l := a.Look
	cur := a.Store.Values().UI.ThemeMode
	modes := []struct {
		id    settings.ThemeMode
		label string
		icon  gfx.Icon
	}{
		{settings.ThemeLight, locales.SettingsLight(), icons.Sun},
		{settings.ThemeDark, locales.SettingsDark(), icons.Moon},
		{settings.ThemeSystem, locales.SettingsSystem(), icons.Monitor},
	}
	l.SettingSectionBody(c, locales.SettingsThemeMode(), func() {
		ui.Column(c).FillWidth().Pad(l.Space(4)).Children(func() {
			l.SegmentGrid(c, func() {
				for _, m := range modes {
					seg := l.Segment(c.Key(string(m.id)), kit.SegmentSpec{Name: m.label, On: cur == m.id, Content: func(fg tokens.Color) {
						ui.Row(c).Items(ui.Center).Gap(l.Space(2)).Children(func() {
							l.Icon(c, m.icon, l.Rem(1), fg)
							l.TextPx(c, m.label, 13, 19.5, kit.WeightMedium, fg)
						})
					}})
					if seg.Clicked() {
						a.save(func(v *settings.Values) { v.UI.ThemeMode = m.id })
					}
				}
			})
		})
	})
}

// fontSize 是「字体大小」分区：小、中、大三档，每档两行（名称与像素值）。
func (a *Appearance) fontSize(c ui.C) {
	l := a.Look
	cur := a.Store.Values().UI.FontSize
	names := map[string]string{
		"small": locales.SettingsFontSmall(), "medium": locales.SettingsFontMedium(), "large": locales.SettingsFontLarge(),
	}
	l.SettingSectionBody(c, locales.SettingsFontSize(), func() {
		ui.Column(c).FillWidth().Pad(l.Space(4)).Children(func() {
			l.SegmentGrid(c, func() {
				for _, f := range tokens.FontSizes {
					name, px := names[f.ID], strconv.Itoa(f.Px)
					seg := l.Segment(c.Key(f.ID), kit.SegmentSpec{
						// aria-label 是「名称, 像素px」
						Name: name + ", " + px + "px", On: string(cur) == f.ID,
						Content: func(fg tokens.Color) {
							ui.Column(c).Items(ui.Center).Children(func() {
								l.TextPx(c, name, 13, 19.5, kit.WeightMedium, fg)
								// span.block text-[11px] opacity-70 mt-0.5：{size.value}px 在 DOM 里是两个文本节点
								l.TextRunsPx(c, 11, 16.5, kit.WeightMedium, kit.Fade(fg, 0.7), px, "px").MarginTop(l.Space(0.5))
							})
						},
					})
					if seg.Clicked() {
						a.save(func(v *settings.Values) { v.UI.FontSize = settings.FontSize(f.ID) })
					}
				}
			})
		})
	})
}

// motion 是「动画」分区：一段说明加四个等宽分段按钮。
func (a *Appearance) motion(c ui.C) {
	l := a.Look
	t := l.T
	cur := a.Store.Values().UI.MotionPreference
	options := []struct {
		id    settings.MotionPreference
		label string
	}{
		{settings.MotionSystem, locales.SettingsMotionSystem()},
		{settings.MotionOff, locales.SettingsMotionOff()},
		{settings.MotionReduced, locales.SettingsMotionReduced()},
		{settings.MotionStandard, locales.SettingsMotionStandard()},
	}
	l.SettingSectionBody(c, locales.SettingsMotionTitle(), func() {
		ui.Column(c).FillWidth().Pad(l.Space(4)).Gap(l.Space(3)).Children(func() {
			l.Text(c, locales.SettingsMotionDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			l.SegmentGrid(c, func() {
				for _, o := range options {
					seg := l.Segment(c.Key(string(o.id)), kit.SegmentSpec{Name: o.label, On: cur == o.id, Content: func(fg tokens.Color) {
						l.TextPx(c, o.label, 13, 19.5, kit.WeightMedium, fg)
					}})
					if seg.Clicked() {
						a.save(func(v *settings.Values) { v.UI.MotionPreference = o.id })
					}
				}
			})
		})
	})
}
