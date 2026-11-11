package main

// 对应 packages/db/src/{collection,tag,knowledge}.ts 的只读查询，及 knowledge.ipc.ts 里的
// collection:list / tag:list / knowledge:counts 三个频道。输出形状与 TS 一致（字段名、null、数字类型），
// 由 library_test.go 对着 TS 生成的 golden.json 校验。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/sqlite"
)

// writeMu 串行化写操作：MyGo 事务不能穿插读取，写入采用先读后写，需保证读到写之间无他人插入。
type Library struct {
	db         *sqlite.DB
	writeMu    sync.Mutex
	configDir  string // 对应 userData/config，放 ai-models.json
	sem        semanticState
	paths      Paths
	images     imageState
	openDialog openDialogFn // 选择图片的对话框；测试里替换
	relaunch   func()       // 重启应用；测试里替换
	jobs       *BackgroundJobRuntime
	imports    *importQueue
	crawl      *crawlService // 文档批次调度（crawl_service.go）
	// 导入管线的可替换环节：url 类抽取（采集一族接入点）、抽取产物的清理、热门评论采集、文件选择、测试用整体替换
	extractURL       func(ctx context.Context, task map[string]any, onStage func(string)) (*extractedContent, error)
	disposeExtracted func(*extractedContent)
	webRenderer      webRenderer  // 渲染进程（渲染切片接入前为空）
	webFetcher       *safeFetcher // 网页采集的出站抓取；空则用 defaultSafeFetcher（测试注入）
	captureComments  func(ctx context.Context, task map[string]any, itemID string) error
	pickFiles        func(multiple bool, filters []dialogFilter) ([]string, error)
	extractOverride  importExtractFn
	pickDirectory    func(title string) (string, error)
	saveFileDialog   func(title, defaultPath string, filters []dialogFilter) (string, error)
	// 主题阅读一族迁移前的接入点：取消这些条目的主题阅读任务；返回仍被主题阅读快照租用的资产文件名
	cancelThemedReading     func(ids []string)
	leasedSnapshotAssets    func() []string
	pushMu                  sync.RWMutex
	push                    func(channel string, payload any) // 向主窗口推送事件；窗口创建后由 main 设置
	sc                      shortcutState                     // 快捷键配置（shortcuts.go）
	cfg                     configState                       // 配置迁移的 read→apply 状态（config_transfer.go）
	appData                 string                            // %APPDATA%（getPath(\"appData\")）；空则读环境变量
	hasActiveThemedReading  func() bool
	retireIdleThemedReading func() // 主题阅读一族迁移前的接入点：恢复前让空闲的主题阅读 worker 退场
	dbPath                  string // 在线数据库文件路径（备份 VACUUM INTO 的来源）
	backup                  backupState
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func (l *Library) collectionList(ctx context.Context) (any, error) {
	rows, err := l.db.Query(ctx, `SELECT c.id, c.name, c.icon, c.sort_order, c.created_at, c.updated_at, (
		 SELECT COUNT(*) FROM knowledge_items i
		 WHERE i.collection_id = c.id AND i.deleted_at IS NULL
	   ) AS item_count
	   FROM collections c ORDER BY c.sort_order, c.created_at`)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, map[string]any{
			"id": r[0], "name": r[1], "icon": r[2], "sortOrder": r[3],
			"itemCount": r[6], "createdAt": r[4], "updatedAt": r[5],
		})
	}
	return out, nil
}

