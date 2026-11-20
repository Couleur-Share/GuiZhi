package library

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 表格的类型格（台账 B-051）：td 里的普通文字，没有 whitespace-nowrap，放不下时在空格处折行，行高跟着长；
// 放不进内容宽的单词不折也不裁，从内容盒的左缘起整词溢出。英文的「Web page」比内容宽 52px 的格子宽（折成两行，行高 +8px），
// 「Document」是一个词、比格子宽（整词溢出）；中文标签（「网页」）与英文的短标签都不折。
// 读数来自渲染层（英文表格变体，`light-royal-blue-medium-1280x800`）：「Web」[1037.81 275 26.38 15]、「page」[1036.69 291 28.62 15]，
// 「Document」[1025 507 60.11 15]；这里用的是同一份判据，不是新的期望值。
//
// 这几个测试改进程语言，所以不并行，结束时还原（并行测试要等串行测试跑完才恢复，不会撞上）。

// typeCellEntries 是三条类型不同的条目：网页（英文标签「Web page」，折成两行）、文档（「Document」，一个词）、笔记（「Note」，短）。
func typeCellEntries() []services.LibraryEntry {
	const t0 int64 = 1_794_790_800_000
	mk := func(id, title, itemType string, i int64) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, Snippet: "摘要", ItemType: itemType, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: t0 - i*90_000, UpdatedAt: t0 - i*60_000,
			Tags: []services.LibraryTag{},
		}
	}
	return []services.LibraryEntry{
		mk("t1", "网页条目", "webpage", 0),
		mk("t2", "文档条目", "document", 1),
		mk("t3", "笔记条目", "note", 2),
	}
}

// typeCellRig 是已切到表格视图的夹具（界面语言取调用时的进程语言，调用前用 langAtStart 设好）。
func typeCellRig(t *testing.T) *rig {
	t.Helper()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(typeCellEntries()) })
	r.toTable()
	r.frames(4)
	return r
}

// paintedLine 是画面上的一行文字：位置取文字盒，宽度 W 取画出来的宽度（与对照工具同一口径：优先用渲染器量出的宽度），Box 是文字盒的宽。
type paintedLine struct{ X, Y, W, Box float32 }

// linesOf 是画面上值为 value 的文字行（可能不止一处）。
func linesOf(r *rig, value string) []paintedLine {
	snap := r.session.Snapshot()
	var out []paintedLine
	for i, tx := range snap.Texts {
		if tx.Value != value {
			continue
		}
		w := tx.Bounds.Width
		if i < len(snap.TextWidths) && snap.TextWidths[i] > 0 {
			w = snap.TextWidths[i]
		}
		out = append(out, paintedLine{tx.Bounds.X, tx.Bounds.Y, w, tx.Bounds.Width})
	}
	return out
}

// titleCenterY 是行的标题按钮的纵向中心（内容在行里垂直居中，所以相邻两行的中心距 = 两行行高之和的一半）。
func titleCenterY(r *rig, title string) float32 {
	r.t.Helper()
	n := r.mustNode(gfx.RoleButton, title)
	return n.Bounds.Y + n.Bounds.Height/2
}

// rowPitches 返回网页→文档、文档→笔记两对相邻行的标题中心距。
func rowPitches(r *rig) (webToDoc, docToNote float32) {
	r.t.Helper()
	y1, y2, y3 := titleCenterY(r, "网页条目"), titleCenterY(r, "文档条目"), titleCenterY(r, "笔记条目")
	return y2 - y1, y3 - y2
}

