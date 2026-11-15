package aiflow

// 全局问答纯函数的 golden 对拍：tryParseAction / extractCitedOrdinals / locateNormalized / extractReadWindow /
// matchWikiPages / resolveBodyLinks。夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-qa-pure.test.ts
// （真实 TS 实现导出）。循环本身见 qa_engine_golden_test.go。

import (
	"sort"
	"testing"
)

// goldenText 展开 golden 里 string | {"$text": 规格} 形式的文本。
func goldenText(t *testing.T, v any) string {
	t.Helper()
	text, ok := expandTexts(t, v, &[]textDigest{}).(string)
	if !ok {
		t.Fatalf("golden 里的文本字段不是字符串：%#v", v)
	}
	return text
}

func describeAction(action agentAction, ok bool) any {
	if !ok {
		return nil
	}
	switch action.kind {
	case actionSearch:
		return map[string]any{"kind": "search", "query": action.query}
	case actionRead:
		return map[string]any{"kind": "read", "target": jsNumberString(action.target)}
	}
	return map[string]any{"kind": "answer", "text": compactText(action.text)}
}

func TestQAParseActionGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name   string `json:"name"`
			Text   any    `json:"text"`
			Output any    `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-parse-action", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			assertSame(t, "tryParseAction", c.Output, describeAction(tryParseAction(goldenText(t, c.Text))))
		})
	}
}

func TestQACitedGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name   string `json:"name"`
			Text   string `json:"text"`
			Valid  []int  `json:"valid"`
			Output []int  `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-cited", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			valid := map[int]bool{}
			for _, ordinal := range c.Valid {
				valid[ordinal] = true
			}
			got := []int{}
			for ordinal := range extractCitedOrdinals(c.Text, valid) {
				got = append(got, ordinal)
			}
			sort.Ints(got)
			assertSame(t, "extractCitedOrdinals", nonNil(c.Output), got)
		})
	}
}

func TestQALocateGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string `json:"name"`
			Haystack any    `json:"haystack"`
			Needle   any    `json:"needle"`
			Output   int    `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-locate", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := locateNormalized(goldenText(t, c.Haystack), goldenText(t, c.Needle)); got != c.Output {
				t.Errorf("locateNormalized = %d，TS 是 %d", got, c.Output)
			}
		})
	}
}

func TestQAWindowGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name   string `json:"name"`
			Text   any    `json:"text"`
			Limit  int    `json:"limit"`
			Hint   any    `json:"hint"`
			Output struct {
				Text   any `json:"text"`
				Length int `json:"length"`
			} `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-window", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			hint := ""
			if c.Hint != nil {
				hint = goldenText(t, c.Hint)
			}
			got := extractReadWindow(goldenText(t, c.Text), c.Limit, hint)
			assertSame(t, "extractReadWindow", map[string]any{"text": c.Output.Text, "length": c.Output.Length},
				map[string]any{"text": compactText(got), "length": u16Len(got)})
		})
	}
}

func entryIDs(entries []WikiCatalogEntry) []string {
	ids := []string{}
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func TestQAMatchWikiGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name    string             `json:"name"`
			Catalog []WikiCatalogEntry `json:"catalog"`
			Query   string             `json:"query"`
			Output  []string           `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-match-wiki", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			assertSame(t, "matchWikiPages", nonNil(c.Output), entryIDs(matchWikiPages(c.Catalog, c.Query)))
		})
	}
}

func TestQABodyLinksGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name    string             `json:"name"`
			Body    string             `json:"body"`
			Catalog []WikiCatalogEntry `json:"catalog"`
			Output  []string           `json:"output"`
		} `json:"cases"`
	}
	loadGolden(t, "qa-body-links", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			assertSame(t, "resolveBodyLinks", nonNil(c.Output), entryIDs(resolveBodyLinks(c.Body, c.Catalog)))
		})
	}
}