func (l *Library) tagList(ctx context.Context) (any, error) {
	rows, err := l.db.Query(ctx, `SELECT t.id, t.name, t.color_key, t.created_at, t.updated_at, (
		 SELECT COUNT(*) FROM knowledge_item_tags kit
		 JOIN knowledge_items i ON i.id = kit.item_id
		 WHERE kit.tag_id = t.id AND i.deleted_at IS NULL
	   ) AS item_count
	   FROM tags t ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, map[string]any{
			"id": r[0], "name": r[1], "colorKey": r[2], "itemCount": r[5],
			"createdAt": r[3], "updatedAt": r[4],
		})
	}
	return out, nil
}

// FacetCountsQuery 对应 KnowledgeFacetCountsQuery。
type FacetCountsQuery struct {
	Scope           string `json:"scope"`
	CollectionID    string `json:"collectionId"`
	TagID           string `json:"tagId"`
	Platform        string `json:"platform"`
	Search          string `json:"search"`
	SearchMode      string `json:"searchMode"`
	IncludeArchived bool   `json:"includeArchived"`
}

type facetSQL struct {
	join, where string
	params      []any
}

// buildFacetCountSql 对应 knowledge.ts 的同名函数；omit 为不施加的分面（"collection"|"tag"|"platform"|""）。
func buildFacetCountSql(q FacetCountsQuery, omit string) facetSQL {
	var cond []string
	var params []any
	switch q.Scope {
	case "uncategorized":
		cond = append(cond, "i.deleted_at IS NULL", "i.collection_id IS NULL", "i.status != 'archived'")
	case "favorites":
		cond = append(cond, "i.deleted_at IS NULL", "i.is_favorite = 1")
	case "archived":
		cond = append(cond, "i.deleted_at IS NULL", "i.status = 'archived'")
	case "trash":
		cond = append(cond, "i.deleted_at IS NOT NULL")
	default: // "all" 与未知值
		cond = append(cond, "i.deleted_at IS NULL")
		if !q.IncludeArchived {
			cond = append(cond, "i.status != 'archived'")
		}
	}
	if omit != "collection" && q.CollectionID != "" {
		cond = append(cond, "i.collection_id = ?")
		params = append(params, q.CollectionID)
	}
	if omit != "tag" && q.TagID != "" {
		cond = append(cond, "EXISTS (SELECT 1 FROM knowledge_item_tags kit WHERE kit.item_id = i.id AND kit.tag_id = ?)")
		params = append(params, q.TagID)
	}
	if omit != "platform" && q.Platform != "" {
		cond = append(cond, "EXISTS (SELECT 1 FROM source_records sr WHERE sr.item_id = i.id AND sr.platform = ?)")
		params = append(params, q.Platform)
	}

	term := strings.TrimFunc(q.Search, jsSpace)
	var match string
	var hasMatch bool
	if term != "" {
		mode := FtsMatchMode(q.SearchMode)
		if mode != FtsRecall {
			mode = FtsPhrase
		}
		match, hasMatch = BuildFtsMatchQuery(term, mode)
		if !hasMatch {
			cond = append(cond, "0")
		}
	}
	join := ""
	if hasMatch {
		join = `JOIN (
        SELECT item_id, bm25(knowledge_fts,0,10,1,5) AS fts_rank FROM knowledge_fts
        WHERE knowledge_fts MATCH ?
      ) f ON f.item_id = i.id`
		params = append([]any{match}, params...)
	}
	return facetSQL{join: join, where: "WHERE " + strings.Join(cond, " AND "), params: params}
}

func (l *Library) knowledgeCounts(ctx context.Context, q FacetCountsQuery) (any, error) {
	scope, err := l.db.Query(ctx, `SELECT
		 SUM(CASE WHEN deleted_at IS NULL AND collection_id IS NULL AND status != 'archived' THEN 1 ELSE 0 END),
		 SUM(CASE WHEN deleted_at IS NULL AND status != 'archived' THEN 1 ELSE 0 END),
		 SUM(CASE WHEN deleted_at IS NULL AND is_favorite = 1 THEN 1 ELSE 0 END),
		 SUM(CASE WHEN deleted_at IS NULL AND status = 'archived' THEN 1 ELSE 0 END),
		 SUM(CASE WHEN deleted_at IS NOT NULL THEN 1 ELSE 0 END)
	   FROM knowledge_items`)
	if err != nil {
		return nil, err
	}
	group := func(sql string, p facetSQL) (map[string]int64, error) {
		rows, err := l.db.Query(ctx, sql, p.params...)
		if err != nil {
			return nil, err
		}
		m := map[string]int64{}
		for _, r := range rows.Values {
			if k, ok := r[0].(string); ok {
				m[k] = asInt(r[1])
			}
		}
		return m, nil
	}
	cs := buildFacetCountSql(q, "collection")
	byCollection, err := group(`SELECT i.collection_id, COUNT(*) AS count FROM knowledge_items i
	   `+cs.join+` `+cs.where+`
	   AND collection_id IS NOT NULL
	   GROUP BY collection_id`, cs)
	if err != nil {
		return nil, err
	}
	ts := buildFacetCountSql(q, "tag")
	byTag, err := group(`SELECT facet_tag.tag_id AS tag_id, COUNT(*) AS count
	   FROM knowledge_items i
	   JOIN knowledge_item_tags facet_tag ON facet_tag.item_id = i.id
	   `+ts.join+` `+ts.where+`
	   GROUP BY facet_tag.tag_id`, ts)
	if err != nil {
		return nil, err
	}
	ps := buildFacetCountSql(q, "platform")
	byPlatform, err := group(`SELECT s.platform AS platform, COUNT(DISTINCT s.item_id) AS count
	   FROM source_records s
	   JOIN knowledge_items i ON i.id = s.item_id
	   `+ps.join+` `+ps.where+`
	   AND s.platform IS NOT NULL
	   GROUP BY s.platform`, ps)
	if err != nil {
		return nil, err
	}
	s := scope.Values[0]
	return map[string]any{
		"uncategorized": asInt(s[0]), "all": asInt(s[1]), "favorites": asInt(s[2]),
		"archived": asInt(s[3]), "trash": asInt(s[4]),
		"byCollection": byCollection, "byTag": byTag, "byPlatform": byPlatform,
	}, nil
}

// register 把本文件的频道登记到 Bridge。
func (l *Library) register(b *Bridge) {
	b.handlers["collection:list"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return l.collectionList(ctx)
	}
	b.handlers["tag:list"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return l.tagList(ctx)
	}
	b.handlers["knowledge:counts"] = func(ctx context.Context, args []json.RawMessage) (any, error) {
		q := FacetCountsQuery{Scope: "all"} // 与 TS 一致：未传查询时用 { scope: "all" }
		if len(args) > 0 && string(args[0]) != "null" {
			if err := json.Unmarshal(args[0], &q); err != nil {
				return nil, err
			}
		}
		return l.knowledgeCounts(ctx, q)
	}
}
