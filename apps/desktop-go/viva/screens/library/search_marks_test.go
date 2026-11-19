package library

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 搜索命中词高亮（台账 B-041）的界面级测试：卡片与表格里，标题与摘要里的命中词画成 mark（字色 amber-950），
// 没有搜索词、搜索词没有命中时不画。mark 的版面与像素在 kit/marktext_test.go 里对着 React 的实测值逐项核过，这里只测接线：
// 谁在什么条件下把搜索词交给了文字，以及清空搜索之后是否恢复原样。

// markedEntries 是三条条目：
//   - m1：标题里一处、摘要里两处「设计」；
//   - m2：标题含拉丁字母（大小写不同的搜索词也要命中，且保留原文的大小写），摘要不含「设计」；
//   - m3：没有标题（显示「无标题」），摘要里有「无标题」三个字——卡片只标真标题，空标题的「无标题」不标；表格标连「无标题」。
func markedEntries() []services.LibraryEntry {
	const t0 int64 = 1_794_790_800_000
	mk := func(id, title, snippet string, i int64) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, Snippet: snippet, ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: t0 - i*90_000, UpdatedAt: t0 - i*60_000,
			Tags: []services.LibraryTag{},
		}
	}
	return []services.LibraryEntry{
		mk("m1", "产品设计原则", "好的设计是尽可能少的设计", 0),
		mk("m2", "Designing Data", "no hit in the snippet", 1),
		mk("m3", "", "这条的无标题不是占位", 2),
	}
}

// markedTexts 是画面上字色为 mark 字色（amber-950）的文字，按出现顺序。
func (r *rig) markedTexts() []string {
	want := r.look.Col(tokens.Tailwind("amber-950"))
	var out []string
	for _, t := range r.session.Snapshot().Texts {
		if t.Style.Color == want {
			out = append(out, t.Value)
		}
	}
	return out
}

func TestSearchHitsAreMarkedInCardsAndTable(t *testing.T) {
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(markedEntries()) })
	expect := func(step string, want ...string) {
		t.Helper()
		r.frames(4)
		if got := r.markedTexts(); !slices.Equal(got, want) {
			t.Fatalf("%s：命中词高亮 = %q，期望 %q", step, got, want)
		}
	}
	search := func(text string) { r.store.SetSearch(r.screen.Ctx, text) }

	expect("没有搜索词")

	// 卡片：m1 标题里一处、摘要里两处；其余条目不含「设计」，不在结果里。
	search("设计")
	expect("卡片 设计", "设计", "设计", "设计")

	// 卡片：m3 的标题是空的，「无标题」是占位，不标；摘要里真有这三个字，标。
	search("无标题")
	expect("卡片 无标题", "无标题")

	// 表格：标题按钮连「无标题」也标（渲染层的表格高亮的是解析后的标题），摘要单元格照标。
	r.click(gfx.RoleButton, locales.LibraryViewList())
	r.frames(6)
	expect("表格 无标题", "无标题", "无标题")

	// 表格：标题按钮一处、摘要单元格两处。
	search("设计")
	expect("表格 设计", "设计", "设计", "设计")

	// 搜索词两端的空白不算（渲染层 highlightText 先 trim），不区分大小写，命中词保留原文的大小写。
	search("  DESIGNING ")
	expect("表格 大小写与空白", "Designing")

	// 清空搜索：恢复原样。
	search("")
	expect("清空搜索")

	// 搜索词没有命中任何条目：空结果，不画高亮（也不崩）。
	search("没有任何条目含有这个词")
	expect("没有命中")
}
