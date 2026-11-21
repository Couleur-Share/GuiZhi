package notes

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// reading-source-golden.json 由渲染层单测导出（apps/desktop/tests/unit/import/reading-source-golden.test.ts，
// UPDATE_GOLDEN=1 重新导出）：真实的 TS replaceReadingSource / readingSourceText 跑固定语料的结果。
// 这里逐条重放，要求取文与改写的结果（或错误文案）逐字节一致——改写结果直接写进条目正文，不能有「差不多」。

type goldenOutcome struct {
	Text  *string `json:"text"`
	Error *string `json:"error"`
}

type readingGoldenCase struct {
	ItemType    string        `json:"itemType"`
	Kind        string        `json:"kind"`
	Content     string        `json:"content"`
	Replacement string        `json:"replacement"`
	Source      goldenOutcome `json:"source"`
	Result      goldenOutcome `json:"result"`
}

func shapeForType(itemType string) Shape {
	switch itemType {
	case "forum":
		return ShapeForum
	case "image":
		return ShapeImage
	}
	return ShapePlain
}

// diffOutcome 核对一次调用与 golden 的差别；一致返回空串。
func diffOutcome(want goldenOutcome, got string, err error) string {
	switch {
	case want.Error != nil && err == nil:
		return fmt.Sprintf("TS 报错 %q，Go 没报错（结果 %q）", *want.Error, got)
	case want.Error != nil && err.Error() != *want.Error:
		return fmt.Sprintf("错误文案不同：TS %q，Go %q", *want.Error, err.Error())
	case want.Error == nil && err != nil:
		return fmt.Sprintf("Go 报错 %q，TS 没报错", err.Error())
	case want.Error == nil && want.Text != nil && got != *want.Text:
		return fmt.Sprintf("结果不同：\n    TS %q\n    Go %q", *want.Text, got)
	}
	return ""
}

func TestReadingSourceMatchesTheTypeScriptGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/reading-source-golden.json")
	if err != nil {
		t.Fatalf("读取 golden 失败（由渲染层单测 UPDATE_GOLDEN=1 导出）：%v", err)
	}
	var golden struct {
		Cases []readingGoldenCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("golden 不是合法 JSON：%v", err)
	}
	if len(golden.Cases) < 500 {
		t.Fatalf("golden 语料过少：%d", len(golden.Cases))
	}
	var bad []string
	changed, failed := 0, 0
	for i, c := range golden.Cases {
		shape := shapeForType(c.ItemType)
		text, err := ReadingSourceText(c.Content, shape, c.Kind)
		if d := diffOutcome(c.Source, text, err); d != "" {
			bad = append(bad, fmt.Sprintf("#%d 取文（%s/%s）%s\n    内容 %q", i, c.ItemType, c.Kind, d, c.Content))
		}
		got, err := ReplaceReadingSource(c.Content, shape, c.Kind, c.Replacement)
		if d := diffOutcome(c.Result, got, err); d != "" {
			bad = append(bad, fmt.Sprintf("#%d 改写（%s/%s，替换 %q）%s\n    内容 %q", i, c.ItemType, c.Kind, c.Replacement, d, c.Content))
		}
		switch {
		case c.Result.Error != nil:
			failed++
		case c.Result.Text != nil && *c.Result.Text != c.Content:
			changed++
		}
	}
	if len(bad) > 0 {
		shown := bad
		if len(shown) > 8 {
			shown = shown[:8]
		}
		t.Fatalf("%d 项与 TS 不一致（只列前 8 项）：\n%s", len(bad), strings.Join(shown, "\n"))
	}
	if changed < 300 || failed < 5 {
		t.Errorf("语料里真实改写 %d、报错 %d 过少，golden 可能被截断或换了口径", changed, failed)
	}
}
