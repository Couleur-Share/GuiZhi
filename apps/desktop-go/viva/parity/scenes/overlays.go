package scenes

import (
	"slices"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	"guizhi-mygo-shell/viva/screens/overlays"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// 全局浮层（M3-6a：命令面板，以及更新、首次使用设置清单、旧版数据迁移三个对话框）的整窗对照场景，与 React 采集器
// （scripts/parity/scenes/react-overlays-scenes.mjs、lib/react-overlays.mjs）一一对应：同一份夹具（viva/parity/testdata/overlays-fixture.json）、
// 同样的操作。整窗对照，浮层开在设置页（默认分区）之上：遮罩与面板压在 rail 与顶栏之上，遮罩背后的模糊由 BackdropSpec 模拟（台账 A-007）。
//
// React 一侧在页内替换远程搜索与版本、迁移探测等读取接口，Viva 一侧用假服务装配同样的数据；后台动作与防抖都同步执行，所以每一步之后的画面是确定的。
//
// 夹具里有两处是 Viva 在渲染层之外多画的信息，场景不把它交给 Viva（渲染层不显示，两边画面才可比）：
// 更新对话框的发布日期（UpdateStatus.ReleaseDate 留空）与迁移确认页的来源路径（LegacyDetection.Path 留空）。它们由界面级 L4 用例覆盖。

// overlaysFixture 取夹具；它是编译进来的常量，解码失败是程序错误。
func overlaysFixture() parity.OverlaysFixture {
	fix, err := parity.LoadOverlaysFixture()
	if err != nil {
		panic(err)
	}
	return fix
}

// overlaysEnv 让浮层的后台动作与防抖同步执行（对照要确定）。
func overlaysEnv() overlays.Env {
	return overlays.Env{
		Spawn: func(fn func()) { fn() },
		After: func(_ time.Duration, fn func()) { fn() },
	}
}

// overlaysRig 是一次渲染自己的浮层界面与整窗：状态不跨渲染共享。
type overlaysRig struct {
	screen *overlays.Screen
	window func() (view func(c ui.C))
}

// overlaysScene 装配一个全局浮层场景：setup 往界面里装配要演示的浮层并返回打开它的步骤（页面函数在 setup 之后取，浮层已挂在 Window.Dialogs 上）；
// pill 为真时遮罩背后的顶栏带「有新版本」按钮；模糊背景的「打开之前」渲染（Build）只画遮罩背后的整窗，不带浮层。
func overlaysScene(id string, backdrop *parity.BackdropSpec, pill bool,
	setup func(look *kit.Look, rig *overlaysRig) func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		rig := &overlaysRig{screen: &overlays.Screen{
			Look: look, Guard: kit.Guard{Notify: kit.NewNotifier(nil)},
			OpenExternal: func(string) error { return nil }, // 有外部打开钩子，更新说明里的链接才画成链接
		}}
		open := setup(look, rig)
		w := overlaysWindow(look, pill)
		w.Dialogs = rig.screen.View
		return w.View, open
	}
	return withBackdrop(parity.Scene{
		ID:      id,
		Build:   overlaysBase(pill),
		Prepare: prepare,
		Waivers: slices.Concat(pageSettingsWaivers, []parity.Waiver{overlayInkWaiver}, waivers),
	}, backdrop)
}

// overlaysWindow 是遮罩背后的整窗（顶栏 + rail + 设置页，默认分区）：pill 为真时顶栏带「有新版本」按钮。
func overlaysWindow(look *kit.Look, pill bool) *shellscreen.Window {
	w := settingsWindow(look)
	// 遮罩背后的设置页会显示「语言」下拉的当前值：英文变体里渲染层是 English，这里的设置也要与界面语言一致（运行时取，不在 init 里定）。
	if locales.CurrentLang() == locales.En {
		_ = w.Settings.Store.Update(func(v *settings.Values) { v.App.Language = settings.LangEn })
	}
	if pill {
		w.Update, w.UpdateVersion = true, updateVersion
	}
	return w
}

