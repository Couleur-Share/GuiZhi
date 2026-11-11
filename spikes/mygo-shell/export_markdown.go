package main

// 对应 services/export-markdown.ts 与 backup.ipc.ts 里的 export:markdown / export:aiHandoff。
// 每个条目导出为一个 .md（YAML frontmatter + 正文），按集合建子目录；引用到的本地资产拷进 assets/。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const exportAssetsDir = "assets"

var (
	reservedDeviceName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])$`)
	assetRefPattern    = regexp.MustCompile(`local-(image|video)://([A-Za-z0-9_.-]+)`)
	assetLinkPattern   = regexp.MustCompile(`local-(?:image|video)://([A-Za-z0-9_.-]+)`)
	safeAssetName      = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// sanitizeFileName 对应 sanitizeFileName：Windows/macOS 通用的文件名清洗；空结果由调用方兜底。
func sanitizeFileName(name string, maxLength int) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range name {
		switch {
		case strings.ContainsRune(`\/:*?"<>|`, r):
			r = ' '
		case r <= 0x1f:
			continue // 控制字符直接删除（先于空白折叠，与 TS 两步 replace 的顺序一致）
		}
		if jsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	s := jsTrim(b.String())
	s = jsTrim(utf16Slice(s, maxLength))
	return strings.TrimRight(s, ". ")
}

func isSafeAssetFileName(name string) bool {
	return safeAssetName.MatchString(name) && name != "." && name != ".." && !strings.HasPrefix(name, ".")
}

// extractAllLocalAssetRefs 对应 shared/utils/media-refs.ts：先 local-image 后 local-video，去重保序。
func extractAllLocalAssetRefs(content string) []string {
	if content == "" {
		return nil
	}
	var refs []string
	seen := map[string]bool{}
	for _, protocol := range []string{"image", "video"} {
		for _, m := range assetRefPattern.FindAllStringSubmatch(content, -1) {
			if m[1] == protocol && isSafeAssetFileName(m[2]) && !seen[m[2]] {
				seen[m[2]] = true
				refs = append(refs, m[2])
			}
		}
	}
	return refs
}

func rewriteAssetLinks(content string, copied map[string]bool, relativePrefix string) string {
	if content == "" || len(copied) == 0 {
		return content
	}
	return assetLinkPattern.ReplaceAllStringFunc(content, func(match string) string {
		name := assetLinkPattern.FindStringSubmatch(match)[1]
		if copied[name] {
			return relativePrefix + name
		}
		return match
	})
}

type exportRow struct {
	id, title, content, itemType, status string
	summary, transcript, collectionID    *string
	collectionName                       *string
	favorite                             bool
	createdAt, updatedAt                 int64
}

