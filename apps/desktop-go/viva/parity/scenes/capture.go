package scenes

import (
	"context"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	capturescreen "guizhi-mygo-shell/viva/screens/capture"
)

// 快速采集对话框（M3-3：capture.dlg 的空草稿、链接、分享口令、文件加标签加合集四态）的对照场景，与 React 采集器
// （scripts/parity/scenes/react-capture-scenes.mjs、lib/react-capture.mjs）一一对应：同一份夹具（viva/parity/testdata/capture-fixture.json）、
// 同样的操作。整窗对照，对话框开在设置页（默认分区）之上：遮罩与面板压在 rail 与顶栏之上，遮罩背后的模糊由 BackdropSpec 模拟（台账 A-007）。
//
// React 一侧页内替换合集列表（collection.list）与文件选择框（import.selectFiles），Viva 一侧用假服务装配同样的数据；
// 后台动作（读合集、选文件）都同步执行，所以每一步之后的画面是确定的。

// captureFixture 取夹具；它是编译进来的常量，解码失败是程序错误。
func captureFixture() parity.CaptureFixture {
	fix, err := parity.LoadCaptureFixture()
	if err != nil {
		panic(err)
	}
	return fix
}

// captureSteps 是对话框打开之后的操作；先于它的「打开」由场景统一做。dlg 与服务是这一次渲染自己的那一份。
type captureSteps func(s *headless.Session, fix parity.CaptureFixture) error

// captureDialogScene 是整窗（顶栏 + rail + 设置页）上弹着快速采集对话框的场景：steps 非空时在对话框打开后接着操作。
func captureDialogScene(id string, steps captureSteps, waivers ...parity.Waiver) parity.Scene {
	fix := captureFixture()
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		imports := &servicestest.Imports{}
		imports.SetFiles(fix.Scenes.FilesTags.Files)
		library := (&servicestest.Library{}).Seed(services.LibraryCounts{}, fix.Collections, nil)
		dlg := &capturescreen.Dialog{
			Look: look, Ctx: context.Background(), Guard: kit.Guard{}, Imports: imports, Library: library,
			Spawn: func(fn func()) { fn() },
			// 有知识库时 Root 会接上「空白笔记」（root_capture.go），按钮才显示；对照要的是显示按钮的那个状态，点它在这里没有后续。
			Hooks: capturescreen.Hooks{BlankNote: func(string) {}},
		}
		w := settingsWindow(look)
		// 遮罩背后的设置页会显示「语言」下拉的当前值：英文变体里渲染层是 English，这里的设置也要与界面语言一致（运行时取，不在 init 里定）。
		if locales.CurrentLang() == locales.En {
			_ = w.Settings.Store.Update(func(v *settings.Values) { v.App.Language = settings.LangEn })
		}
		w.Dialogs = dlg.View
		return w.View, func(s *headless.Session) error {
			// 打开：与渲染层派发 shortcut:newItem 一致，不经过顶栏的「新建」按钮（那会留下悬停与焦点的痕迹）。
			dlg.Open()
			if err := settleFrames(s, 4); err != nil {
				return err
			}
			if steps != nil {
				if err := steps(s, fix); err != nil {
					return err
				}
			}
			return parity.ParkPointer(s)
		}
	}
	backdrop := &parity.BackdropSpec{Sigma: 12, ScrimAlpha: 0.6, PanelRole: "group", PanelNameFn: locales.CaptureTitle}
	return withBackdrop(parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: slices.Concat(pageSettingsWaivers, []parity.Waiver{overlayInkWaiver}, modalTitleWaivers(locales.CaptureTitle()), modalTitleWaivers(enCaptureTitle), captureEnglishWaivers(), waivers),
	}, backdrop)
}

// settleFrames 出 n 帧并等画面落定（点击与输入的结果上屏）。
func settleFrames(s *headless.Session, n int) error {
	for range n {
		if err := s.Frame(); err != nil {
			return err
		}
	}
	return s.Rest()
}

// typeDraft 往草稿框输入多行文字（草稿框是对话框打开时自动聚焦的那个）：换行用回车键，其余按字符输入。
func typeDraft(s *headless.Session, text string) error {
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			if err := s.Keys("enter"); err != nil {
				return err
			}
		}
		if line == "" {
			continue
		}
		if err := s.Type(line); err != nil {
			return err
		}
	}
	return settleFrames(s, 3)
}

// draftSteps 是「在空草稿框里输入 text」。
func draftSteps(text func(parity.CaptureFixture) string) captureSteps {
	return func(s *headless.Session, fix parity.CaptureFixture) error { return typeDraft(s, text(fix)) }
}

// filesTagsSteps 是「选文件、逐个回车加标签、选合集」，顺序与 React 一侧一致。
func filesTagsSteps(s *headless.Session, fix parity.CaptureFixture) error {
	spec := fix.Scenes.FilesTags
	if err := clickAndSettle(s, gfx.RoleButton, locales.CaptureChooseFiles()); err != nil {
		return err
	}
	if err := clickAndSettle(s, gfx.RoleTextField, locales.CaptureTagsPlaceholder()); err != nil {
		return err
	}
	for _, name := range spec.Tags {
		if err := s.Type(name); err != nil {
			return err
		}
		if err := s.Keys("enter"); err != nil {
			return err
		}
		if err := settleFrames(s, 3); err != nil {
			return err
		}
	}
	return pickOption(s, locales.LibraryCollection(), fix.CollectionName(spec.Collection))
}

func init() {
	registry["capture.dlg.empty"] = captureDialogScene("capture.dlg.empty", nil)
	registry["capture.dlg.urls"] = captureDialogScene("capture.dlg.urls", draftSteps(func(f parity.CaptureFixture) string { return f.Scenes.URLs.Draft }))
	registry["capture.dlg.mixed"] = captureDialogScene("capture.dlg.mixed", draftSteps(func(f parity.CaptureFixture) string { return f.Scenes.Mixed.Draft }))
	registry["capture.dlg.files-tags"] = captureDialogScene("capture.dlg.files-tags", filesTagsSteps)
}
