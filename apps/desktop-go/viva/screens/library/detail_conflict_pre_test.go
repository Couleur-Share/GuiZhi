package library

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测草稿冲突通知条「比较两份内容」里两栏等宽多段文字的折行（台账 B-063）：渲染层每栏是
// `<pre className="max-h-48 overflow-auto whitespace-pre-wrap">{label}{value}</pre>`，两个相邻的文本节点，折行时第二行回到 pre 的左缘。

// openConflictCompare 打开一条笔记的冲突比较区：服务端的标题与内容换成 serverTitle / serverContent，冲突字段按 fields 给。
func openConflictCompare(t *testing.T, serverTitle, serverContent string, fields ...string) *rig {
	t.Helper()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.setDetail(func(d *DetailView) {
		d.Conflict = &DraftConflict{ItemID: detailNoteID, Server: services.LibraryItem{Title: serverTitle, Content: serverContent}, Fields: fields}
	})
	r.click(gfx.RoleButton, locales.LibraryDraftConflictCompare())
	r.frames(4)
	if !r.hasTextWithPrefix(locales.LibraryDraftConflictSaved()) {
		t.Fatalf("夹具失效：展开之后应画出两栏，实际画面文字 %q", r.screenText())
	}
	return r
}

// labelOf 取画面上文字等于 label 的那个 Label（两栏各一个标签，同名只会有一个）。
func labelOf(t *testing.T, r *rig, label string) gfx.Text {
	t.Helper()
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Value == label {
			return tx
		}
	}
	t.Fatalf("画面上没有标签 %q：%q", label, r.screenText())
	return gfx.Text{}
}

// valueFragments 取标签所在那一栏里属于内容的各截：从标签的行起、左缘不在标签之左、文字是 value 的子串，按行、按横坐标排序。
func valueFragments(r *rig, label gfx.Text, value string, lines int, lineH float32) []gfx.Text {
	var out []gfx.Text
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Value == label.Value || tx.Bounds.X < label.Bounds.X-0.5 || tx.Bounds.Y < label.Bounds.Y-1.5 ||
			tx.Bounds.Y > label.Bounds.Y+float32(lines)*lineH || !strings.Contains(value, strings.TrimSpace(tx.Value)) {
			continue
		}
		out = append(out, tx)
	}
	return out
}

// 一栏里标签与内容是两个相邻的文本节点：第一行标签之后接内容的开头，折行之后其余各行回到栏的左缘（标签的左缘），
// 行距是 text-sm 的 20 px；两栏各自如此，左栏在前（标签先于内容）。
func TestConflictCompareWrapsLabelAndValueAsTwoNodes(t *testing.T) {
	t.Parallel()
	server := strings.Repeat("服务端改过的很长的标题，", 4)
	r := openConflictCompare(t, server, "", services.LibraryDraftFieldTitle)
	line := r.screen.Look.Rem(1.25)

	for _, c := range []struct {
		label, value string
		lines        int // 这一栏内容占的行数上限（再往下是别的元素，如详情标题框，不能算进来）
	}{
		{locales.LibraryDraftConflictSaved(), server, 12},
		{locales.LibraryDraftConflictMine(), r.view().Detail.Item.Title, 1},
	} {
		label := labelOf(t, r, c.label)
		frags := valueFragments(r, label, c.value, c.lines, line)
		if len(frags) == 0 {
			t.Fatalf("%s 这一栏没有内容文字", c.label)
		}
		first := frags[0]
		if math.Abs(float64(first.Bounds.Y-label.Bounds.Y)) > 1.01 || math.Abs(float64(first.Bounds.X-(label.Bounds.X+label.Bounds.Width))) > 0.5 {
			t.Errorf("%s：内容的开头应接在标签之后、同一行：标签 %+v，内容 %+v", c.label, label.Bounds, first.Bounds)
		}
		var joined strings.Builder
		for i, f := range frags {
			joined.WriteString(f.Value)
			if i == 0 {
				continue
			}
			if math.Abs(float64(f.Bounds.X-label.Bounds.X)) > 0.5 {
				t.Errorf("%s 第 %d 行应回到栏的左缘 %.2f，实际 %.2f", c.label, i+1, label.Bounds.X, f.Bounds.X)
			}
			if want := label.Bounds.Y + float32(i)*line; math.Abs(float64(f.Bounds.Y-want)) > 1.01 {
				t.Errorf("%s 第 %d 行纵坐标应是 %.2f（行距 %.0f），实际 %.2f", c.label, i+1, want, line, f.Bounds.Y)
			}
		}
		if strings.TrimSpace(joined.String()) != strings.TrimSpace(c.value) {
			t.Errorf("%s：内容各截连起来应是原文：%q，期望 %q", c.label, joined.String(), c.value)
		}
		if c.label == locales.LibraryDraftConflictSaved() && len(frags) < 3 {
			t.Errorf("夹具应折成至少三行才有意义，实际 %d 行", len(frags))
		}
	}
	// 文本节点的先后：标签一定排在它自己的内容之前（可访问性与文字序列按这个顺序）。
	order := map[string]int{}
	for i, tx := range r.session.Snapshot().Texts {
		if _, seen := order[tx.Value]; !seen {
			order[tx.Value] = i
		}
	}
	savedFirst := strings.TrimSpace(valueFragments(r, labelOf(t, r, locales.LibraryDraftConflictSaved()), server, 12, line)[0].Value)
	if order[locales.LibraryDraftConflictSaved()] > order[savedFirst] {
		t.Errorf("标签应先于内容：标签序号 %d，内容序号 %d", order[locales.LibraryDraftConflictSaved()], order[savedFirst])
	}
}