func strPtr(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func isoMillis(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

func buildFrontmatter(row exportRow, tagNames []string, sourceURI *string) string {
	lines := []string{"---", "title: " + jsQuote(row.title), "type: " + row.itemType, "status: " + row.status}
	if len(tagNames) > 0 {
		quoted := make([]string, len(tagNames))
		for i, n := range tagNames {
			quoted[i] = jsQuote(n)
		}
		lines = append(lines, "tags: ["+strings.Join(quoted, ",")+"]")
	}
	if row.collectionName != nil && *row.collectionName != "" {
		lines = append(lines, "collection: "+jsQuote(*row.collectionName))
	}
	if sourceURI != nil && *sourceURI != "" {
		lines = append(lines, "source: "+jsQuote(*sourceURI))
	}
	if row.favorite {
		lines = append(lines, "favorite: true")
	}
	if row.summary != nil && *row.summary != "" {
		lines = append(lines, "summary: "+jsQuote(*row.summary))
	}
	lines = append(lines, "created: "+isoMillis(row.createdAt), "updated: "+isoMillis(row.updatedAt), "---")
	return strings.Join(lines, "\n")
}

func buildDocument(row exportRow, tagNames []string, sourceURI *string, content string) string {
	parts := []string{buildFrontmatter(row, tagNames, sourceURI), "", content}
	if row.transcript != nil && *row.transcript != "" {
		parts = append(parts, "", "## 转写文本", "", *row.transcript)
	}
	return strings.TrimRight(strings.Join(parts, "\n"), "\n") + "\n"
}

// copyReferencedAssets 把条目引用到的本地资产拷进导出目录，返回成功拷贝的文件名集合。
func (l *Library) copyReferencedAssets(contents []string, targetDir string) (map[string]bool, error) {
	var refs []string
	seen := map[string]bool{}
	for _, c := range contents {
		for _, ref := range extractAllLocalAssetRefs(c) {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	type pair struct{ ref, source string }
	var resolved []pair
	for _, ref := range refs {
		for _, dir := range []string{l.paths.ImagesDir(), l.paths.VideosDir()} {
			candidate := filepath.Join(dir, ref)
			if _, err := os.Stat(candidate); err == nil {
				resolved = append(resolved, pair{ref, candidate})
				break
			}
		}
	}
	copied := map[string]bool{}
	if len(resolved) == 0 {
		return copied, nil
	}
	assets := filepath.Join(targetDir, exportAssetsDir)
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return nil, err
	}
	for _, p := range resolved {
		if err := copyFile(p.source, filepath.Join(assets, p.ref)); err != nil {
			return nil, err
		}
		copied[p.ref] = true
	}
	return copied, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// exportKnowledgeToMarkdown 把全部未删除条目导出到 targetDir（目录须已存在）。
func (l *Library) exportKnowledgeToMarkdown(ctx context.Context, targetDir string) (count, assetCount int, err error) {
	rs, err := l.db.Query(ctx, `SELECT i.id, i.title, i.content, i.summary, i.transcript,
            i.item_type, i.status, i.collection_id, i.is_favorite,
            i.created_at, i.updated_at,
            c.name AS collection_name
     FROM knowledge_items i
     LEFT JOIN collections c ON c.id = i.collection_id
     WHERE i.deleted_at IS NULL
     ORDER BY i.created_at ASC`)
	if err != nil {
		return 0, 0, err
	}
	rows := make([]exportRow, 0, len(rs.Values))
	for _, v := range rs.Values {
		r := exportRow{
			id: v[0].(string), summary: strPtr(v[3]), transcript: strPtr(v[4]), collectionID: strPtr(v[7]),
			favorite: asInt(v[8]) == 1, createdAt: asInt(v[9]), updatedAt: asInt(v[10]), collectionName: strPtr(v[11]),
		}
		r.title, _ = v[1].(string)
		r.content, _ = v[2].(string)
		r.itemType, _ = v[5].(string)
		r.status, _ = v[6].(string)
		rows = append(rows, r)
	}

	tagNames := map[string][]string{}
	trs, err := l.db.Query(ctx, `SELECT kit.item_id AS item_id, t.name AS name
     FROM knowledge_item_tags kit
     JOIN tags t ON t.id = kit.tag_id
     ORDER BY t.name`)
	if err != nil {
		return 0, 0, err
	}
	for _, v := range trs.Values {
		id := v[0].(string)
		tagNames[id] = append(tagNames[id], v[1].(string))
	}
	srs, err := l.db.Query(ctx, `SELECT item_id, source_uri
     FROM source_records
     WHERE source_uri IS NOT NULL
     ORDER BY captured_at ASC`)
	if err != nil {
		return 0, 0, err
	}
	sources := map[string]string{} // 按 captured_at 升序遍历，后写覆盖先写 → 留下最新来源
	for _, v := range srs.Values {
		sources[v[0].(string)] = v[1].(string)
	}

	// 集合目录名：同名集合冲突时追加 id 前缀区分；Windows/macOS 大小写不敏感，占位表按小写记
	dirs := map[string]string{}
	claimed := map[string]bool{}
	for _, row := range rows {
		if row.collectionID == nil || *row.collectionID == "" {
			continue
		}
		if _, ok := dirs[*row.collectionID]; ok {
			continue
		}
		name := ""
		if row.collectionName != nil {
			name = *row.collectionName
		}
		base := sanitizeFileName(name, 60)
		if base == "" {
			base = "collection"
		}
		if reservedDeviceName.MatchString(base) {
			base += "-dir"
		}
		dirName := base
		if claimed[jsLower(base)] {
			dirName = base + "-" + utf16Slice(*row.collectionID, 8)
		}
		claimed[jsLower(dirName)] = true
		dirs[*row.collectionID] = dirName
	}

	contents := make([]string, len(rows))
	for i, r := range rows {
		contents[i] = r.content
	}
	copied, err := l.copyReferencedAssets(contents, targetDir)
	if err != nil {
		return 0, 0, err
	}
	for _, row := range rows {
		dirName, hasDir := "", false
		if row.collectionID != nil && *row.collectionID != "" {
			dirName, hasDir = dirs[*row.collectionID], true
		}
		itemDir, prefix := targetDir, "./"+exportAssetsDir+"/"
		if hasDir && dirName != "" {
			itemDir, prefix = filepath.Join(targetDir, dirName), "../"+exportAssetsDir+"/"
		}
		if err := os.MkdirAll(itemDir, 0o755); err != nil {
			return count, len(copied), err
		}
		baseName := sanitizeFileName(row.title, 60)
		if baseName == "" {
			baseName = "untitled"
		}
		fileName := baseName + "-" + utf16Slice(row.id, 8) + ".md"
		var src *string
		if s, ok := sources[row.id]; ok {
			src = &s
		}
		doc := buildDocument(row, tagNames[row.id], src, rewriteAssetLinks(row.content, copied, prefix))
		if err := os.WriteFile(filepath.Join(itemDir, fileName), nodeUTF8(doc), 0o644); err != nil {
			return count, len(copied), err
		}
		count++
	}
	return count, len(copied), nil
}

func formatExportTimestamp(t time.Time) string {
	return t.Format("20060102-150405")
}

// registerExport 注册 export:markdown / export:aiHandoff。对话框由 l.pickDirectory / l.saveFileDialog 提供，测试里替换。
func (l *Library) registerExport(b *Bridge) {
	b.handlers["export:markdown"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		if l.pickDirectory == nil {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		dir, err := l.pickDirectory("选择导出目录")
		if err != nil {
			return nil, err
		}
		if dir == "" {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		exportDir := filepath.Join(dir, "GuiZhi-Export-"+formatExportTimestamp(time.Now()))
		if err := os.MkdirAll(exportDir, 0o755); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		count, assets, err := l.exportKnowledgeToMarkdown(ctx, exportDir)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		fmt.Printf("[export] Markdown 导出完成: %d 条 + %d 个资产 → %s\n", count, assets, exportDir)
		return map[string]any{"success": true, "count": count, "dir": exportDir}, nil
	}

	b.handlers["export:aiHandoff"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		req := jsArg(a, 0)
		text, ok := jsOptGet(req, "text").(string)
		if !ok || jsTrim(text) == "" {
			return map[string]any{"success": false, "error": "交接稿内容为空"}, nil
		}
		title := ""
		switch t := jsOptGet(req, "title").(type) {
		case string:
			title = t
		case jsUndefinedT, jsNullT:
		default:
			return nil, errors.New("(intermediate value).replace is not a function") // TS 对非字符串 title 抛 TypeError
		}
		name := sanitizeFileName(title, 60)
		if name == "" {
			name = "guizhi-note"
		}
		fileName := name + ".md"
		if l.saveFileDialog == nil {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		path, err := l.saveFileDialog("另存为 AI 交接稿", defaultExportPath(fileName), []dialogFilter{{"Markdown", []string{"md"}}})
		if err != nil {
			return nil, err
		}
		if path == "" {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		if err := os.WriteFile(path, nodeUTF8(text), 0o644); err != nil {
			return map[string]any{"success": false, "error": "写入文件失败：" + err.Error()}, nil
		}
		return map[string]any{"success": true, "filePath": path}, nil
	}
}

// defaultExportPath 另存为对话框的默认落点；取不到下载目录时只给文件名，交给系统决定。
func defaultExportPath(fileName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return fileName
	}
	return filepath.Join(home, "Downloads", fileName)
}