func TestTableTypeCellWrapsEnglishWebPageAtTheSpace(t *testing.T) {
	langAtStart(t, locales.En)
	r := typeCellRig(t)

	web, page := linesOf(r, "Web"), linesOf(r, "page")
	if len(web) != 1 || len(page) != 1 {
		t.Fatalf("「Web page」应折成「Web」「page」两行，实际 Web %d 处、page %d 处（整个标签仍是一行？）", len(web), len(page))
	}
	if dy := page[0].Y - web[0].Y; math.Abs(float64(dy)-16) > 0.01 {
		t.Errorf("两行相距 %.2f，应是 text-xs 的行高 16", dy)
	}
	// text-align: center：两行的中心在同一条竖线上（渲染层读数 1051.00 与 1051.00；字偶距的差异 ≤ 1px）。
	cw, cp := web[0].X+web[0].W/2, page[0].X+page[0].W/2
	if math.Abs(float64(cw-cp)) > 1 {
		t.Errorf("两行没有居中对齐：中心 %.2f 与 %.2f", cw, cp)
	}
	// 行高：网页行因两行文字比别的行高 8px（两行 32 加上下内边距，对一行 16），相邻行中心距比不折行的一对多 4px。
	webToDoc, docToNote := rowPitches(r)
	if d := webToDoc - docToNote; math.Abs(float64(d)-4) > 0.01 {
		t.Errorf("网页行应比文档行高 8px（相邻行中心距多 4px），实际两对中心距 %.2f 与 %.2f（差 %.2f）", webToDoc, docToNote, d)
	}
	// 短标签不折行，并且在格子里居中：与折行的两行同在内容盒的中线上（text-align: center；Grow(1) 让文字盒撑满内容宽）。
	note := linesOf(r, "Note")
	if len(note) != 1 {
		t.Fatalf("「Note」应是一行，实际 %d 处", len(note))
	}
	if cn, mid := note[0].X+note[0].W/2, (cw+cp)/2; math.Abs(float64(cn-mid)) > 1 {
		t.Errorf("「Note」没有在格子里居中：中心 %.2f，折行两行的中心 %.2f", cn, mid)
	}
	if note[0].Box > note[0].W+0.5 {
		t.Errorf("「Note」的文字盒宽 %.2f 比文字宽 %.2f 多：放得下的标签不该撑满格子（走单行路径，盒子取文字的自然宽）", note[0].Box, note[0].W)
	}
}

func TestTableTypeCellKeepsAnUnbreakableWordWhole(t *testing.T) {
	langAtStart(t, locales.En)
	r := typeCellRig(t)

	doc := linesOf(r, "Document")
	if len(doc) != 1 {
		var parts []string
		for _, tx := range r.session.Snapshot().Texts {
			if tx.Value != "" && tx.Value != "Document" && strings.HasPrefix("Document", tx.Value) {
				parts = append(parts, tx.Value)
			}
		}
		t.Fatalf("「Document」是一个词，不应在词内折行或裁掉：整词 %d 处，前缀片段 %q", len(doc), parts)
	}
	// 渲染层读数：整词 60.11px 宽，超出 52px 的内容盒、从内容盒左缘起向右溢出（不是居中溢出两侧）。
	if w := doc[0].W; w < 59 || w > 61.5 {
		t.Errorf("「Document」的宽度 %.2f，应是整词宽度（渲染层 60.11）", w)
	}
	web := linesOf(r, "Web")
	if len(web) != 1 {
		t.Fatalf("找不到「Web」行（%d 处）", len(web))
	}
	// 内容盒左缘 = 同一列里居中的文字的中心减去内容宽的一半；整词溢出时左缘就是文字的起点（渲染层 1025）。
	cellCenter := web[0].X + web[0].W/2
	if left := cellCenter - 52.0/2; math.Abs(float64(doc[0].X-left)) > 1 {
		t.Errorf("「Document」应从内容盒左缘 %.2f 起排，实际 %.2f", left, doc[0].X)
	}
}

func TestTableTypeCellDoesNotWrapChineseLabels(t *testing.T) {
	langAtStart(t, locales.Zh)
	r := typeCellRig(t)
	for _, label := range []string{"网页", "文档", "笔记"} {
		lines := linesOf(r, label)
		if len(lines) != 1 {
			t.Errorf("中文类型标签「%s」应是一行，实际 %d 处", label, len(lines))
			continue
		}
		// 放得下的标签走原来的单行路径：文字盒取文字的自然宽、由格子居中，不撑满格子（宽盒子会让字形边缘的 LCD 渗色不再被裁，画面与已对照过的不同）。
		if lines[0].Box > lines[0].W+0.5 {
			t.Errorf("「%s」的文字盒宽 %.2f 比文字宽 %.2f 多：放得下的标签不该撑满格子", label, lines[0].Box, lines[0].W)
		}
	}
	webToDoc, docToNote := rowPitches(r)
	if math.Abs(float64(webToDoc-docToNote)) > 0.01 {
		t.Errorf("中文下三行等高，相邻行中心距应相同，实际 %.2f 与 %.2f", webToDoc, docToNote)
	}
}

