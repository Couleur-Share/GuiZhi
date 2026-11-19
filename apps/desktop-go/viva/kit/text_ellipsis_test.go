package kit

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 渲染层（Chromium）的 text-overflow: ellipsis 不去掉截断点前的空格（「alpha beta …」）；
// Viva 的省略号逻辑把前缀右侧的空格去掉（「alpha beta…」，gfx/wrap.go 的 trimSpaceRight，只认 ASCII 空格，没有选项）。
// 这是台账里登记的已知偏差（A-025；原 C-006，用户 2026-11-19 裁决接受）：这条测试把「Viva 现在就是这样」写成可执行的事实——截断点正好在空格之前时，
// 省略号前没有空格；哪天 Viva 升级后保留了空格，这条测试失败，台账条目就该关闭（那时 library 场景里的 ellipsisSpaceWaivers 也一并去掉）。
const ellipsisSample = "alpha beta gamma delta epsilon"

func TestVivaEllipsisDropsTheSpaceBeforeTheCut(t *testing.T) {
	const n, rowH = 60, 24
	black := tokens.RGBA(0, 0, 0, 1)
	h := openPage(t, still(), 450, n*rowH+20, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Children(func() {
			for i := 0; i < n; i++ {
				vc := c.Key(fmt.Sprintf("viva-%d", i))
				ui.Column(vc).Absolute().Left(10).Top(float32(i * rowH)).Width(float32(30 + i*3)).Children(func() {
					ui.Text(vc.Key("t"), ellipsisSample).FontSize(14).LineHeight(20).Color(l.Col(black)).Ellipsis().FillWidth()
				})
			}
		})
	})
	h.frames(3)
	type placed struct {
		y     float32
		value string
	}
	var rows []placed
	for _, tx := range h.session.Snapshot().Texts {
		rows = append(rows, placed{y: tx.Bounds.Y, value: tx.Value})
	}
	if len(rows) != n {
		t.Fatalf("文字行数 %d，期望 %d", len(rows), n)
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].y < rows[b].y })
	// cutBeforeSpace 数「截断点紧挨着一个空格」的宽度：Chromium 在这些行里会留着这个空格。
	cutBeforeSpace := 0
	for i, row := range rows {
		if !strings.HasSuffix(row.value, "…") {
			continue
		}
		prefix := strings.TrimSuffix(row.value, "…")
		if strings.HasSuffix(prefix, " ") || strings.HasSuffix(prefix, "\u00a0") {
			t.Fatalf("行 %d：Viva 的省略号前保留了空格 %q，台账 A-025 可以关闭了", i, row.value)
		}
		if strings.HasPrefix(ellipsisSample, prefix) && strings.HasPrefix(ellipsisSample[len(prefix):], " ") {
			cutBeforeSpace++
		}
	}
	if cutBeforeSpace == 0 {
		t.Fatalf("%d 个宽度里没有一个恰好在空格前截断，用例没有覆盖到这个偏差", n)
	}
}