// overlaysBase 是不带浮层的整窗：遮罩背后的页面。
func overlaysBase(pill bool) func(look *kit.Look) func(c ui.C) {
	return func(look *kit.Look) func(c ui.C) { return overlaysWindow(look, pill).View }
}

// paletteBackdrop 是命令面板的遮罩（bg-background/55 加 backdrop-blur-sm，4px）：遮罩之上是名为「全局命令面板」的对话框面板（台账 A-007）。
func paletteBackdrop() *parity.BackdropSpec {
	return &parity.BackdropSpec{Sigma: 4, ScrimAlpha: 0.55, PanelRole: "group", PanelNameFn: locales.CommandPaletteTitle, DialogFirst: true}
}

// overlayModalBackdrop 是 Modal 对话框的遮罩（backdrop-blur-md，12px）：面板名在比较时才取（英文变体要等语言设定之后）。
func overlayModalBackdrop(name func() string) *parity.BackdropSpec {
	spec := modalBackdrop("")
	spec.PanelNameFn = name
	return spec
}

// palettePrepare 装配命令面板：知识库、Wiki、采集任务三个远程来源取夹具里的命中，备份服务不装配（场景不执行动作）。
func palettePrepare(look *kit.Look, rig *overlaysRig) *overlays.Palette {
	fix := overlaysFixture()
	entries := make([]services.LibraryEntry, 0, len(fix.Palette.Knowledge))
	for _, k := range fix.Palette.Knowledge {
		entries = append(entries, services.LibraryEntry{
			ID: k.ID, Title: k.Title, Snippet: k.Snippet, ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, Tags: []services.LibraryTag{},
		})
	}
	library := (&servicestest.Library{}).SeedEntries(entries)
	wiki := &servicestest.WikiFake{}
	for _, h := range fix.Palette.Wiki {
		wiki.PutPage(services.WikiPage{ID: h.ID, Title: h.Title, Kind: h.Kind, Summary: h.Summary})
	}
	tasks := make([]services.ImportTask, 0, len(fix.Palette.Tasks))
	for _, t := range fix.Palette.Tasks {
		tasks = append(tasks, services.ImportTask{ID: t.ID, DisplayName: t.DisplayName, Status: t.Status, Error: t.Error})
	}
	imports := &servicestest.Imports{}
	imports.Set(services.ImportPage{Entries: tasks}, nil)

	p := overlays.NewPalette(rig.screen.Guard, nil)
	p.Env = overlaysEnv()
	p.Library, p.Wiki, p.Imports = library, wiki, imports
	rig.screen.Palette = p
	return p
}

// paletteScene 是命令面板场景：打开面板（与渲染层派发 shortcut:search 一致，不经过顶栏的搜索框，那会留下悬停与焦点的痕迹）；
// typeQuery 为真时再输入夹具里的搜索词。搜索框的焦点留在框里（打开即聚焦，是用户看到的状态），所以不清焦点。
func paletteScene(id string, typeQuery bool, waivers ...parity.Waiver) parity.Scene {
	return overlaysScene(id, paletteBackdrop(), false,
		func(look *kit.Look, rig *overlaysRig) func(s *headless.Session) error {
			p := palettePrepare(look, rig)
			return func(s *headless.Session) error {
				p.Open()
				if err := settleFrames(s, 4); err != nil {
					return err
				}
				if typeQuery {
					if err := s.Type(overlaysFixture().Palette.Query); err != nil {
						return err
					}
					if err := settleFrames(s, 4); err != nil {
						return err
					}
				}
				return parity.ParkPointer(s)
			}
		}, waivers...)
}