// 超过 max-h-48（12 rem）的内容在栏里竖向滚动（overflow-auto）：滚轮滚一步，内容上移；折行没有把栏撑高到超过上限。
func TestConflictCompareLongValueScrollsInsideTheColumn(t *testing.T) {
	t.Parallel()
	var rows []string
	for i := 0; i < 60; i++ {
		rows = append(rows, fmt.Sprintf("第%d行", i))
	}
	r := openConflictCompare(t, "", strings.Join(rows, "\n"), services.LibraryDraftFieldContent)
	saved := labelOf(t, r, locales.LibraryDraftConflictSaved())
	_, haveLast := func() (gfx.Text, bool) {
		for _, tx := range r.session.Snapshot().Texts {
			if tx.Value == rows[len(rows)-1] {
				return tx, true
			}
		}
		return gfx.Text{}, false
	}()
	if !haveLast {
		t.Fatal("前提：60 行都应声明（滚动区里的内容整块声明，裁剪由滚动区做）")
	}
	if err := r.session.Scroll(gfx.ScrollEvent{X: saved.Bounds.X + 10, Y: saved.Bounds.Y + 30, DeltaY: 80, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	r.frames(3)
	after := labelOf(t, r, locales.LibraryDraftConflictSaved())
	if !(after.Bounds.Y < saved.Bounds.Y-40) {
		t.Errorf("滚轮之后栏里的内容应上移：之前 %.2f，之后 %.2f", saved.Bounds.Y, after.Bounds.Y)
	}
	// 左栏（我的修改）没有被滚动：各栏的滚动区各自独立。
	if mine := labelOf(t, r, locales.LibraryDraftConflictMine()); math.Abs(float64(mine.Bounds.Y-saved.Bounds.Y)) > 1.01 {
		t.Errorf("另一栏不该跟着滚动：%.2f 对 %.2f", mine.Bounds.Y, saved.Bounds.Y)
	}
}

// 两栏都放得下一行时（宽内容区）标签与内容各一个 Label 同一行；空值只剩标签。
func TestConflictCompareShortAndEmptyValues(t *testing.T) {
	t.Parallel()
	r := openConflictCompare(t, "", "", services.LibraryDraftFieldTitle)
	saved := labelOf(t, r, locales.LibraryDraftConflictSaved())
	if frags := valueFragments(r, saved, "x", 3, r.screen.Look.Rem(1.25)); len(frags) != 0 {
		t.Errorf("服务端标题为空时这一栏只剩标签：%+v", frags)
	}
	mine := labelOf(t, r, locales.LibraryDraftConflictMine())
	title := r.view().Detail.Item.Title
	frags := valueFragments(r, mine, title, 3, r.screen.Look.Rem(1.25))
	if len(frags) == 0 || math.Abs(float64(frags[0].Bounds.Y-mine.Bounds.Y)) > 1.01 {
		t.Errorf("我的标题应与标签同一行：%+v", frags)
	}
}
