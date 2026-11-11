package main

// 对应 packages/db/src/web-source.ts：initialize（条目创建时记录首个"远程版本"与基线）、
// check（重采后更新/保留待采用版本）、adopt（采用某个原文版本）、versions/baseline 查询。
// attach 与快照资源随微信快照切片迁移。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/egoist/mygo/plugins/sqlite"
)

func webContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// webSourceVersionJSON 与 JSON.stringify(WebSourceVersion) 逐字节一致（键序、转义）。
func webSourceVersionJSON(id, itemID string, r webCaptureResult) string {
	o := newObj()
	o.set("id", id)
	o.set("itemId", itemID)
	o.set("sourceUrl", r.FinalURL)
	o.set("title", r.Title)
	o.set("markdown", r.Markdown)
	o.set("contentHash", webContentHash(r.Markdown))
	o.set("capturedAt", float64(r.CapturedAt))
	o.set("engineVersion", r.EngineVersion)
	o.set("complete", r.Complete && !r.Truncated && r.Error == nil)
	o.set("kind", "remote")
	s, _ := jsStringify(o)
	return s
}

func webSourceInitStmts(itemID, title, content string, r webCaptureResult) []sqlite.Statement {
	versionID := newUUID()
	return []sqlite.Statement{
		{
			SQL:  "INSERT INTO web_source_versions VALUES (?,?,?,?)",
			Args: []any{versionID, itemID, webSourceVersionJSON(versionID, itemID, r), r.CapturedAt},
		},
		{
			SQL:  "INSERT INTO web_source_baselines VALUES (?,?,?,?,?,?,?) ON CONFLICT(item_id) DO UPDATE SET version_id=excluded.version_id,content_hash=excluded.content_hash,title=excluded.title,checked_at=excluded.checked_at,remote_hash=excluded.remote_hash,summary_stale=excluded.summary_stale",
			Args: []any{itemID, versionID, webContentHash(content), title, nowMs(), webContentHash(r.Markdown), 0},
		},
	}
}

// webSourceVersion 是 web_source_versions.payload 的解析形式；Raw 保留原始 JSON，IPC 原样返回。
type webSourceVersion struct {
	ID            string          `json:"id"`
	ItemID        string          `json:"itemId"`
	SourceURL     string          `json:"sourceUrl"`
	Title         string          `json:"title"`
	Markdown      string          `json:"markdown"`
	ContentHash   string          `json:"contentHash"`
	CapturedAt    int64           `json:"capturedAt"`
	EngineVersion *string         `json:"engineVersion"`
	Complete      bool            `json:"complete"`
	Kind          string          `json:"kind"`
	Snapshot      json.RawMessage `json:"snapshot"`
	Raw           string          `json:"-"`
}

// snapshotHash 对应 version.snapshot?.hash：没有快照或没有 hash 都是 undefined。
func (v webSourceVersion) snapshotHash() (string, bool) {
	if len(v.Snapshot) == 0 || string(v.Snapshot) == "null" {
		return "", false
	}
	var s struct {
		Hash *string `json:"hash"`
	}
	if json.Unmarshal(v.Snapshot, &s) != nil || s.Hash == nil {
		return "", false
	}
	return *s.Hash, true
}

func (v webSourceVersion) hasSnapshot() bool {
	return len(v.Snapshot) > 0 && string(v.Snapshot) != "null"
}

type webSourceBaseline struct {
	VersionID    string
	ContentHash  string
	Title        string
	RemoteHash   string
	SummaryStale int64
}

var reExtractorTail = regexp.MustCompile(`^([^/]*/[^-]*)-.*$`)

// extractorOf 对应 extractorOf：去掉 engineVersion 末尾的采集路由（readability/1-static → readability/1）。
func extractorOf(engineVersion *string) (string, bool) {
	if engineVersion == nil {
		return "", false
	}
	return reExtractorTail.ReplaceAllString(*engineVersion, "$1"), true
}

func (l *Library) webSourceVersions(ctx context.Context, itemID string) ([]webSourceVersion, error) {
	rows, err := l.db.Query(ctx, "SELECT payload FROM web_source_versions WHERE item_id=? ORDER BY captured_at DESC,rowid DESC", itemID)
	if err != nil {
		return nil, err
	}
	out := make([]webSourceVersion, 0, len(rows.Values))
	for _, r := range rows.Values {
		raw, _ := r[0].(string)
		var v webSourceVersion
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		v.Raw = raw
		out = append(out, v)
	}
	return out, nil
}

