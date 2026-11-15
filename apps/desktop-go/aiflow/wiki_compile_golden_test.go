package aiflow

// Wiki 页面草稿解析与清洗的 golden 对拍：normalizeWikiTitle / parseAliases / parseWikiResponse / sanitizePages /
// buildLinkResolver + cleanWikiLinks / stripDuplicateTitleHeading / evidenceUrl。
// 夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-wiki-compile.test.ts（真实 TS 实现导出）。

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// expandTexts 遍历 golden 里的 JSON 值，把 {"$text": 规格} 展开成文本，并按遍历序（数组按下标、对象按键序）记指纹。
func expandTexts(t *testing.T, v any, digests *[]textDigest) any {
	t.Helper()
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, entry := range x {
			out[i] = expandTexts(t, entry, digests)
		}
		return out
	case map[string]any:
		if spec, ok := x["$text"]; ok && len(x) == 1 {
			raw, err := json.Marshal(spec)
			if err != nil {
				t.Fatalf("文本规格无法序列化：%v", err)
			}
			var text textSpec
			if err := json.Unmarshal(raw, &text); err != nil {
				t.Fatalf("文本规格无法解析：%v", err)
			}
			*digests = append(*digests, digestOf(text.String()))
			return text.String()
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(x))
		for _, key := range keys {
			out[key] = expandTexts(t, x[key], digests)
		}
		return out
	}
	return v
}

func compactDraft(d WikiPageDraft) map[string]any {
	var aliases any
	if d.AliasesJSON != nil {
		aliases = compactText(*d.AliasesJSON)
	}
	return map[string]any{
		"title":           compactText(d.Title),
		"normalizedTitle": compactText(d.NormalizedTitle),
		"kind":            string(d.Kind),
		"summary":         compactText(d.Summary),
		"body":            compactText(d.Body),
		"aliasesJson":     aliases,
	}
}

func TestWikiParseGolden(t *testing.T) {
	var golden struct {
		Version   string `json:"version"`
		Normalize []struct {
			Input  string `json:"input"`
			Output string `json:"output"`
		} `json:"normalize"`
		Aliases []struct {
			Input  *string  `json:"input"`
			Output []string `json:"output"`
		} `json:"aliases"`
		Parse []struct {
			Name   string `json:"name"`
			Text   string `json:"text"`
			Output any    `json:"output"`
		} `json:"parse"`
		KindLabels map[string]string `json:"kindLabels"`
	}
	loadGolden(t, "wiki-parse", &golden)

	if WikiCompilerVersion != golden.Version {
		t.Errorf("编译器版本不一致：TS %q，Go %q", golden.Version, WikiCompilerVersion)
	}
	for _, c := range golden.Normalize {
		if got := NormalizeWikiTitle(c.Input); got != c.Output {
			t.Errorf("NormalizeWikiTitle(%q) = %q，TS 是 %q", c.Input, got, c.Output)
		}
	}
	for _, c := range golden.Aliases {
		label := "<null>"
		if c.Input != nil {
			label = *c.Input
		}
		assertSame(t, "ParseAliases("+label+")", c.Output, nonNil(ParseAliases(c.Input)))
	}
	for _, c := range golden.Parse {
		var got any
		if pages, ok := ParseWikiResponse(c.Text); ok {
			if pages == nil {
				pages = []any{}
			}
			got = pages
		}
		assertSame(t, "ParseWikiResponse："+c.Name, c.Output, got)
	}
	got := map[string]string{
		"topic":   WikiKindLabel(WikiKindTopic),
		"entity":  WikiKindLabel(WikiKindEntity),
		"concept": WikiKindLabel(WikiKindConcept),
	}
	assertSame(t, "WikiKindLabel", golden.KindLabels, got)
}

func TestWikiSanitizeGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name         string         `json:"name"`
			Pages        []any          `json:"pages"`
			InputDigests []textDigest   `json:"inputDigests"`
			Output       map[string]any `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "wiki-sanitize", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			digests := []textDigest{}
			pages, _ := expandTexts(t, c.Pages, &digests).([]any)
			if !reflect.DeepEqual(digests, nonNil(c.InputDigests)) {
				t.Fatalf("重建的输入与 TS 的指纹不一致（生成器有差异）\nTS：%+v\nGo：%+v", c.InputDigests, digests)
			}
			drafts, err := SanitizePages(pages)
			var got map[string]any
			if err != nil {
				got = map[string]any{"error": err.Error()}
			} else {
				list := make([]map[string]any, 0, len(drafts))
				for _, d := range drafts {
					list = append(list, compactDraft(d))
				}
				got = map[string]any{"drafts": list}
			}
			assertSame(t, "SanitizePages", c.Output, got)
		})
	}
}

func TestWikiLinksGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string             `json:"name"`
			Catalog  []WikiCatalogEntry `json:"catalog"`
			Batch    []WikiPageDraft    `json:"batch"`
			Bodies   []string           `json:"bodies"`
			Resolver [][2]string        `json:"resolver"`
			Outputs  []struct {
				Body    string   `json:"body"`
				Targets []string `json:"targets"`
			} `json:"outputs"`
		} `json:"cases"`
	}
	loadGolden(t, "wiki-links", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			resolver := BuildLinkResolver(c.Catalog, c.Batch)
			pairs := make([][2]string, 0, len(resolver))
			for key, value := range resolver {
				pairs = append(pairs, [2]string{key, value})
			}
			// 与 TS 同序：按键排序（这些键都在 BMP 内，字节序与 UTF-16 码元序一致）
			sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
			assertSame(t, "BuildLinkResolver", c.Resolver, pairs)

			if len(c.Bodies) != len(c.Outputs) {
				t.Fatalf("golden 的正文与输出数量不一致：%d / %d", len(c.Bodies), len(c.Outputs))
			}
			for i, body := range c.Bodies {
				cleaned, targets := CleanWikiLinks(body, resolver)
				assertSame(t, "CleanWikiLinks："+body, c.Outputs[i], map[string]any{"body": cleaned, "targets": targets})
			}
		})
	}
}

func TestWikiStripGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name   string     `json:"name"`
			Body   textSpec   `json:"body"`
			Title  string     `json:"title"`
			Input  textDigest `json:"input"`
			Output any        `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "wiki-strip", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			checkInput(t, c.Body.String(), c.Input)
			assertSame(t, "StripDuplicateTitleHeading", c.Output, compactText(StripDuplicateTitleHeading(c.Body.String(), c.Title)))
		})
	}
}

func TestWikiEvidenceGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Input  string  `json:"input"`
			Output *string `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "wiki-evidence", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		var got *string
		if value, ok := EvidenceURL(c.Input); ok {
			got = &value
		}
		assertSame(t, "EvidenceURL("+c.Input+")", c.Output, got)
	}
}