// 行高随语言变：运行中从中文切到英文，类型格折行让网页行变高，窗口化行列的行高记录要重量（tableEpoch 带界面语言）；
// 判据同 language_live_test.go：切换后的画面必须与直接在英文下新建的逐行相同（文字加位置），并且网页行确实长高了。
func TestTableRowHeightsFollowTheLanguageAfterALiveSwitch(t *testing.T) {
	langAtStart(t, locales.Zh)
	live := typeCellRig(t)

	locales.SetLang(locales.En)
	live.frames(4)
	webToDoc, docToNote := rowPitches(live)
	if d := webToDoc - docToNote; math.Abs(float64(d)-4) > 0.01 {
		t.Errorf("中文切到英文后网页行应长高 8px（中心距多 4px），实际 %.2f 与 %.2f（差 %.2f）：行高记录没有随语言重量", webToDoc, docToNote, d)
	}
	sameTexts(t, "中文切到英文后的表格，与直接在英文下新建的表格对拍", langScreenTexts(live), langScreenTexts(typeCellRig(t)))

	locales.SetLang(locales.Zh)
	live.frames(4)
	webToDoc, docToNote = rowPitches(live)
	if math.Abs(float64(webToDoc-docToNote)) > 0.01 {
		t.Errorf("换回中文后三行应等高，实际中心距 %.2f 与 %.2f", webToDoc, docToNote)
	}
	sameTexts(t, "换回中文，与直接在中文下新建的表格对拍", langScreenTexts(live), langScreenTexts(typeCellRig(t)))
}

// 不可断的词在每个字号档下都整词不拆：文字盒的最小宽度按当前字号档量（Look.Rem），三档的根字号 14、16、18 都不能把「Document」拆开。
// 这里只测不变量（单行、整词），不对非中档字号档下的坐标下结论——英文变体只有中档的渲染层读数。
func TestTableTypeCellKeepsAnUnbreakableWordWholeAtEveryFontTier(t *testing.T) {
	langAtStart(t, locales.En)
	for _, root := range []float32{14, 16, 18} {
		r := wsRig(t, func(r *rig) {
			r.svc.SeedEntries(typeCellEntries())
			r.look.RootPx = root
		})
		r.toTable()
		r.frames(4)
		if got := len(linesOf(r, "Document")); got != 1 {
			t.Errorf("根字号 %.0f：「Document」应是一整行，实际 %d 处", root, got)
		}
	}
}

// 行高随界面语言变（台账 B-051 的行缓存键）：一页 100 行的默认夹具里每 8 行有一条网页，英文里它们折成两行、比别的行高 8px。
// 窗口只声明视口附近的行，视口以上的行靠行高记录算位置；tableEpoch 里没有界面语言的话，切语言之后那些行仍按中文的高度算，
// 视口里的行整体错位。3 行的小表测不出来（所有行都在窗口里、每帧按实际排版算），所以这里用整页对拍：
// 参照是切换之后直接以那个语言打开的新会话（整页声明，没有任何历史可以带着旧的行高）。不并行：改进程语言。
func TestTableMeasuresEveryRowAgainWhenTheLanguageChanges(t *testing.T) {
	langAtStart(t, locales.Zh)
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	win.wheel(t, 1800)

	locales.SetLang(locales.En)
	win.frames(6)
	ref := openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true})
	ref.wheel(t, 1800)
	tablePair{win: win, ref: ref}.same(t, "切到英文之后")

	locales.SetLang(locales.Zh)
	win.frames(6)
	ref = openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true})
	ref.wheel(t, 1800)
	tablePair{win: win, ref: ref}.same(t, "换回中文之后")
}

// 表格比最小宽度宽时各列按比例放大：1920 宽的窗口里类型列的内容宽是 104px（1280 宽时是 52px），「Web page」「Document」都放得下，
// 一行、居中，与短标签同在格子的中线上。放得下与否的估算（列宽减两侧内边距）是保守的：估算放不下的标签走宽盒子，
// 盒子撑满格子（Grow(1)）文字才居中——没有它，实际放得下的标签会靠格子左边排。
func TestTableTypeCellCentersLabelsThatOnlyFitInAWideTable(t *testing.T) {
	langAtStart(t, locales.En)
	r := typeCellRig(t)
	if err := r.session.Resize(1920, 1080); err != nil {
		t.Fatalf("调整窗口大小失败：%v", err)
	}
	r.frames(6)
	note, web, doc := linesOf(r, "Note"), linesOf(r, "Web page"), linesOf(r, "Document")
	if len(note) != 1 || len(web) != 1 || len(doc) != 1 {
		t.Fatalf("1920 宽时「Note」「Web page」「Document」都应是一行：%d、%d、%d 处", len(note), len(web), len(doc))
	}
	mid := func(l paintedLine) float32 { return l.X + l.W/2 }
	for _, c := range []struct {
		name string
		line paintedLine
	}{{"Web page", web[0]}, {"Document", doc[0]}} {
		if d := mid(c.line) - mid(note[0]); math.Abs(float64(d)) > 1 {
			t.Errorf("「%s」的中心 %.2f 与「Note」的 %.2f 不在同一条竖线上（差 %.2f）：放得下的标签应在格子里居中", c.name, mid(c.line), mid(note[0]), d)
		}
	}
}