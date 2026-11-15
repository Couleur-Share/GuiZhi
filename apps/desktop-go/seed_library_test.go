package main

// 合成资料库生成器：给「同一份库副本」的场景用（空闲内存基线、原生界面性能基准、截图）。
// 它不是回归测试：只有设置了 GUIZHI_SEED_DB 才运行，平时被跳过。
//
//	$env:GUIZHI_SEED_DB = "D:\tmp\seed\data\knowledge.db"
//	$env:GUIZHI_SEED_ITEMS = "520"         # 条目数，默认 520（落库约 13 MB，与真实库同一量级；1200 条约 50 MB，作大库对照）
//	go test -count=1 -run '^TestSeedLibrary$' .
//
// 用与产品相同的初始化（initDatabaseFile）和写入处理器（knowledge:create、collection:create、tag 隐式创建）建库，
// 所以 FTS 行、计数、标签关联都与真实写入一致；数据由固定种子的伪随机数生成，两次运行结果相同。
// 不读取任何用户数据目录；生成后做一次 WAL 检查点并留下干净关闭标记，产物是单个可直接拷贝的 knowledge.db。

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func seedEnvInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v >= 0 {
		return v
	}
	return def
}

// seedArticle 生成约 n 字节的中文正文：标题、段落、强调、列表、表格、代码块、引用。不含图片，避免两套界面对本地图片的差异。
func seedArticle(rng *rand.Rand, title string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n> 合成正文，用于内存与性能基准。\n\n", title)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "## 第 %d 节：%s\n\n", i+1, []string{"数据与边界", "采集与整理", "检索与问答", "备份与迁移"}[rng.Intn(4)])
		b.WriteString("归知把**网页、平台内容、本地文件**收进来，再用合集、标签和全文检索整理。重点是**不丢数据。**后面的文字紧跟强调结束，用来检查中文强调的边界。")
		fmt.Fprintf(&b, "还有 ~~已废弃~~ 的旧方案，以及 `VACUUM INTO` 这样的行内代码和 [链接文字](https://example.com/doc/%d)。\n\n", rng.Intn(100000))
		if i%3 == 0 {
			b.WriteString("- 采集：网页正文、文档站点批量抓取\n- 整理：条目编辑、合集、标签\n- [x] 已完成的任务项\n- [ ] 未完成的任务项\n\n")
		}
		if i%4 == 1 {
			b.WriteString("| 能力 | 状态 | 说明 |\n| --- | :---: | --- |\n| 全文检索 | 可用 | FTS5 |\n| 语义检索 | 可用 | 精确扫描 |\n\n")
		}
		if i%5 == 2 {
			b.WriteString("```go\nfunc (l *Library) importListPage(ctx context.Context) error {\n\treturn nil // 示例代码块\n}\n```\n\n")
		}
		b.WriteString("本地优先意味着数据首先属于用户自己的机器，网络只是可选的搬运工具，而不是依赖。备份、导出与迁移都围绕这条原则展开，所以数据库采用 WAL 模式，备份用 VACUUM INTO，换库之前先做检查点。\n\n")
	}
	return b.String()
}

