package main

// 主题阅读渲染层差分：对拍 golden-themed.mts 录制的结果（V8 里的 TS 原文件）与 goja 里的同一份 TS 打包产物。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThemedEngineGolden(t *testing.T) {
	dir := os.Getenv("GUIZHI_THEMED_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs []forumRec `json:"recs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Recs) < 100 {
		t.Fatalf("基线过少：%d", len(g.Recs))
	}
	for _, r := range g.Recs {
		r := r
		t.Run(r.Kind+"/"+r.Name, func(t *testing.T) {
			want := scrubSyntaxMessages(fgDecode(t, r.Output))
			fgCompare(t, want, scrubSyntaxMessages(toGeneric(t, themedGoldenRun(t, r))))
		})
	}
}

// jsonArg 把输入里的子字段原样转成 JSON 字符串交给 JS（对象不经 Go 结构，避免键序与数值形态改变）。
func jsonArg(raw json.RawMessage) string { return string(raw) }

// themedWrap 把「JSON 字符串或错误」转成 TS 基线里的 {ok}|{error} 形态。
func themedWrap(s string, err error) any {
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"ok": s}
}

// themedWrapJSON 同上，但引擎返回的是已经带 ok/issue 信封的 JSON。
func themedWrapJSON(t *testing.T, s string, err error) any {
	t.Helper()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var v any
	if e := json.Unmarshal([]byte(s), &v); e != nil {
		t.Fatalf("引擎返回的不是 JSON：%v\n%s", e, s)
	}
	return v
}

func themedGoldenRun(t *testing.T, r forumRec) any {
	t.Helper()
	switch r.Kind {
	case "thmBlocks":
		var in string
		mustJSON(t, r.Input, &in)
		s, err := themedCall("renderBlocks", in)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		var blocks any
		if e := json.Unmarshal([]byte(s), &blocks); e != nil {
			t.Fatal(e)
		}
		return map[string]any{"ok": blocks}
	case "thmDoc":
		var in struct {
			Version    json.RawMessage `json:"version"`
			InstanceID *string         `json:"instanceId"`
			AssetURLs  json.RawMessage `json:"assetUrls"`
		}
		mustJSON(t, r.Input, &in)
		var inst, urls any
		if in.InstanceID != nil {
			inst = *in.InstanceID
		}
		if string(in.AssetURLs) != "null" {
			urls = string(in.AssetURLs)
		}
		return themedWrap(themedCall("document", jsonArg(in.Version), inst, urls))
	case "thmV3Validate":
		var in struct {
			Version json.RawMessage `json:"version"`
			Partial bool            `json:"partial"`
		}
		mustJSON(t, r.Input, &in)
		s, err := themedCall("validateV3", jsonArg(in.Version), in.Partial)
		return themedWrapJSON(t, s, err)
	case "thmV3Inner":
		var in struct {
			Version     json.RawMessage `json:"version"`
			Assets      json.RawMessage `json:"assets"`
			Interactive bool            `json:"interactive"`
			Partial     bool            `json:"partial"`
			Host        bool            `json:"host"`
		}
		mustJSON(t, r.Input, &in)
		s, err := themedCall("v3Inner", jsonArg(in.Version), jsonArg(in.Assets), in.Interactive, in.Partial, in.Host)
		return themedWrapJSON(t, s, err)
	case "thmExport":
		var in struct {
			Version    json.RawMessage `json:"version"`
			URLs       json.RawMessage `json:"urls"`
			StaticOnly bool            `json:"staticOnly"`
		}
		mustJSON(t, r.Input, &in)
		return themedWrap(themedCall("exportDocument", jsonArg(in.Version), jsonArg(in.URLs), in.StaticOnly))
	case "thmV3Section":
		var in struct {
			HTML     string `json:"html"`
			Unit     string `json:"unit"`
			Enhanced bool   `json:"enhanced"`
		}
		mustJSON(t, r.Input, &in)
		s, err := themedCall("normalizeSection", in.HTML, in.Unit, in.Enhanced)
		return themedWrapJSON(t, s, err)
	case "thmV3Clean":
		var in struct {
			HTML   string          `json:"html"`
			Assets json.RawMessage `json:"assets"`
		}
		mustJSON(t, r.Input, &in)
		return themedWrap(themedCall("cleanV3Html", in.HTML, jsonArg(in.Assets)))
	case "thmV3Script":
		s, err := themedCall("checkScript", string(r.Input))
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		var issue map[string]any
		_ = json.Unmarshal([]byte(s), &issue)
		return map[string]any{"ok": issue}
	}
	t.Fatalf("未知基线类型 %s", r.Kind)
	return nil
}

// scrubSyntaxMessages 抹掉脚本语法错误里的引擎原文：goja 与 V8 的措辞不同（已知差异），
// 其余字段（种类、单元、行号、“交互 xx 语法错误”前缀）仍逐项比对。
func scrubSyntaxMessages(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && k == "message" {
				if i := strings.Index(s, "语法错误："); i >= 0 {
					x[k] = s[:i+len("语法错误：")]
				}
				continue
			}
			x[k] = scrubSyntaxMessages(e)
		}
	case []any:
		for i := range x {
			x[i] = scrubSyntaxMessages(x[i])
		}
	}
	return v
}
