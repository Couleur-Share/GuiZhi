package main

// 大库夹具生成器（默认跳过），供 verify/run-bigdb.ps1 测大库启动、列表、搜索、备份、恢复耗时。
//   GUIZHI_MAKE_BIG_DB=<输出 knowledge.db 路径>  GUIZHI_BIG_ITEMS=30000  GUIZHI_BIG_CHARS=3000
// 数据形态与生产写入一致：knowledge_items + knowledge_fts（SegmentTextForFts 分词）+ knowledge_fts_map。
// 内容是确定性伪随机的中英混排（固定种子，可复现），不含任何真实用户数据。

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMakeBigDatabaseFixture(t *testing.T) {
	out := os.Getenv("GUIZHI_MAKE_BIG_DB")
	if out == "" {
		t.Skip("需要 GUIZHI_MAKE_BIG_DB")
	}
	n, chars := 30000, 3000
	if v, err := strconv.Atoi(os.Getenv("GUIZHI_BIG_ITEMS")); err == nil && v > 0 {
		n = v
	}
	if v, err := strconv.Atoi(os.Getenv("GUIZHI_BIG_CHARS")); err == nil && v > 0 {
		chars = v
	}
	_ = os.Remove(out)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	l, err := initDatabaseFile(ctx, out, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	db, err := openReadWrite(out)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rng := rand.New(rand.NewSource(20261112))
	hanzi := []rune("的一是不了人我在有他这中大来上国个到说们为子和你地出道也时年得就那要下以生会自着去之过家学对可她里后小么心多天而能好都然没日于起还发成事只作当想看文无开手十用主行方又如前所本见经头面公同三已老从动两长知民样现分将外但身些与高意进把法此实回二理美点月明其种声全工已都情部定关")
	words := []string{"SQLite", "FTS5", "WebView2", "Electron", "Go", "MyGo", "backup", "restore", "index", "query", "知识库", "采集", "检索", "备份", "迁移", "编译", "归知", "阅读", "问答", "全文"}
	const rareEvery = 5000 // 每 5000 条出现一次的稀有词，用于测「低命中」搜索
	para := func(i int) string {
		var b strings.Builder
		for b.Len() < chars*3 { // 每个汉字 3 字节
			if rng.Intn(9) == 0 {
				b.WriteString(" " + words[rng.Intn(len(words))] + " ")
			} else {
				b.WriteRune(hanzi[rng.Intn(len(hanzi))])
			}
			if rng.Intn(40) == 0 {
				b.WriteString("。\n")
			}
		}
		if i%rareEvery == 0 {
			b.WriteString("\n稀有词ZXQVBIG" + strconv.Itoa(i))
		}
		return b.String()
	}

	const batch = 500
	for start := 0; start < n; start += batch {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := start; i < start+batch && i < n; i++ {
			id := fmt.Sprintf("big-%07d", i)
			title := fmt.Sprintf("大库样本 %d：%s", i, words[i%len(words)])
			content := para(i)
			ts := int64(1_700_000_000_000 + int64(i)*60_000)
			if _, err := tx.Exec(`INSERT INTO knowledge_items (id, title, content, item_type, status, is_favorite, is_pinned, review_status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
				id, title, content, "note", "active", i%97 == 0, 0, "clear", ts, ts); err != nil {
				t.Fatal(err)
			}
			res, err := tx.Exec(`INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?,?,?,?)`, id, SegmentTextForFts(title), SegmentTextForFts(content), "")
			if err != nil {
				t.Fatal(err)
			}
			rowid, _ := res.LastInsertId()
			if _, err := tx.Exec(`INSERT OR REPLACE INTO knowledge_fts_map (item_id, fts_rowid) VALUES (?,?)`, id, rowid); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := os.Stat(out)
	t.Logf("已生成 %d 条、%.0f MB：%s", n, float64(st.Size())/1048576, out)
}