func (l *Library) webSourceBaseline(ctx context.Context, itemID string) (*webSourceBaseline, error) {
	rows, err := l.db.Query(ctx, "SELECT version_id,content_hash,title,remote_hash,summary_stale FROM web_source_baselines WHERE item_id=?", itemID)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	r := rows.Values[0]
	str := func(v any) string { s, _ := v.(string); return s }
	return &webSourceBaseline{VersionID: str(r[0]), ContentHash: str(r[1]), Title: str(r[2]), RemoteHash: str(r[3]), SummaryStale: asInt(r[4])}, nil
}

type webSourceItem struct {
	Title, Content string
	HasSummary     bool
}

// webSourceItemGet 对应 KnowledgeItemDB.get 在此处用到的字段；不存在或已删除时返回 nil。
func (l *Library) webSourceItemGet(ctx context.Context, itemID string) (*webSourceItem, error) {
	rows, err := l.db.Query(ctx, "SELECT title,content,summary,deleted_at FROM knowledge_items WHERE id=?", itemID)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	r := rows.Values[0]
	if r[3] != nil {
		return nil, nil
	}
	str := func(v any) string { s, _ := v.(string); return s }
	return &webSourceItem{Title: str(r[0]), Content: str(r[1]), HasSummary: str(r[2]) != ""}, nil
}

func webSourceSaveStmt(id, itemID, payload string, capturedAt int64) sqlite.Statement {
	return sqlite.Statement{SQL: "INSERT INTO web_source_versions VALUES (?,?,?,?)", Args: []any{id, itemID, payload, capturedAt}}
}

func webSourceBaselineStmt(itemID, versionID, content, title, remoteHash string, stale bool) sqlite.Statement {
	return sqlite.Statement{
		SQL:  "INSERT INTO web_source_baselines VALUES (?,?,?,?,?,?,?) ON CONFLICT(item_id) DO UPDATE SET version_id=excluded.version_id,content_hash=excluded.content_hash,title=excluded.title,checked_at=excluded.checked_at,remote_hash=excluded.remote_hash,summary_stale=excluded.summary_stale",
		Args: []any{itemID, versionID, webContentHash(content), title, nowMs(), remoteHash, b2i(stale)},
	}
}

// webSourceLocalSnapshot 对应 snapshot()：覆盖正文前把用户当前正文存成 local 版本（键序与 TS 一致）。
func webSourceLocalSnapshot(itemID, title, markdown, sourceURL string) sqlite.Statement {
	id := newUUID()
	now := nowMs()
	o := newObj()
	o.set("id", id)
	o.set("itemId", itemID)
	o.set("title", title)
	o.set("markdown", markdown)
	o.set("sourceUrl", sourceURL)
	o.set("contentHash", webContentHash(markdown))
	o.set("capturedAt", float64(now))
	o.set("engineVersion", "guizhi")
	o.set("complete", true)
	o.set("kind", "local")
	payload, _ := jsStringify(o)
	return webSourceSaveStmt(id, itemID, payload, now)
}

// webSourceInvalidateStmts 对应 invalidate：正文改写后清掉向量与 Wiki 摄入记录。
func webSourceInvalidateStmts(itemID string) []sqlite.Statement {
	return []sqlite.Statement{
		{SQL: "DELETE FROM knowledge_embeddings WHERE item_id=?", Args: []any{itemID}},
		{SQL: "DELETE FROM wiki_ingestions WHERE item_id=?", Args: []any{itemID}},
	}
}

