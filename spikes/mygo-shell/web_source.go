package main

// 对应 packages/db/src/web-source.ts 的 initialize：网页条目创建时记录首个"远程版本"与基线。
// 随条目创建事务一起提交；后续比较/采用/更新（check/adopt）随网页采集的 IPC 切片迁移。

import (
	"crypto/sha256"
	"encoding/hex"

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
