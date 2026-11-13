package main

// 归知 MCP server（stdio）——Go 版，由 `--mcp` 启动，替代 TS 版 apps/desktop/src/mcp/server.ts
// （后者靠 ELECTRON_RUN_AS_NODE 运行 JS 包；Go 外壳没有 Node，直接用本体可执行文件）。
//
// 硬约束与 TS 版一致：stdout 只能是 JSON-RPC；日志一律写 stderr。
// 协议层按 @modelcontextprotocol/server 2.0.0 的实测行为对拍（含协议版本协商、zod 校验报错原文）。
// 差异（有意）：每个请求串行处理（TS 版并发）；响应键顺序不同（JSON 语义相同）。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// SDK 2.0.0 的 legacy 协商表（实测：2026-07-28 及未知版本回落到最新）。
var mcpSupportedVersions = []string{"2024-10-07", "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}

const mcpLatestVersion = "2025-11-25"

// MCP server 版本：TS 版开发态回落 "0.0.0-dev"，打包态由构建注入；Go 版可用 ldflags 覆盖。
var mcpServerVersion = "0.0.0-dev"

type mcpRPCRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

// zod 的 invalid_type 问题（协议层校验报错会把 issues 数组整段 JSON.stringify 进 message）。
type zodTypeIssue struct {
	Expected string `json:"expected"`
	Code     string `json:"code"`
	Path     []any  `json:"path"`
	Message  string `json:"message"`
}

func zodIssuesJSON(issues []zodTypeIssue) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(issues)
	return strings.TrimRight(buf.String(), "\n")
}