// seedForum 生成含 floors 个回复的论坛笔记正文（与采集端同一格式）。
func seedForum(floors int) string {
	var b strings.Builder
	b.WriteString("## 讨论总结\n\n合成总结。\n\n## 正文\n\n主楼内容。\n\n")
	fmt.Fprintf(&b, "## 讨论（%d 条）\n\n", floors)
	for i := 1; i <= floors; i++ {
		var rt *forumReplyTo
		if i > 3 && i%3 == 0 {
			f := i - 2
			rt = &forumReplyTo{Author: fmt.Sprintf("用户%d", f), Floor: &f, Snippet: "被回复楼的摘要文字，给人扫读用。", HasFloor: true}
		}
		content := fmt.Sprintf("第 %d 楼的回复：我觉得**原生界面**值得试，但输入法和读屏要先验证。这里再写一点文字让楼层有高度。", i)
		if i%7 == 0 {
			content += "\n\n```\nfmt.Println(\"代码也会出现在回复里\")\n```"
		}
		b.WriteString(formatForumReplyBlock(i, fmt.Sprintf("用户%d", i), content, rt))
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestSeedLibrary(t *testing.T) {
	out := os.Getenv("GUIZHI_SEED_DB")
	if out == "" {
		t.Skip("设置 GUIZHI_SEED_DB=<knowledge.db 路径> 以生成合成资料库")
	}
	ctx := context.Background()
	nItems := seedEnvInt("GUIZHI_SEED_ITEMS", 520)
	nImports := seedEnvInt("GUIZHI_SEED_IMPORTS", 300)
	nCollections := seedEnvInt("GUIZHI_SEED_COLLECTIONS", 40)
	nTags := seedEnvInt("GUIZHI_SEED_TAGS", 120)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{out, out + "-wal", out + "-shm", out + ".clean"} {
		_ = os.Remove(p) // 每次从空库重建，保证可重复
	}
	lease, err := initDatabaseFile(ctx, out, dbInitOptions{BackupsDir: filepath.Join(filepath.Dir(filepath.Dir(out)), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(ctx, out, sqlite.OpenOptions{})
	if err != nil {
		lease.release()
		t.Fatal(err)
	}
	l := &Library{db: db, dbPath: out}
	b := &Bridge{handlers: map[string]handler{}}
	l.registerWrite(b)
	l.registerCatalogWrite(b)
	call := func(channel string, arg any) any {
		t.Helper()
		raw, err := json.Marshal(arg)
		if err != nil {
			t.Fatal(err)
		}
		v, err := b.handlers[channel](ctx, []json.RawMessage{raw})
		if err != nil {
			t.Fatalf("%s: %v", channel, err)
		}
		return v
	}
	idOf := func(v any) string {
		raw, _ := json.Marshal(v)
		var m struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &m)
		return m.ID
	}

	rng := rand.New(rand.NewSource(20261115))
	started := time.Now()

	collectionIDs := make([]string, 0, nCollections)
	for i := 0; i < nCollections; i++ {
		collectionIDs = append(collectionIDs, idOf(call("collection:create", map[string]any{"name": fmt.Sprintf("合集 %02d", i+1)})))
	}
	tagPool := make([]string, nTags)
	for i := range tagPool {
		tagPool[i] = fmt.Sprintf("标签%03d", i+1)
	}

	// 正文大小分布（均值约 6 KB）：70% 1–3 KB，20% 3–12 KB，8% 12–40 KB，2% 40–100 KB。
	// 落库后体积约为正文的 6 倍（正文、FTS 内容与索引、版本快照），实测 520 条 13.4 MB、1200 条 49.7 MB。
	pickSize := func() int {
		switch r := rng.Intn(100); {
		case r < 70:
			return 1024 + rng.Intn(2048)
		case r < 90:
			return 3072 + rng.Intn(9*1024)
		case r < 98:
			return 12*1024 + rng.Intn(28*1024)
		default:
			return 40*1024 + rng.Intn(60*1024)
		}
	}
	types := []string{"webpage", "webpage", "webpage", "webpage", "webpage", "webpage", "note", "note", "forum", "document"}
	for i := 0; i < nItems; i++ {
		itemType := types[rng.Intn(len(types))]
		title := fmt.Sprintf("样本条目 %04d：归知的本地优先架构", i+1)
		var content string
		if itemType == "forum" {
			content = seedForum(10 + rng.Intn(140))
		} else {
			content = seedArticle(rng, title, pickSize())
		}
		in := map[string]any{"title": title, "content": content, "itemType": itemType}
		if rng.Intn(10) < 8 && len(collectionIDs) > 0 {
			in["collectionId"] = collectionIDs[rng.Intn(len(collectionIDs))]
		}
		var tags []string
		for k := rng.Intn(5); k > 0; k-- {
			tags = append(tags, tagPool[rng.Intn(len(tagPool))])
		}
		in["tagNames"] = tags
		call("knowledge:create", in)
	}

	// 导入任务：各种状态都有，供导入页与计数使用
	sites := []string{"https://zhuanlan.zhihu.com/p/", "https://www.v2ex.com/t/", "https://mp.weixin.qq.com/s/", "https://github.com/egoist/mygo/issues/"}
	statuses := []string{"completed", "completed", "completed", "failed", "pending", "duplicate"}
	now := time.Now().UnixMilli()
	for i := 0; i < nImports; i++ {
		st := statuses[i%len(statuses)]
		var errMsg any
		if st == "failed" {
			errMsg = "抓取失败：目标站点返回 403，需要登录态后重试（" + strconv.Itoa(i) + "）"
		}
		if _, err := db.Execute(ctx, `INSERT INTO import_tasks (id, source_kind, source_input, display_name, status, error, created_at, updated_at)
 VALUES (?, 'url', ?, ?, ?, ?, ?, ?)`,
			"seed-"+strconv.Itoa(i), sites[i%len(sites)]+strconv.Itoa(100000+i), fmt.Sprintf("样本导入 #%d", i+1), st, errMsg,
			now-int64(i)*37_000, now-int64(i)*37_000); err != nil {
			t.Fatal(err)
		}
	}

	db.Close()
	if err := checkpointLiveWAL(ctx, out); err != nil {
		t.Fatalf("WAL 检查点：%v", err)
	}
	writeCleanShutdownMarker(out)
	lease.release()
	st, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("已生成 %s：%d 条目、%d 合集、%d 标签、%d 导入任务，%.1f MB，用时 %s",
		out, nItems, nCollections, nTags, nImports, float64(st.Size())/1048576, time.Since(started).Round(time.Millisecond))
}
