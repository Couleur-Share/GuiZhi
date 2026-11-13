package main

// MCP 差分测试：golden.mcp 由 TS 真实实现（tools.ts / ai-handoff.ts / mcp-scope.ts / mcp-install.ts）生成。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type mcpGolden struct {
	MCP struct {
		Tools struct {
			Search []mcpToolCase `json:"search"`
			Read   []mcpToolCase `json:"read"`
		} `json:"tools"`
		Handoff []struct {
			Item            json.RawMessage `json:"item"`
			IncludeFullText bool            `json:"includeFullText"`
			Result          struct {
				Text         string `json:"text"`
				CharCount    int    `json:"charCount"`
				OmittedChars int    `json:"omittedChars"`
			} `json:"result"`
		} `json:"handoff"`
		Scopes []struct {
			Raw    json.RawMessage `json:"raw"`
			Parsed struct {
				Mode                 string   `json:"mode"`
				AllowedCollectionIDs []string `json:"allowedCollectionIds"`
				AllowUncategorized   bool     `json:"allowUncategorized"`
			} `json:"parsed"`
			Text string `json:"text"`
		} `json:"scopes"`
		Install []struct {
			Name   string          `json:"name"`
			Before *string         `json:"before"`
			Spec   json.RawMessage `json:"spec"`
			Result struct {
				Value json.RawMessage `json:"value"`
				Error *string         `json:"error"`
			} `json:"result"`
			After  *string `json:"after"`
			Backup *string `json:"backup"`
		} `json:"install"`
		Stdio struct {
			Requests []json.RawMessage `json:"requests"`
			Lines    []json.RawMessage `json:"lines"`
		} `json:"stdio"`
	} `json:"mcp"`
}

type mcpToolCase struct {
	Input map[string]any `json:"input"`
	Scope struct {
		Mode                 string   `json:"mode"`
		AllowedCollectionIDs []string `json:"allowedCollectionIds"`
		AllowUncategorized   bool     `json:"allowUncategorized"`
	} `json:"scope"`
	Result struct {
		Value json.RawMessage `json:"value"`
		Error *string         `json:"error"`
	} `json:"result"`
}

func (c mcpToolCase) scope() mcpScope {
	ids := c.Scope.AllowedCollectionIDs
	if ids == nil {
		ids = []string{}
	}
	return mcpScope{Mode: c.Scope.Mode, AllowedCollections: ids, AllowUncategorized: c.Scope.AllowUncategorized}
}

func loadMcpGolden(t *testing.T) (string, mcpGolden) {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g mcpGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.MCP.Tools.Search) == 0 {
		t.Fatal("golden 里没有 mcp，请重新生成基准")
	}
	return dir, g
}

func TestMcpToolsParity(t *testing.T) {
	dir, g := loadMcpGolden(t)
	l := openWriteCopy(t, dir, "knowledge.db")
	ctx := context.Background()
	for i, c := range g.MCP.Tools.Search {
		var in mcpSearchInput
		if q, ok := c.Input["query"].(string); ok {
			in.Query = &q
		}
		if f, ok := c.Input["limit"].(float64); ok {
			in.Limit = &f
		}
		in.Platform, _ = c.Input["platform"].(string)
		in.Collection, _ = c.Input["collection"].(string)
		got, err := l.mcpSearch(ctx, in, c.scope())
		if err != nil {
			t.Errorf("search #%d %v: %v", i, c.Input, err)
			continue
		}
		var want string
		_ = json.Unmarshal(c.Result.Value, &want)
		if got != want {
			t.Errorf("search #%d %v scope=%+v 不一致\nTS: %q\nGo: %q", i, c.Input, c.Scope, want, got)
		}
	}
	for i, c := range g.MCP.Tools.Read {
		id, _ := c.Input["id"].(string)
		full := c.Input["includeFullText"] != false
		text, found, err := l.mcpReadItem(ctx, id, full, c.scope())
		if err != nil {
			t.Errorf("read #%d: %v", i, err)
			continue
		}
		var want struct {
			Text  string `json:"text"`
			Found bool   `json:"found"`
		}
		_ = json.Unmarshal(c.Result.Value, &want)
		if text != want.Text || found != want.Found {
			t.Errorf("read #%d %v scope=%+v 不一致\nTS: %q (%v)\nGo: %q (%v)", i, c.Input, c.Scope, want.Text, want.Found, text, found)
		}
	}
}

func TestAiHandoffParity(t *testing.T) {
	_, g := loadMcpGolden(t)
	for i, c := range g.MCP.Handoff {
		var raw struct {
			Title        string `json:"title"`
			Content      string `json:"content"`
			Transcript   string `json:"transcript"`
			Summary      string `json:"summary"`
			ItemType     string `json:"itemType"`
			SourceURI    string `json:"sourceUri"`
			CollectionNm string `json:"collectionName"`
			Tags         []struct {
				Name string `json:"name"`
			} `json:"tags"`
			CreatedAt int64 `json:"createdAt"`
		}
		if err := json.Unmarshal(c.Item, &raw); err != nil {
			t.Fatal(err)
		}
		item := aiHandoffItem{Title: raw.Title, Content: raw.Content, Transcript: raw.Transcript, Summary: raw.Summary,
			ItemType: raw.ItemType, SourceURI: raw.SourceURI, Collection: raw.CollectionNm, CreatedAtMs: raw.CreatedAt}
		for _, tag := range raw.Tags {
			item.Tags = append(item.Tags, tag.Name)
		}
		got := buildAiHandoff(item, c.IncludeFullText)
		if got.Text != c.Result.Text || got.CharCount != c.Result.CharCount || got.OmittedChars != c.Result.OmittedChars {
			t.Errorf("handoff #%d (full=%v) 不一致\nTS: %q (%d/%d)\nGo: %q (%d/%d)", i, c.IncludeFullText, c.Result.Text, c.Result.CharCount, c.Result.OmittedChars,
				got.Text, got.CharCount, got.OmittedChars)
		}
	}
}

