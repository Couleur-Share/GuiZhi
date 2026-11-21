package library

import (
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 本文件测批量结果条「逐条结果」清单里过长失败原因的折行（台账 B-055）：渲染层是 <li>{id}：{outcome}</li>，
// 一段行内文字，失败原因很长时在行内折行，第二行回到清单项的左缘；编号、冒号、结果仍是三个相邻的文字节点。

// resultRowTexts 取清单里编号为 id 的那一行画出的文字（按纵坐标、横坐标排序）：从编号文字所在的行起，
// 到下一行的编号文字之前。nextID 为空表示取到画面里清单的最后一个文字。
func resultRowTexts(r *rig, id, nextID string) []gfx.Text {
	var (
		all              = r.session.Snapshot().Texts
		top, left, limit = float32(-1), float32(0), float32(math.MaxFloat32)
	)
	for _, tx := range all {
		switch tx.Value {
		case id:
			top, left = tx.Bounds.Y, tx.Bounds.X
		case nextID:
			limit = tx.Bounds.Y
		}
	}
	var out []gfx.Text
	if top < 0 {
		return nil
	}
	for _, tx := range all {
		// 编号文字的左缘就是清单项的左缘：它左边是侧栏与列表，同一纵坐标范围里的别的文字不算。
		if tx.Bounds.X >= left-0.5 && tx.Bounds.Y >= top-0.5 && tx.Bounds.Y < limit-0.5 {
			out = append(out, tx)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if math.Abs(float64(out[i].Bounds.Y-out[j].Bounds.Y)) > 0.5 {
			return out[i].Bounds.Y < out[j].Bounds.Y
		}
		return out[i].Bounds.X < out[j].Bounds.X
	})
	return out
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) <= 0.5 }

// 过长的失败原因折成多行：第一行是「编号」「：」与结果的开头首尾相接，其余各行的起点都回到清单项的左缘
// （与编号文字的左缘一致），不是缩在结果开头的位置；各行的文字拼起来正好是原文；下一条结果接在最后一行后面。
func TestBatchResultsLongFailureWrapsBackToTheRowEdge(t *testing.T) {
	t.Parallel()
	reason := strings.Repeat("磁盘已满，无法写入，", 10)
	r := resultsRig(t, 60, map[int]string{3: reason})
	results := r.view().Batch.Results
	id, next := results[3].ID, results[4].ID
	line := r.screen.Look.Rem(1)

	texts := resultRowTexts(r, id, next)
	if len(texts) < 4 {
		t.Fatalf("编号、冒号与至少两行结果，实际 %d 个文字：%+v", len(texts), texts)
	}
	idText, sep := texts[0], texts[1]
	if idText.Value != id || sep.Value != locales.NativeLibraryResultSeparator() {
		t.Fatalf("第一行应是编号与冒号：%q %q", idText.Value, sep.Value)
	}
	if !approx(sep.Bounds.X, idText.Bounds.X+idText.Bounds.Width) {
		t.Errorf("冒号应紧跟编号：编号右缘 %.2f，冒号左缘 %.2f", idText.Bounds.X+idText.Bounds.Width, sep.Bounds.X)
	}
	rest := texts[2:]
	if !approx(rest[0].Bounds.X, sep.Bounds.X+sep.Bounds.Width) || !approx(rest[0].Bounds.Y, idText.Bounds.Y) {
		t.Errorf("结果的第一段应与冒号同一行、首尾相接：冒号右缘 %.2f，第一段 (%.2f, %.2f)", sep.Bounds.X+sep.Bounds.Width, rest[0].Bounds.X, rest[0].Bounds.Y)
	}
	var joined strings.Builder
	for i, tx := range rest {
		joined.WriteString(tx.Value)
		if i == 0 {
			continue
		}
		if !approx(tx.Bounds.X, idText.Bounds.X) {
			t.Errorf("第 %d 行应回到清单项的左缘（%.2f），实际 %.2f（缩在结果列的左缘就是 B-055 的现象）", i+1, idText.Bounds.X, tx.Bounds.X)
		}
		if !approx(tx.Bounds.Y, idText.Bounds.Y+float32(i)*line) {
			t.Errorf("第 %d 行的纵坐标应是 %.2f，实际 %.2f", i+1, idText.Bounds.Y+float32(i)*line, tx.Bounds.Y)
		}
	}
	if joined.String() != reason {
		t.Errorf("各行文字拼起来应是原文：\n得到 %q\n期望 %q", joined.String(), reason)
	}
	nextY, ok := idYs(r, []string{next})[next]
	if !ok || !approx(nextY, idText.Bounds.Y+float32(len(rest))*line) {
		t.Errorf("下一条应接在 %d 行之后（%.2f），实际 %.2f（画出：%v）", len(rest), idText.Bounds.Y+float32(len(rest))*line, nextY, ok)
	}
}

// 一行放得下的结果仍是三个文字节点在同一行首尾相接：编号、冒号、「成功」或短的失败原因。
func TestBatchResultsShortRowsStayOnOneLine(t *testing.T) {
	t.Parallel()
	r := resultsRig(t, 60, map[int]string{1: "磁盘已满"})
	results := r.view().Batch.Results
	for i, want := range []string{locales.CommonSuccess(), "磁盘已满"} {
		texts := resultRowTexts(r, results[i].ID, results[i+1].ID)
		if len(texts) != 3 {
			t.Fatalf("第 %d 条应是三个文字（编号、冒号、结果），实际 %d 个：%+v", i+1, len(texts), texts)
		}
		if texts[2].Value != want {
			t.Errorf("第 %d 条的结果应是 %q，实际 %q", i+1, want, texts[2].Value)
		}
		for k := 1; k < 3; k++ {
			if !approx(texts[k].Bounds.Y, texts[0].Bounds.Y) || !approx(texts[k].Bounds.X, texts[k-1].Bounds.X+texts[k-1].Bounds.Width) {
				t.Errorf("第 %d 条的第 %d 段应与前一段同行首尾相接：%+v", i+1, k+1, texts)
			}
		}
	}
}

// 窗口宽度变了，折行随之重排，行高也随之重新量：量过的行高不能留着旧宽度下的值，
// 否则变窄之后多出来的那一行会压在下一条结果上（下一条的位置按旧行高算）。
func TestBatchResultsLongFailureReflowsWhenTheWidthChanges(t *testing.T) {
	t.Parallel()
	reason := strings.Repeat("磁盘已满，无法写入，", 6)
	r := resultsRig(t, 60, map[int]string{3: reason})
	results := r.view().Batch.Results
	id, next := results[3].ID, results[4].ID
	line := r.screen.Look.Rem(1)

	linesOf := func() int {
		texts := resultRowTexts(r, id, next)
		return len(texts) - 2
	}
	before := linesOf()
	if err := r.session.Resize(800, 800); err != nil {
		t.Fatalf("改窗口宽度失败：%v", err)
	}
	r.frames(6)
	after := linesOf()
	if after <= before {
		t.Fatalf("窗口变窄之后失败原因应折成更多行：之前 %d 行，之后 %d 行", before, after)
	}
	y, okID := idYs(r, []string{id})[id]
	nextY, okNext := idYs(r, []string{next})[next]
	if !okID || !okNext || !approx(nextY-y, float32(after)*line) {
		t.Fatalf("变窄之后下一条应接在 %d 行之后：行距 %.2f，期望 %.2f", after, nextY-y, float32(after)*line)
	}
}