// zodTypeName 对应 zod v4 的 parsedType：invalid_type 报错里的 "received X"。
func zodTypeName(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if math.IsNaN(t) {
			return "nan"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func zodExpected(expected string, v any, present bool) string {
	got := "undefined"
	if present {
		got = zodTypeName(v)
	}
	return fmt.Sprintf("Invalid input: expected %s, received %s", expected, got)
}

// ── 工具定义（文案与 server.ts 逐字一致）──

const mcpSearchDesc = "在用户的「归知」个人知识库里做全文检索，返回匹配条目的 id、标题、来源平台与正文片段。" +
	"知识库里存的是用户主动采集的内容：抖音/B站/小红书/YouTube 视频（含完整口播转写稿）、" +
	"V2EX 讨论帖、网页剪藏、本地图文与音视频。" +
	"想了解某条的完整内容时，再用 read_item 加上这里返回的 id。"

const mcpReadDesc = "按 id 读取归知知识库中某个条目的完整记录，返回一份自包含的 Markdown：" +
	"来源链接、平台、作者、时长等元信息，AI 生成的内容总结，以及完整的口播文字稿或图中文字。" +
	"开头附有阅读须知，说明这份材料的来源与可信度边界，请照它说的做。"

var mcpPlatforms = []string{"douyin", "bilibili", "xiaohongshu", "youtube", "v2ex", "web", "local"}

func mcpToolList() []any {
	schema := func(props map[string]any, order []string, required []string) json.RawMessage {
		// 键序与 zod 输出一致：type, $schema, properties, required
		var sb strings.Builder
		sb.WriteString(`{"type":"object","$schema":"https://json-schema.org/draft/2020-12/schema","properties":{`)
		for i, k := range order {
			if i > 0 {
				sb.WriteString(",")
			}
			kb, _ := json.Marshal(k)
			vb, _ := marshalNoEscape(props[k])
			sb.Write(kb)
			sb.WriteString(":")
			sb.Write(vb)
		}
		sb.WriteString(`},"required":`)
		rb, _ := json.Marshal(required)
		sb.Write(rb)
		sb.WriteString("}")
		return json.RawMessage(sb.String())
	}
	searchProps := map[string]any{
		"query":      rawJSON(`{"type":"string","description":"检索关键词。可以是词组；中文长句会被自动拆词，不必逐字精确"}`),
		"limit":      rawJSON(`{"description":"最多返回几条，默认 10","type":"integer","minimum":1,"maximum":50}`),
		"platform":   rawJSON(`{"description":"只看某个来源平台的条目","type":"string","enum":["douyin","bilibili","xiaohongshu","youtube","v2ex","web","local"]}`),
		"collection": rawJSON(`{"description":"只看某个合集（按名字精确匹配）","type":"string"}`),
	}
	readProps := map[string]any{
		"id":              rawJSON(`{"type":"string","description":"条目 id，由 search_knowledge 返回"}`),
		"includeFullText": rawJSON(`{"description":"是否包含完整口播文字稿与论坛逐楼回复，默认 true。只想快速判断这条讲什么时可以设为 false，能省下大量 token","type":"boolean"}`),
	}
	return []any{
		map[string]any{"name": "search_knowledge", "title": "检索归知知识库", "description": mcpSearchDesc,
			"inputSchema": schema(searchProps, []string{"query", "limit", "platform", "collection"}, []string{"query"})},
		map[string]any{"name": "read_item", "title": "读取归知条目全文", "description": mcpReadDesc,
			"inputSchema": schema(readProps, []string{"id", "includeFullText"}, []string{"id"})},
	}
}

func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

// ── 入参校验（zod 对象：按 shape 顺序逐字段校验，未知键静默丢弃）──

// validateToolArgs 返回校验错误（SDK 把多条问题以 ", " 拼接）；ok 时通过 out 返回已规整的字段。
func validateToolArgs(tool string, raw any, has bool) (issues []string, args map[string]any) {
	args = map[string]any{}
	// SDK：args ?? {}
	if !has || raw == nil {
		raw = map[string]any{}
	}
	obj, isObj := raw.(map[string]any)
	if !isObj {
		return []string{zodExpected("object", raw, true)}, nil
	}
	field := func(name string) (any, bool) { v, ok := obj[name]; return v, ok }
	str := func(name string, required bool) {
		v, ok := field(name)
		if !ok {
			if required {
				issues = append(issues, name+": "+zodExpected("string", nil, false))
			}
			return
		}
		if _, isStr := v.(string); !isStr {
			issues = append(issues, name+": "+zodExpected("string", v, true))
			return
		}
		args[name] = v
	}
	switch tool {
	case "search_knowledge":
		str("query", true)
		if v, ok := field("limit"); ok {
			n, isNum := v.(float64)
			switch {
			case !isNum:
				issues = append(issues, "limit: "+zodExpected("number", v, true))
			default:
				bad := false
				switch {
				case n != math.Trunc(n) || math.IsInf(n, 0): // int 检查带 abort，后面的 min/max 不再报
					issues = append(issues, "limit: Invalid input: expected int, received number")
					bad = true
				case n < 1:
					issues = append(issues, "limit: Too small: expected number to be >=1")
					bad = true
				case n > 50:
					issues = append(issues, "limit: Too big: expected number to be <=50")
					bad = true
				}
				if !bad {
					args["limit"] = n
				}
			}
		}
		if v, ok := field("platform"); ok {
			s, isStr := v.(string)
			valid := false
			if isStr {
				for _, p := range mcpPlatforms {
					if p == s {
						valid = true
					}
				}
			}
			if !valid {
				quoted := make([]string, len(mcpPlatforms))
				for i, p := range mcpPlatforms {
					quoted[i] = `"` + p + `"`
				}
				issues = append(issues, "platform: Invalid option: expected one of "+strings.Join(quoted, "|"))
			} else {
				args["platform"] = s
			}
		}
		str("collection", false)
	case "read_item":
		str("id", true)
		if v, ok := field("includeFullText"); ok {
			if b, isBool := v.(bool); isBool {
				args["includeFullText"] = b
			} else {
				issues = append(issues, "includeFullText: "+zodExpected("boolean", v, true))
			}
		}
	}
	return issues, args
}

// ── server 主体 ──

type mcpServer struct {
	dataDir string // userData 目录（含 data/knowledge.db 与 config/）；空则按默认规则解析

	mu  sync.Mutex
	lib *Library
}

// resolveKnowledgeDbPath 对应 TS 的 resolveKnowledgeDbPath：GUIZHI_DATA_DIR 优先。
// 未设置时回落到 %APPDATA%\GuiZhi（便携/自定义数据目录的四级解析尚未移植，见迁移文档）。
func (s *mcpServer) resolveKnowledgeDbPath() string {
	dir := strings.TrimSpace(s.dataDir)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("GUIZHI_DATA_DIR"))
	}
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
		return filepath.Join(dir, "data", "knowledge.db")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "GuiZhi", "data", "knowledge.db")
}

