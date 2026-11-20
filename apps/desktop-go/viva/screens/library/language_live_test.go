package library

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
)

// 界面语言运行中切换（台账 B-049）：语言是进程级状态，设置里一改，已经画出来的侧栏与列表整页换语言。
// 判据不是「出现了某几个英文词」，而是对拍：运行中切换后的画面，必须与在目标语言下新建的画面逐行相同——
// 缓存了旧语言文字的行、构造时取一次就存下来的文案，都会在对拍里露出来；换回去也要与最初逐行相同。
//
// 这几个测试不并行：语言是进程级状态，它们自己还原成中文；并行测试要等串行测试跑完才恢复，不会撞上。

// langScreenTexts 是画面上从上到下的每一行文字，连同它画在哪里（位置与大小取到 0.01 逻辑像素）：
// 只比文字会漏掉「行高缓存还是旧语言的，后面的行整体错位」这一类缓存残留。
func langScreenTexts(r *rig) []string {
	var out []string
	for _, tx := range r.session.Snapshot().Texts {
		b := tx.Bounds
		out = append(out, fmt.Sprintf("%s @%.2f,%.2f %.2fx%.2f", tx.Value, b.X, b.Y, b.Width, b.Height))
	}
	return out
}

// sameTexts 逐行比较两份画面文字；不同时给出前几处差异。
func sameTexts(t *testing.T, what string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	var diffs []string
	for i := 0; i < max(len(got), len(want)) && len(diffs) < 8; i++ {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			diffs = append(diffs, fmt.Sprintf("第 %d 行：实际 %q，期望 %q", i, g, w))
		}
	}
	t.Errorf("%s：画面文字不一致（实际 %d 行，期望 %d 行）\n  %s", what, len(got), len(want), strings.Join(diffs, "\n  "))
}

func langAtStart(t *testing.T, l locales.Lang) {
	t.Helper()
	locales.SetLang(l)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
}

// 卡片视图：侧栏 + 工具条 + 分页条 + 卡片。
func TestLiveLanguageSwitchRepaintsSidebarAndCards(t *testing.T) {
	langAtStart(t, locales.Zh)
	live := wsRig(t)
	zh := langScreenTexts(live)

	locales.SetLang(locales.En)
	live.frames(3)
	enLive := langScreenTexts(live)
	if slices.Equal(zh, enLive) {
		t.Fatal("切到英文后画面文字一行没变：语言没有传到界面")
	}
	enFresh := langScreenTexts(wsRig(t)) // 此刻进程语言是英文：新建的画面就是英文的
	sameTexts(t, "卡片视图切到英文，与新建的英文画面对拍", enLive, enFresh)

	locales.SetLang(locales.Zh)
	live.frames(3)
	sameTexts(t, "卡片视图换回中文，与最初的中文画面对拍", langScreenTexts(live), zh)
}

// 表格视图：表头、列设置、行内文字都要换。
func TestLiveLanguageSwitchRepaintsTable(t *testing.T) {
	langAtStart(t, locales.Zh)
	live := wsRig(t)
	live.toTable()
	zh := langScreenTexts(live)

	locales.SetLang(locales.En)
	live.frames(3)
	enLive := langScreenTexts(live)
	if slices.Equal(zh, enLive) {
		t.Fatal("切到英文后表格画面文字一行没变：语言没有传到界面")
	}
	fresh := wsRig(t)
	fresh.toTable()
	sameTexts(t, "表格视图切到英文，与新建的英文画面对拍", enLive, langScreenTexts(fresh))

	locales.SetLang(locales.Zh)
	live.frames(3)
	sameTexts(t, "表格视图换回中文，与最初的中文画面对拍", langScreenTexts(live), zh)
}
