package main

// 公众号导入链路的手写测试：抽取 → 保存（版本/快照资产/基线）→ 补采附加版本 → 资产租约与孤儿清理。
// 真实 SQLite（基准库副本）+ 本地回环夹具服务，不访问真实网络。

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func wxPNG(tag string) string {
	return base64.StdEncoding.EncodeToString(append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, []byte("IHDR"+tag)...))
}

func wxCase(url, html string, images map[string]string) wxGoldenCase {
	c := wxGoldenCase{Name: "inline", URL: url}
	c.Pages = map[string]struct {
		HTML     string  `json:"html"`
		FinalURL *string `json:"finalUrl"`
	}{url: {HTML: html}}
	c.Images = map[string]struct {
		B64    *string `json:"b64"`
		Status *int    `json:"status"`
	}{}
	for u, b := range images {
		b := b
		c.Images[u] = struct {
			B64    *string `json:"b64"`
			Status *int    `json:"status"`
		}{B64: &b}
	}
	return c
}

func wxArticle(text string, imgs ...string) string {
	body := "<p>" + text + "</p>"
	for _, i := range imgs {
		body += `<img src="` + i + `">`
	}
	return `<html><head><meta property="article:published_time" content="2024-05-01T12:00:00Z"></head><body><h1 id="activity-name">标题</h1><div id="js_content">` + body + `</div></body></html>`
}

func countRows(t *testing.T, l *Library, sql string, args ...any) int {
	t.Helper()
	rows, err := l.db.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	return int(asInt(rows.Values[0][0]))
}