// handle 延迟到首次工具调用才开库：错误原文要原样回给模型，而不是表现为 server 启动失败。
func (s *mcpServer) openLib(ctx context.Context) (*Library, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dbPath := s.resolveKnowledgeDbPath()
	if s.lib != nil {
		return s.lib, dbPath, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, dbPath, fmt.Errorf("找不到归知的知识库文件：%s\n若归知把数据目录改到了别处，请给 MCP server 设置 GUIZHI_DATA_DIR 环境变量指向该目录（含 data 子目录的那一层）。", dbPath)
	}
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{ReadOnly: true, BusyTimeout: 8 * time.Second})
	if err != nil {
		return nil, dbPath, err
	}
	s.lib = &Library{db: db}
	return s.lib, dbPath, nil
}

func (s *mcpServer) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lib != nil {
		s.lib.db.Close()
		s.lib = nil
	}
}

func mcpTextResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isError}
}

func (s *mcpServer) callTool(ctx context.Context, name string, rawArgs any, hasArgs bool) (any, *mcpRPCError) {
	if name != "search_knowledge" && name != "read_item" {
		return nil, &mcpRPCError{Code: -32602, Message: "Tool " + name + " not found"}
	}
	issues, args := validateToolArgs(name, rawArgs, hasArgs)
	if len(issues) > 0 {
		return mcpTextResult("Input validation error: Invalid arguments for tool "+name+": "+strings.Join(issues, ", "), true), nil
	}
	fail := func(prefix string, err error) any { return mcpTextResult(prefix+err.Error(), true) }
	lib, dbPath, err := s.openLib(ctx)
	if name == "search_knowledge" {
		if err != nil {
			return fail("检索归知知识库失败：", err), nil
		}
		scope := readMcpScope(filepath.Join(filepath.Dir(filepath.Dir(dbPath)), "config"))
		in := mcpSearchInput{}
		if q, ok := args["query"].(string); ok {
			in.Query = &q
		}
		if n, ok := args["limit"].(float64); ok {
			in.Limit = &n
		}
		in.Platform, _ = args["platform"].(string)
		in.Collection, _ = args["collection"].(string)
		text, err := lib.mcpSearch(ctx, in, scope)
		if err != nil {
			return fail("检索归知知识库失败：", err), nil
		}
		return mcpTextResult(text, false), nil
	}
	if err != nil {
		return fail("读取归知条目失败：", err), nil
	}
	scope := readMcpScope(filepath.Join(filepath.Dir(filepath.Dir(dbPath)), "config"))
	include := true
	if b, ok := args["includeFullText"].(bool); ok {
		include = b
	}
	text, found, err := lib.mcpReadItem(ctx, args["id"].(string), include, scope)
	if err != nil {
		return fail("读取归知条目失败：", err), nil
	}
	return mcpTextResult(text, !found), nil
}