// updateScene 是更新对话框「发现新版本」：主进程推 updater:status = available（顶栏出现更新按钮），点按钮打开对话框。
// Viva 一侧由外壳置 Window.Update 与 UpdateVersion，更新状态层并入同一条推送再打开对话框（打开时强制检查一次，结果与推送一致）。
func updateScene(id string, waivers ...parity.Waiver) parity.Scene {
	return overlaysScene(id, overlayModalBackdrop(locales.SettingsCheckUpdate), true,
		func(look *kit.Look, rig *overlaysRig) func(s *headless.Session) error {
			fix := overlaysFixture()
			updater := &servicestest.Updater{}
			updater.SetResult(services.UpdateCheckResult{Available: true, Version: fix.Update.Version})
			u := overlays.NewUpdate(updater, rig.screen.Guard, nil)
			u.Env = overlaysEnv()
			u.CurrentVersion = func() string { return fix.Update.CurrentVersion }
			rig.screen.Update = u
			return func(s *headless.Session) error {
				// 渲染层不显示发布日期，夹具推送里不带（见文件头）。
				u.OnStatus(services.UpdateStatus{State: services.UpdateStateAvailable, Version: fix.Update.Version, ReleaseNotes: fix.Update.ReleaseNotes})
				u.Open()
				if err := settleFrames(s, 4); err != nil {
					return err
				}
				return parity.ParkPointer(s)
			}
		}, slices.Concat(updateTitleWaivers(), updateNotesWaivers(), updateEnglishWaivers(), []parity.Waiver{updateColonWaiver()})...)
}

// setupScene 是首次使用设置清单：文本模型没配（「快速配置」）、转写与语义检索都没配；夹具要求展开「更多可选设置」时再点开折叠钮。
func setupScene(id string) parity.Scene {
	return overlaysScene(id, overlayModalBackdrop(locales.SetupTitle), false,
		func(look *kit.Look, rig *overlaysRig) func(s *headless.Session) error {
			fix := overlaysFixture()
			capture := &servicestest.Capture{}
			capture.SetFunasrStatus(services.FunasrStatus{})
			setup := overlays.NewSetup(rig.screen.Guard, nil)
			setup.Env = overlaysEnv()
			setup.AI, setup.Prefs, setup.CaptureTools = &servicestest.AISettingsFake{}, &servicestest.Prefs{}, capture
			rig.screen.Setup = setup
			return func(s *headless.Session) error {
				setup.RequestOpen()
				if err := settleFrames(s, 4); err != nil {
					return err
				}
				if fix.Setup.OptionalOpen {
					if err := parity.ClickRole(s, gfx.RoleButton, locales.SetupMoreOptional(2)); err != nil {
						return err
					}
					if err := settleFrames(s, 4); err != nil {
						return err
					}
				}
				return parity.ParkAndBlur(s)
			}
		}, slices.Concat(setupTitleWaivers(), setupEnglishWaivers())...)
}

// migrationScene 是旧版数据迁移确认：启动时探测到旧库（条目数取夹具），弹出确认页。
// 渲染层不显示来源路径，探测结果里的路径不交给 Viva（见文件头）。
func migrationScene(id string) parity.Scene {
	return overlaysScene(id, overlayModalBackdrop(locales.MigrationTitle), false,
		func(look *kit.Look, rig *overlaysRig) func(s *headless.Session) error {
			fix := overlaysFixture()
			service := &servicestest.Migration{}
			service.SetDetection(services.LegacyDetection{ItemCount: fix.Migration.ItemCount}, true)
			m := overlays.NewMigration(rig.screen.Guard, nil)
			m.Env = overlaysEnv()
			m.Service, m.Prefs = service, &servicestest.Prefs{}
			rig.screen.Migration = m
			return func(s *headless.Session) error {
				m.Detect()
				if err := settleFrames(s, 4); err != nil {
					return err
				}
				return parity.ParkAndBlur(s)
			}
		}, slices.Concat(migrationTitleWaivers(), migrationEnglishWaivers())...)
}

func init() {
	registry["app.window.overlays.palette-default"] = paletteScene("app.window.overlays.palette-default", false, overlaysEnglishBackdropWaivers()...)
	registry["app.window.overlays.palette-search"] = paletteScene("app.window.overlays.palette-search", true, slices.Concat(paletteSnippetWaivers(), paletteEnglishWaivers())...)
	registry["app.window.overlays.update-available"] = updateScene("app.window.overlays.update-available")
	registry["app.window.overlays.setup-checklist"] = setupScene("app.window.overlays.setup-checklist")
	registry["app.window.overlays.migration-confirm"] = migrationScene("app.window.overlays.migration-confirm")
}