// webSourceCheck 对应 check：重采已有网页条目，返回 updated / unchanged / pending-version。
func (l *Library) webSourceCheck(ctx context.Context, itemID string, r webCaptureResult) (string, error) {
	if !r.Complete || r.Truncated || r.Error != nil {
		return "", errors.New("不完整或失败正文不能更新已有条目")
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	item, err := l.webSourceItemGet(ctx, itemID)
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", errors.New("条目不存在或已删除")
	}
	baseline, err := l.webSourceBaseline(ctx, itemID)
	if err != nil {
		return "", err
	}
	versions, err := l.webSourceVersions(ctx, itemID)
	if err != nil {
		return "", err
	}
	versionID := newUUID()
	payload := webSourceVersionJSON(versionID, itemID, r)
	var version webSourceVersion
	if err := json.Unmarshal([]byte(payload), &version); err != nil {
		return "", err
	}
	var previous, basis *webSourceVersion
	for i := range versions {
		if previous == nil && versions[i].Kind == "remote" {
			previous = &versions[i]
		}
		if basis == nil && baseline != nil && versions[i].ID == baseline.VersionID {
			basis = &versions[i]
		}
	}
	if basis == nil {
		basis = previous
	}
	// 本次采集没有快照（hash 为 undefined）；旧版本带 hash 时视为已变化
	if previous != nil && previous.ContentHash == version.ContentHash && previous.Title == version.Title {
		if _, hasHash := previous.snapshotHash(); !hasHash {
			if _, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "UPDATE web_source_baselines SET checked_at=? WHERE item_id=?", Args: []any{nowMs(), itemID}}}); err != nil {
				return "", err
			}
			if baseline != nil && baseline.VersionID == previous.ID {
				return "unchanged", nil
			}
			return "pending-version", nil
		}
	}
	stmts := []sqlite.Statement{webSourceSaveStmt(versionID, itemID, payload, version.CapturedAt)}
	var basisEngine *string
	if basis != nil {
		basisEngine = basis.EngineVersion
	}
	be, bok := extractorOf(basisEngine)
	ve, vok := extractorOf(version.EngineVersion)
	if baseline == nil || webContentHash(item.Content) != baseline.ContentHash || be != ve || bok != vok {
		if _, err := l.db.Transaction(ctx, stmts); err != nil {
			return "", err
		}
		return "pending-version", nil
	}
	stmts = append(stmts, webSourceLocalSnapshot(itemID, item.Title, item.Content, version.SourceURL))
	title := version.Title
	if item.Title != baseline.Title {
		title = item.Title
	}
	update, found, err := l.knowledgeUpdateStmts(ctx, itemID, UpdateInput{Title: optString{Set: true, Val: title}, Content: optString{Set: true, Val: version.Markdown}})
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("条目不存在或已删除")
	}
	baselineTitle := baseline.Title
	if item.Title == baseline.Title {
		baselineTitle = title
	}
	stmts = append(stmts, update...)
	stmts = append(stmts, webSourceInvalidateStmts(itemID)...)
	stmts = append(stmts, webSourceBaselineStmt(itemID, versionID, version.Markdown, baselineTitle, version.ContentHash, item.HasSummary))
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return "", err
	}
	return "updated", nil
}

// webAdoptInput 对应 AdoptWebVersionInput。
type webAdoptInput struct {
	ItemID              string
	VersionID           string
	ExpectedContentHash string
	ExpectedTitle       string
}

// webSourceAdopt 对应 adopt：把某个完整的原文版本采用为正文，先把当前正文存成 local 版本。
func (l *Library) webSourceAdopt(ctx context.Context, in webAdoptInput) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	item, err := l.webSourceItemGet(ctx, in.ItemID)
	if err != nil {
		return err
	}
	versions, err := l.webSourceVersions(ctx, in.ItemID)
	if err != nil {
		return err
	}
	var version *webSourceVersion
	for i := range versions {
		if versions[i].ID == in.VersionID {
			version = &versions[i]
			break
		}
	}
	if item == nil || version == nil || !version.Complete {
		return errors.New("条目或完整版本不存在")
	}
	if webContentHash(item.Content) != in.ExpectedContentHash || item.Title != in.ExpectedTitle {
		return errors.New("正文或标题已在比较期间发生变化，请重新比较后采用")
	}
	baseline, err := l.webSourceBaseline(ctx, in.ItemID)
	if err != nil {
		return err
	}
	title := item.Title
	if baseline != nil && item.Title == baseline.Title {
		title = version.Title
	}
	update, found, err := l.knowledgeUpdateStmts(ctx, in.ItemID, UpdateInput{Title: optString{Set: true, Val: title}, Content: optString{Set: true, Val: version.Markdown}})
	if err != nil {
		return err
	}
	if !found {
		return errors.New("条目或完整版本不存在")
	}
	baselineTitle := "__legacy_unknown_title__"
	if baseline != nil {
		baselineTitle = baseline.Title
		if baseline.Title == item.Title {
			baselineTitle = title
		}
	}
	stmts := []sqlite.Statement{webSourceLocalSnapshot(in.ItemID, item.Title, item.Content, version.SourceURL)}
	stmts = append(stmts, update...)
	stmts = append(stmts, webSourceInvalidateStmts(in.ItemID)...)
	stmts = append(stmts, webSourceBaselineStmt(in.ItemID, version.ID, version.Markdown, baselineTitle, version.ContentHash, item.HasSummary))
	_, err = l.db.Transaction(ctx, stmts)
	return err
}
