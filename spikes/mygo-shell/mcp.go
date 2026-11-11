package main

// 对应 mcp.ipc.ts、services/mcp-scope.ts、services/mcp-install.ts 与 apps/desktop/src/mcp/tools.ts。
//
// 与 Electron 版的唯一结构差别：MCP server 不再是 ELECTRON_RUN_AS_NODE 跑的 guizhi-mcp.mjs，
// 而是本程序自己的 `--mcp` 模式（见 mcp_server.go），所以 mcp:config 给出的启动命令是
// <本程序> --mcp，用户机器上同样不需要 Node。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	mcpScopeFileName = "mcp.json"
	mcpServerName    = "guizhi"
	mcpDefaultLimit  = 10.0
	mcpMaxLimit      = 50.0
	mcpSnippetChars  = 120
)

type mcpScope struct {
	Mode               string
	AllowedCollections []string
	AllowUncategorized bool
}

var defaultMcpScope = mcpScope{Mode: "all", AllowedCollections: []string{}, AllowUncategorized: true}

// parseMcpScope 对应 parseMcpScope：坏数据一律退回「全部可见」。
func parseMcpScope(raw any) mcpScope {
	o, ok := raw.(*jsObj)
	if !ok && !jsIsObject(raw) {
		return defaultMcpScope
	}
	scope := mcpScope{Mode: "all", AllowedCollections: []string{}, AllowUncategorized: true}
	if !ok {
		return scope // 数组：能当对象用，但没有这些属性
	}
	if o.get("mode") == "selected" {
		scope.Mode = "selected"
	}
	if arr, ok := o.get("allowedCollectionIds").(jsArr); ok {
		seen := map[string]bool{}
		for _, v := range arr {
			if s, ok := v.(string); ok && s != "" && !seen[s] {
				seen[s] = true
				scope.AllowedCollections = append(scope.AllowedCollections, s)
			}
		}
	}
	if b, ok := o.get("allowUncategorized").(bool); ok {
		scope.AllowUncategorized = b
	}
	return scope
}

func (s mcpScope) toMap() map[string]any {
	return map[string]any{"mode": s.Mode, "allowedCollectionIds": s.AllowedCollections, "allowUncategorized": s.AllowUncategorized}
}

// serialize 对应 serializeMcpScope：JSON.stringify(..., null, 2) + "\n"。
func (s mcpScope) serialize() string {
	var b strings.Builder
	b.WriteString("{\n  \"version\": 1,\n  \"mode\": " + jsonString(s.Mode) + ",\n  \"allowedCollectionIds\": ")
	if len(s.AllowedCollections) == 0 {
		b.WriteString("[]")
	} else {
		b.WriteString("[\n")
		for i, id := range s.AllowedCollections {
			b.WriteString("    " + jsonString(id))
			if i < len(s.AllowedCollections)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  ]")
	}
	b.WriteString(fmt.Sprintf(",\n  \"allowUncategorized\": %t\n}\n", s.AllowUncategorized))
	return b.String()
}

func (s mcpScope) visible(collectionID any) bool {
	if s.Mode == "all" {
		return true
	}
	id, _ := collectionID.(string)
	if id == "" {
		return s.AllowUncategorized
	}
	for _, a := range s.AllowedCollections {
		if a == id {
			return true
		}
	}
	return false
}

func (s mcpScope) empty() bool {
	return s.Mode == "selected" && len(s.AllowedCollections) == 0 && !s.AllowUncategorized
}

func readMcpScope(configDir string) mcpScope {
	data, err := os.ReadFile(filepath.Join(configDir, mcpScopeFileName))
	if err != nil {
		return defaultMcpScope
	}
	v, err := jsParse(string(data))
	if err != nil {
		return defaultMcpScope
	}
	return parseMcpScope(v)
}

func writeMcpScope(configDir string, s mcpScope) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, mcpScopeFileName), []byte(s.serialize()), 0o644)
}

// ── 检索 / 读取 ──

func mcpFormatDate(ms int64) string { return time.UnixMilli(ms).Local().Format("2006-01-02") }

func mcpCollapseSnippet(text string) string {
	flat := collapseWhitespace(text)
	if utf16Len(flat) > mcpSnippetChars {
		return utf16Slice(flat, mcpSnippetChars) + "…"
	}
	return flat
}

type mcpSearchInput struct {
	Query      *string
	Limit      *float64
	Platform   string
	Collection string
}

