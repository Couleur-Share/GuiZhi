package inbox

import (
	"testing"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测「主题页任务」入口的接线（台账 B-064）：接了 Hooks.ThemedTasks 就打开任务页代替工作区，没接的退路由
// screen_test.go 的 TestThemedTasksEntryExplainsNotMigratedWithoutBreakingTheList 守着。任务页本身（列表、进度、跳转）在 viva/screens/themed 里测，
// 这里用一个会画标记文字和返回钮的替身验证宿主这一侧：入口、代替关系、返回、离开复位。

const themedMarker = "themed-tasks-page"

// withThemedTasks 接一个替身任务页：画标记文字和「返回处理中心」钮，并记下被画了几帧。
func withThemedTasks(frames *int, backs *int) func(*uiRig) {
	return func(r *uiRig) {
		l := r.screen.Look
		r.screen.Hooks.ThemedTasks = func(c ui.C, back func()) {
			*frames++
			l.Text(c.Key("marker"), themedMarker, kit.TextSM, kit.WeightNormal, l.T.Foreground)
			if l.Button(c.Key("back"), kit.ButtonSpec{Label: locales.ThemedReadingBackToInbox()}, kit.ButtonStyle{}).Clicked() {
				*backs++
				back()
			}
		}
	}
}

func TestThemedTasksEntryOpensTheTasksPageInPlaceOfTheList(t *testing.T) {
	var frames, backs int
	r := newUIRig(t, boardItems(), withThemedTasks(&frames, &backs))
	if r.hasText(themedMarker) || frames != 0 {
		t.Fatal("没点入口时不该画任务页")
	}
	r.clickButton(locales.ThemedReadingTasks())
	if !r.hasText(themedMarker) {
		t.Fatalf("点入口应打开任务页；画面上有 %q", r.session.Texts())
	}
	if r.hasText("复核一") {
		t.Fatal("任务页整个代替工作区，列表不该还在")
	}
	if q := r.notifier.Queued(); len(q) != 0 {
		t.Fatalf("接线后点入口不该再弹「尚未迁移」：%+v", q)
	}
	if got := r.log.entries(); len(got) != 0 {
		t.Fatalf("不该写错误日志：%v", got)
	}
}

func TestThemedTasksPageBackReturnsToTheList(t *testing.T) {
	var frames, backs int
	r := newUIRig(t, boardItems(), withThemedTasks(&frames, &backs))
	r.clickButton(locales.ThemedReadingTasks())
	r.clickButton(locales.ThemedReadingBackToInbox())
	if backs != 1 {
		t.Fatalf("返回应回调一次，实际 %d", backs)
	}
	if r.hasText(themedMarker) || !r.hasText("复核一") {
		t.Fatalf("返回后应回到列表；画面上有 %q", r.session.Texts())
	}
}

func TestThemedTasksPageResetsWhenTheModuleIsLeft(t *testing.T) {
	var frames, backs int
	r := newUIRig(t, boardItems(), withThemedTasks(&frames, &backs))
	r.clickButton(locales.ThemedReadingTasks())
	r.hidden = true
	r.frames(3)
	drawn := frames
	r.frames(3)
	if frames != drawn {
		t.Fatal("离开处理中心后不该再画任务页")
	}
	r.hidden = false
	r.frames(3)
	if r.hasText(themedMarker) || !r.hasText("复核一") {
		t.Fatalf("回到处理中心应是列表而不是上次的任务页；画面上有 %q", r.session.Texts())
	}
}