// dispatch 处理一条请求；返回 nil 表示无需响应（通知）。
func (s *mcpServer) dispatch(ctx context.Context, req mcpRPCRequest) *mcpRPCResponse {
	isRequest := len(req.ID) > 0 && string(req.ID) != "null"
	if !isRequest {
		return nil // 通知：notifications/initialized、cancelled 等一律静默
	}
	reply := func(result any) *mcpRPCResponse {
		return &mcpRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *mcpRPCResponse {
		return &mcpRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpRPCError{Code: code, Message: msg}}
	}
	var params any
	hasParams := len(req.Params) > 0
	if hasParams {
		_ = json.Unmarshal(req.Params, &params)
	}
	pm, _ := params.(map[string]any)

	switch req.Method {
	case "ping":
		return reply(map[string]any{})
	case "initialize":
		var issues []zodTypeIssue
		if !hasParams || pm == nil {
			issues = append(issues, zodTypeIssue{"object", "invalid_type", []any{"params"}, zodExpected("object", params, hasParams)})
		} else {
			if v, ok := pm["protocolVersion"]; !ok || !isString(v) {
				issues = append(issues, zodTypeIssue{"string", "invalid_type", []any{"params", "protocolVersion"}, zodExpected("string", v, ok)})
			}
			for _, k := range []string{"capabilities", "clientInfo"} {
				if v, ok := pm[k]; !ok || !isObject(v) {
					issues = append(issues, zodTypeIssue{"object", "invalid_type", []any{"params", k}, zodExpected("object", v, ok)})
				}
			}
		}
		if len(issues) > 0 {
			return fail(-32603, zodIssuesJSON(issues))
		}
		version := mcpLatestVersion
		for _, v := range mcpSupportedVersions {
			if v == pm["protocolVersion"] {
				version = v
			}
		}
		return reply(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "guizhi", "version": mcpServerVersion},
		})
	case "tools/list":
		return reply(map[string]any{"tools": mcpToolList()})
	case "tools/call":
		var issues []zodTypeIssue
		if !hasParams || pm == nil {
			issues = append(issues, zodTypeIssue{"object", "invalid_type", []any{"params"}, zodExpected("object", params, hasParams)})
		} else {
			if v, ok := pm["name"]; !ok || !isString(v) {
				issues = append(issues, zodTypeIssue{"string", "invalid_type", []any{"params", "name"}, zodExpected("string", v, ok)})
			}
			// arguments 在协议层是 record：出现但不是对象（含 null）时由 SDK 直接拒绝
			if v, ok := pm["arguments"]; ok && !isObject(v) {
				issues = append(issues, zodTypeIssue{"record", "invalid_type", []any{"params", "arguments"}, zodExpected("record", v, true)})
			}
		}
		if len(issues) > 0 {
			return fail(-32602, "Invalid tools/call request: "+zodIssuesJSON(issues))
		}
		args, hasArgs := pm["arguments"]
		result, rpcErr := s.callTool(ctx, pm["name"].(string), args, hasArgs)
		if rpcErr != nil {
			return fail(rpcErr.Code, rpcErr.Message)
		}
		return reply(result)
	}
	return fail(-32601, "Method not found")
}

func isString(v any) bool { _, ok := v.(string); return ok }
func isObject(v any) bool { _, ok := v.(map[string]any); return ok }

// runMcpServer 读 in 的逐行 JSON-RPC，把响应逐行写到 out；in 关闭时返回。
func runMcpServer(ctx context.Context, in io.Reader, out io.Writer, dataDir string) {
	srv := &mcpServer{dataDir: dataDir}
	defer srv.close()
	reader := bufio.NewReaderSize(in, 1<<20)
	var outMu sync.Mutex
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var req mcpRPCRequest
			if jerr := json.Unmarshal(trimmed, &req); jerr == nil && req.Method != "" {
				if resp := srv.dispatch(ctx, req); resp != nil {
					if data, merr := marshalNoEscape(resp); merr == nil {
						outMu.Lock()
						_, _ = out.Write(append(data, '\n'))
						outMu.Unlock()
					}
				}
			} else if jerr != nil {
				fmt.Fprintf(os.Stderr, "[guizhi-mcp] 忽略无法解析的输入行：%v\n", jerr)
			}
		}
		if err != nil {
			return
		}
	}
}