func (l *Library) mcpSearch(ctx context.Context, in mcpSearchInput, scope mcpScope) (string, error) {
	query := ""
	if in.Query != nil {
		query = jsTrim(*in.Query)
	}
	if query == "" {
		return "请给出检索关键词。", nil
	}
	if scope.empty() {
		return "归知当前没有向 MCP 开放任何合集。请在归知的「设置 → MCP 接入」里调整可访问范围。", nil
	}
	listed, err := l.collectionList(ctx)
	if err != nil {
		return "", err
	}
	var all []map[string]any
	switch v := listed.(type) {
	case []map[string]any:
		all = v
	case []any:
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				all = append(all, m)
			}
		}
	}
	var cols []map[string]any
	for _, c := range all {
		if scope.Mode == "all" {
			cols = append(cols, c)
			continue
		}
		for _, id := range scope.AllowedCollections {
			if c["id"] == id {
				cols = append(cols, c)
				break
			}
		}
	}
	collectionID := ""
	if wanted := jsTrim(in.Collection); wanted != "" {
		wanted = jsLower(wanted)
		for _, c := range cols {
			if name, _ := c["name"].(string); jsLower(name) == wanted {
				collectionID, _ = c["id"].(string)
				break
			}
		}
		if collectionID == "" {
			names := make([]string, 0, len(cols))
			for _, c := range cols {
				names = append(names, c["name"].(string))
			}
			joined := strings.Join(names, "、")
			if joined == "" {
				joined = "（还没有合集）"
			}
			return "没有名为「" + in.Collection + "」的合集。现有：" + joined, nil
		}
	}
	names := map[string]string{}
	for _, c := range cols {
		names[c["id"].(string)] = c["name"].(string)
	}
	limit := mcpDefaultLimit
	if in.Limit != nil {
		limit = *in.Limit
	}
	limit = max(1, min(limit, mcpMaxLimit))

	q := map[string]any{"scope": "all", "search": query, "searchMode": "recall", "includeArchived": true, "limit": limit}
	if collectionID != "" {
		q["collectionId"] = collectionID
	}
	if p := jsTrim(in.Platform); p != "" {
		q["platform"] = p
	}
	if scope.Mode != "all" {
		q["collectionScope"] = map[string]any{"ids": scope.AllowedCollections, "includeUncategorized": scope.AllowUncategorized}
	}
	raw, _ := json.Marshal(q)
	var lq ListQuery
	if err := json.Unmarshal(raw, &lq); err != nil {
		return "", err
	}
	res, err := l.knowledgeList(ctx, lq)
	if err != nil {
		return "", err
	}
	out := res.(map[string]any)
	entries, _ := out["entries"].([]map[string]any)
	total := asInt(out["total"])
	if len(entries) == 0 {
		return "没有找到与「" + query + "」相关的条目。可以换个说法，或者把关键词拆得更短一些。", nil
	}
	more := ""
	if total > int64(len(entries)) {
		more = fmt.Sprintf("，下面列出前 %d 条", len(entries))
	}
	lines := []string{fmt.Sprintf("找到 %d 条与「%s」相关的条目%s：", total, query, more), ""}
	for i, e := range entries {
		meta := []string{"id=" + e["id"].(string), "type=" + e["itemType"].(string)}
		if p, _ := e["platform"].(string); p != "" {
			meta = append(meta, "platform="+p)
		}
		if cid, _ := e["collectionId"].(string); cid != "" {
			name, ok := names[cid]
			if !ok {
				name = "?"
			}
			meta = append(meta, "collection="+name)
		}
		meta = append(meta, "updated="+mcpFormatDate(asInt(e["updatedAt"])))
		title, _ := e["title"].(string)
		if title == "" {
			title = "（无标题）"
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, title), "   "+strings.Join(meta, "  "))
		if snippet := mcpCollapseSnippet(e["snippet"].(string)); snippet != "" {
			lines = append(lines, "   "+snippet)
		}
		lines = append(lines, "")
	}
	lines = append(lines, "用 read_item 工具加上面某条的 id，可以读到该条目的完整记录（视频/音频会附完整口播文字稿）。")
	return strings.Join(lines, "\n"), nil
}

func (l *Library) mcpReadItem(ctx context.Context, id string, includeFullText bool, scope mcpScope) (text string, found bool, err error) {
	id = jsTrim(id)
	if id == "" {
		return "请给出条目 id。", false, nil
	}
	got, err := l.knowledgeGet(ctx, id)
	if err != nil {
		return "", false, err
	}
	item, _ := got.(map[string]any)
	if item == nil {
		return "找不到 id 为 " + id + " 的条目。它可能已被删除；用 search_knowledge 重新检索一次。", false, nil
	}
	if !scope.visible(item["collectionId"]) {
		return "条目 " + id + " 不在归知向 MCP 开放的范围内。可在归知的「设置 → MCP 接入」里调整可访问范围。", false, nil
	}
	collectionName := ""
	if cid, _ := item["collectionId"].(string); cid != "" {
		rows, err := l.db.Query(ctx, "SELECT name FROM collections WHERE id = ?", cid)
		if err != nil {
			return "", false, err
		}
		if len(rows.Values) > 0 {
			collectionName, _ = rows.Values[0][0].(string)
		}
	}
	str := func(k string) string { s, _ := item[k].(string); return s }
	h := aiHandoffItem{
		Title: str("title"), Content: str("content"), Transcript: str("transcript"), Summary: str("summary"),
		ItemType: str("itemType"), SourceURI: str("sourceUri"), Collection: collectionName, CreatedAtMs: asInt(item["createdAt"]),
	}
	if tags, ok := item["tags"].([]map[string]any); ok {
		for _, t := range tags {
			n, _ := t["name"].(string)
			h.Tags = append(h.Tags, n)
		}
	}
	return buildAiHandoff(h, includeFullText).Text, true, nil
}