func TestMcpScopeParity(t *testing.T) {
	_, g := loadMcpGolden(t)
	for i, c := range g.MCP.Scopes {
		var raw any = jsUndefined
		if string(c.Raw) != `"__undefined"` {
			v, err := jsParse(string(c.Raw))
			if err != nil {
				t.Fatal(err)
			}
			raw = v
		}
		got := parseMcpScope(raw)
		want := mcpScope{Mode: c.Parsed.Mode, AllowedCollections: c.Parsed.AllowedCollectionIDs, AllowUncategorized: c.Parsed.AllowUncategorized}
		if want.AllowedCollections == nil {
			want.AllowedCollections = []string{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("scope #%d raw=%s 解析不一致\nTS: %+v\nGo: %+v", i, c.Raw, want, got)
		}
		if got.serialize() != c.Text {
			t.Errorf("scope #%d 序列化不一致\nTS: %q\nGo: %q", i, c.Text, got.serialize())
		}
	}
}

func TestMcpInstallParity(t *testing.T) {
	_, g := loadMcpGolden(t)
	for _, c := range g.MCP.Install {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "mcp.json")
			if c.Before != nil {
				if err := os.WriteFile(file, []byte(*c.Before), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			sv, err := jsParse(string(c.Spec))
			if err != nil {
				t.Fatal(err)
			}
			spec := mcpLaunchSpec{Command: jsGet(sv, "command").(string)}
			for _, a := range jsGet(sv, "args").(jsArr) {
				spec.Args = append(spec.Args, a.(string))
			}
			if env, ok := jsGet(sv, "env").(*jsObj); ok {
				for _, k := range env.keys() {
					spec.Env = append(spec.Env, [2]string{k, env.get(k).(string)})
				}
			}
			out, err := installIntoCursorAt(file, spec)
			if c.Result.Error != nil {
				if err == nil {
					t.Fatalf("TS 抛错 %q，Go 返回了 %v", *c.Result.Error, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("Go 抛错: %v", err)
			}
			gotJSON, _ := json.Marshal(out)
			got := map[string]any{}
			_ = json.Unmarshal([]byte(strings.ReplaceAll(string(gotJSON), strings.ReplaceAll(dir, `\`, `\\`), "<DIR>")), &got)
			var want map[string]any
			_ = json.Unmarshal(c.Result.Value, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("返回值不一致\nTS: %v\nGo: %v", want, got)
			}
			read := func(p string) *string {
				data, err := os.ReadFile(p)
				if err != nil {
					return nil
				}
				s := base64.StdEncoding.EncodeToString(data)
				return &s
			}
			if after := read(file); !reflect.DeepEqual(after, c.After) {
				var w, gt string
				if c.After != nil {
					b, _ := base64.StdEncoding.DecodeString(*c.After)
					w = string(b)
				}
				if after != nil {
					b, _ := base64.StdEncoding.DecodeString(*after)
					gt = string(b)
				}
				t.Errorf("写后文件不一致\nTS:\n%q\nGo:\n%q", w, gt)
			}
			if backup := read(file + ".guizhi-backup"); !reflect.DeepEqual(backup, c.Backup) {
				t.Errorf("备份文件不一致")
			}
		})
	}
}

// TestMcpStdioParity：同一批 JSON-RPC 请求喂给 Go server，响应（按 id 对应）必须与真实 TS server 一致。
func TestMcpStdioParity(t *testing.T) {
	dir, g := loadMcpGolden(t)
	st := g.MCP.Stdio
	if len(st.Requests) == 0 {
		t.Skip("golden 无 stdio 数据")
	}
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "data", "knowledge.db"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var in strings.Builder
	for _, r := range st.Requests {
		var s string
		if json.Unmarshal(r, &s) == nil {
			in.WriteString(s) // 字符串条目是原样发送的非法行
		} else {
			var cb bytes.Buffer
			if err := json.Compact(&cb, r); err != nil {
				t.Fatal(err)
			}
			in.Write(cb.Bytes())
		}
		in.WriteString("\n")
	}
	var out strings.Builder
	runMcpServer(context.Background(), strings.NewReader(in.String()), &out, dataDir)

	byID := func(lines []string) map[string]any {
		m := map[string]any{}
		for _, ln := range lines {
			var v map[string]any
			if err := json.Unmarshal([]byte(ln), &v); err != nil {
				t.Fatalf("非 JSON 输出行：%q", ln)
			}
			idb, _ := json.Marshal(v["id"])
			m[string(idb)] = v
		}
		return m
	}
	var wantLines []string
	for _, l := range st.Lines {
		wantLines = append(wantLines, string(l))
	}
	want := byID(wantLines)
	got := byID(strings.Split(strings.TrimRight(out.String(), "\n"), "\n"))
	if len(got) != len(want) {
		t.Errorf("响应条数 Go=%d TS=%d", len(got), len(want))
	}
	bad := 0
	for id, w := range want {
		g := got[id]
		if !reflect.DeepEqual(g, w) {
			bad++
			if bad <= 8 {
				gb, _ := json.Marshal(g)
				wb, _ := json.Marshal(w)
				t.Errorf("id=%s\n  Go: %.700s\n  TS: %.700s", id, gb, wb)
			}
		}
	}
	if bad > 8 {
		t.Errorf("共 %d 条不一致（仅展示前 8）", bad)
	}
}