func TestWechatImportSaveRefreshAndDispose(t *testing.T) {
	e := newWebSnapEnv(t)
	l := e.l
	ctx := context.Background()
	const page = "http://mp.weixin.qq.com/s/rt"
	i1, i2 := "http://mmbiz.qpic.cn/1.png", "http://mmbiz.qpic.cn/2.png"
	imgs := map[string]string{i1: wxPNG("one"), i2: wxPNG("two")}
	use := func(html string) {
		c := wxCase(page, html, imgs)
		l.webFetcher = wxFixture(t, c)
	}
	task := map[string]any{"id": "t1", "sourceInput": page, "sourceKind": "url"}
	norm := "http://mp.weixin.qq.com/s/rt"

	// 1. 首次导入：抽取带快照与租约，保存后落版本、资产登记与基线
	use(wxArticle("第一版正文", i1, i2))
	ex, err := l.importExtractURL(ctx, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ex.WebCapture == nil || ex.WebCapture.Snapshot == nil || ex.ItemType != "webpage" || ex.Title != "标题" {
		t.Fatalf("抽取结果不符：%+v", ex)
	}
	names := ex.WebCapture.SnapshotAssets
	if len(names) != 2 {
		t.Fatalf("应保存 2 张图：%v", names)
	}
	assetLeaseMu.Lock()
	leased := assetLeases[names[0]]
	assetLeaseMu.Unlock()
	if leased == 0 {
		t.Fatal("抽取后资产应处于租约中")
	}
	itemID, err := l.importSaveItem(ctx, importSaveParams{extracted: ex, task: task, normalizedURI: &norm, contentHash: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, l, "SELECT COUNT(*) FROM web_snapshot_assets WHERE version_id IN (SELECT id FROM web_source_versions WHERE item_id=?)", itemID); n != 2 {
		t.Fatalf("快照资产登记 %d 条", n)
	}
	if n := countRows(t, l, "SELECT COUNT(*) FROM web_source_baselines WHERE item_id=?", itemID); n != 1 {
		t.Fatal("应有基线")
	}
	l.disposeExtractedContent(ex)
	assetLeaseMu.Lock()
	leased = assetLeases[names[0]]
	assetLeaseMu.Unlock()
	if leased != 0 {
		t.Fatal("释放后租约应清零")
	}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(l.paths.ImagesDir(), n)); err != nil {
			t.Fatalf("已被条目引用的资产不应被清理：%v", err)
		}
	}

	// 2. 补采同一内容：不新增版本
	refresh := map[string]any{"id": "t2", "sourceInput": page, "sourceKind": "url", "refreshOfItemId": itemID}
	use(wxArticle("第一版正文", i1, i2))
	ex2, err := l.importExtractURL(ctx, refresh, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.importSaveItem(ctx, importSaveParams{extracted: ex2, task: refresh, normalizedURI: &norm, contentHash: "h1"}); err != nil {
		t.Fatal(err)
	}
	l.disposeExtractedContent(ex2)
	if n := countRows(t, l, "SELECT COUNT(*) FROM web_source_versions WHERE item_id=?", itemID); n != 1 {
		t.Fatalf("内容未变时补采不应新增版本：%d", n)
	}

	// 3. 补采到变化的内容：追加新版本，正文与基线不动
	use(wxArticle("第二版正文", i1))
	ex3, err := l.importExtractURL(ctx, refresh, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.importSaveItem(ctx, importSaveParams{extracted: ex3, task: refresh, normalizedURI: &norm, contentHash: "h2"}); err != nil {
		t.Fatal(err)
	}
	l.disposeExtractedContent(ex3)
	if n := countRows(t, l, "SELECT COUNT(*) FROM web_source_versions WHERE item_id=?", itemID); n != 2 {
		t.Fatalf("内容变化后应追加版本：%d", n)
	}
	rows, _ := l.db.Query(ctx, "SELECT content FROM knowledge_items WHERE id=?", itemID)
	if rows.Values[0][0] != ex.Content {
		t.Fatal("补采不应覆盖正文")
	}

	// 4. 补采来源与原条目不一致 → 拒绝
	other := map[string]any{"id": "t3", "sourceInput": "http://mp.weixin.qq.com/s/other", "sourceKind": "url", "refreshOfItemId": itemID}
	l.webFetcher = wxFixture(t, wxCase("http://mp.weixin.qq.com/s/other", wxArticle("别的文章", i1), imgs))
	ex4, err := l.importExtractURL(ctx, other, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.importSaveItem(ctx, importSaveParams{extracted: ex4, task: other, normalizedURI: &norm, contentHash: "h3"}); err == nil || err.Error() != "补采来源与原条目不一致" {
		t.Fatalf("应拒绝来源不一致的补采：%v", err)
	}
	l.disposeExtractedContent(ex4)

	// 5. 没保存就丢弃：新落盘、未被引用的资产被清理
	fresh := map[string]string{"http://mmbiz.qpic.cn/f.png": wxPNG("fresh-only")}
	l.webFetcher = wxFixture(t, wxCase(page, wxArticle("临时", "http://mmbiz.qpic.cn/f.png"), fresh))
	ex5, err := l.importExtractURL(ctx, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	orphan := ex5.WebCapture.SnapshotAssets[0]
	l.disposeExtractedContent(ex5)
	if _, err := os.Stat(filepath.Join(l.paths.ImagesDir(), orphan)); !os.IsNotExist(err) {
		t.Fatalf("未引用的资产应被清理：%v", err)
	}
}

func TestWechatCheckWithSnapshotKeepsBody(t *testing.T) {
	e := newWebSnapEnv(t)
	l := e.l
	ctx := context.Background()
	const page = "http://mp.weixin.qq.com/s/chk"
	l.webFetcher = wxFixture(t, wxCase(page, wxArticle("正文甲"), nil))
	r1, err := l.captureWechat(ctx, page, "c1")
	if err != nil {
		t.Fatal(err)
	}
	defer r1.release()
	l.writeMu.Lock()
	_, err = l.db.Transaction(ctx, webSourceInitStmts(e.itemID, "公众号文章", "正文内容", r1))
	l.writeMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// 同内容再检查 → unchanged
	got, err := l.webSourceCheck(ctx, e.itemID, r1)
	if err != nil || got != "unchanged" {
		t.Fatalf("同内容应 unchanged：%q %v", got, err)
	}
	// 内容变化 → 带快照的采集只留待采用版本，不改正文
	l.webFetcher = wxFixture(t, wxCase(page, wxArticle("正文乙"), nil))
	r2, err := l.captureWechat(ctx, page, "c2")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.release()
	got, err = l.webSourceCheck(ctx, e.itemID, r2)
	if err != nil || got != "pending-version" {
		t.Fatalf("带快照的变化应 pending-version：%q %v", got, err)
	}
	rows, _ := l.db.Query(ctx, "SELECT content FROM knowledge_items WHERE id=?", e.itemID)
	if rows.Values[0][0] != "正文内容" {
		t.Fatal("check 不应改写正文")
	}
}