// ── 一键安装进 Cursor ──

type mcpLaunchSpec struct {
	Command string
	Args    []string
	Env     [][2]string
}

func (s mcpLaunchSpec) entryJSON() string {
	o := newObj()
	o.set("command", s.Command)
	args := jsArr{}
	for _, a := range s.Args {
		args = append(args, a)
	}
	o.set("args", args)
	if len(s.Env) > 0 {
		env := newObj()
		for _, kv := range s.Env {
			env.set(kv[0], kv[1])
		}
		o.set("env", env)
	}
	out, _ := jsStringify(o)
	return out
}

func cursorConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor", "mcp.json")
}

// installIntoCursorAt 对应 installIntoCursorAt：用 jsonc 的就地编辑合并，保住用户手写的注释与排版。
// 返回的 error 对应 TS 里抛出的异常（例如 mcpServers 不是对象）。
func installIntoCursorAt(filePath string, spec mcpLaunchSpec) (map[string]any, error) {
	raw, readErr := os.ReadFile(filePath)
	exists := readErr == nil
	original := string(raw)
	hasContent := strings.TrimFunc(original, jsSpace) != ""
	replaced := false
	if exists && hasContent {
		// 文件坏了就住手：覆盖掉换来的是「一键安装顺便清空了我的 MCP」
		root, errCount := parseJSONCTree(toUTF16(original), jsoncParseOpts{allowTrailingComma: true})
		if errCount > 0 {
			return map[string]any{"success": false, "filePath": filePath,
				"error": filePath + " 不是合法的 JSON，为免弄丢已有配置没有改动它。请手工修好，或用下面的配置片段自行添加。"}, nil
		}
		replaced = mcpEntryExists(root)
	}
	source := "{}"
	if hasContent {
		source = original
	}
	edited, err := jsoncSetProperty(toUTF16(source), []string{"mcpServers", mcpServerName}, spec.entryJSON())
	if err != nil {
		return nil, err
	}
	next := fromU16(edited)
	out := map[string]any{"success": true, "filePath": filePath, "replaced": replaced}
	fail := func(err error) (map[string]any, error) {
		return map[string]any{"success": false, "filePath": filePath, "error": err.Error()}, nil
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fail(err)
	}
	if exists {
		// 单份、每次覆盖：这个操作是幂等的，留最近一次就够回退
		backup := filePath + ".guizhi-backup"
		if err := os.WriteFile(backup, []byte(original), 0o644); err != nil {
			return fail(err)
		}
		out["backupPath"] = backup
	}
	if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	if err := os.WriteFile(filePath, []byte(next), 0o644); err != nil {
		return fail(err)
	}
	return out, nil
}

// mcpEntryExists 对应 Boolean(parsed?.mcpServers?.[guizhi])：重复键以最后一个为准。
func mcpEntryExists(root *jsoncNode) bool {
	last := func(obj *jsoncNode, key string) *jsoncNode {
		if obj == nil || obj.kind != "object" {
			return nil
		}
		var v *jsoncNode
		for _, p := range obj.children {
			if len(p.children) == 2 && p.children[0].value == key {
				v = p.children[1]
			}
		}
		return v
	}
	entry := last(last(root, "mcpServers"), mcpServerName)
	return entry != nil && entry.truthy
}

// ── 频道 ──

func (l *Library) mcpLaunchSpec() mcpLaunchSpec {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return mcpLaunchSpec{Command: exe, Args: []string{"--mcp"}}
}

func (l *Library) registerMcp(b *Bridge) {
	b.handlers["mcp:config"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		spec := l.mcpLaunchSpec()
		return map[string]any{"available": true, "serverPath": spec.Command, "command": spec.Command, "args": spec.Args, "env": map[string]string{}}, nil
	}
	b.handlers["mcp:getScope"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return readMcpScope(l.configDir).toMap(), nil
	}
	b.handlers["mcp:setScope"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		// 入参来自渲染进程：坏形状规整成安全的默认值，而不是原样写进 MCP server 也要读的文件
		scope := parseMcpScope(jsArg(a, 0))
		if err := writeMcpScope(l.configDir, scope); err != nil {
			return nil, err
		}
		return scope.toMap(), nil
	}
	b.handlers["mcp:install"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		client := jsArg(a, 0)
		if client != "cursor" {
			return map[string]any{"success": false, "error": "不支持一键安装：" + jsToString(client)}, nil
		}
		out, err := installIntoCursorAt(cursorConfigPath(), l.mcpLaunchSpec())
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

