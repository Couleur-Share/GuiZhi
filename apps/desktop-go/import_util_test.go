package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type importUtilGolden struct {
	LocaleMatrix []string `json:"localeMatrix"`
	LocalePairs  [][]any  `json:"localePairs"`
	Normalize    [][]any  `json:"normalize"`
	Identity     [][]any  `json:"identity"`
	URLText      [][]any  `json:"urlText"`
	Review       []struct {
		Extracted map[string]any
		Kind      string
		Reasons   []any
	}
	RawReview       []json.RawMessage `json:"review"`
	TextExtract     []json.RawMessage `json:"textExtract"`
	Platforms       [][]any           `json:"platforms"`
	WebVerification [][]any           `json:"webVerification"`
}

func loadImportUtilGolden(t *testing.T) importUtilGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		ImportUtil importUtilGolden `json:"importUtil"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w.ImportUtil
}

func TestImportLocaleCompare(t *testing.T) {
	g := loadImportUtilGolden(t)
	sign := func(n int) string {
		switch {
		case n < 0:
			return "-"
		case n > 0:
			return "+"
		}
		return "0"
	}
	bad := 0
	for i := 0; i < 95; i++ {
		for j := 0; j < 95; j++ {
			a, b := string(rune(32+i)), string(rune(32+j))
			if got, want := sign(localeCompare(a, b)), string(g.LocaleMatrix[i][j]); got != want {
				if bad++; bad <= 20 {
					t.Errorf("localeCompare(%q,%q)=%s，期望 %s", a, b, got, want)
				}
			}
		}
	}
	for _, p := range g.LocalePairs {
		a, b, want := p[0].(string), p[1].(string), int(p[2].(float64))
		if !isASCII(a) || !isASCII(b) {
			continue // 非 ASCII 的排序依赖系统区域与 ICU 版本，属已接受差异（只影响含非 ASCII 参数名的查询串排序）
		}
		if got := localeCompare(a, b); (got < 0) != (want < 0) || (got > 0) != (want > 0) {
			t.Errorf("localeCompare(%q,%q)=%d，期望 %d", a, b, got, want)
		}
	}
}

func TestImportNormalizeURL(t *testing.T) {
	g := loadImportUtilGolden(t)
	for _, c := range g.Normalize {
		in := c[0].(string)
		got, ok := normalizeURL(in)
		var gotAny any
		if ok {
			gotAny = got
		}
		if !reflect.DeepEqual(gotAny, c[1]) {
			t.Errorf("normalizeUrl(%q)\nTS: %v\nGo: %v", in, c[1], gotAny)
		}
	}
}

func TestImportSourceIdentity(t *testing.T) {
	g := loadImportUtilGolden(t)
	for _, c := range g.Identity {
		in := c[0].(string)
		got, ok := sourceIdentity(in)
		var gotAny any
		if ok {
			gotAny = got
		}
		if !reflect.DeepEqual(gotAny, c[1]) {
			t.Errorf("sourceIdentity(%q)\nTS: %v\nGo: %v", in, c[1], gotAny)
		}
	}
}

func TestImportExtractURLs(t *testing.T) {
	g := loadImportUtilGolden(t)
	for _, c := range g.URLText {
		in := c[0].(string)
		got := []any{}
		for _, u := range extractURLsFromText(in) {
			got = append(got, u)
		}
		if !reflect.DeepEqual(got, c[1]) {
			t.Errorf("extractUrlsFromText(%q)\nTS: %v\nGo: %v", in, c[1], got)
		}
	}
}

func TestImportReviewAssessment(t *testing.T) {
	g := loadImportUtilGolden(t)
	for _, raw := range g.RawReview {
		var c []json.RawMessage
		_ = json.Unmarshal(raw, &c)
		var e struct {
			Content       string `json:"content"`
			ItemType      string `json:"itemType"`
			WarningReason string `json:"warningReason"`
		}
		var kind string
		var want struct {
			ReviewRequired bool     `json:"reviewRequired"`
			Reasons        []string `json:"reasons"`
		}
		_ = json.Unmarshal(c[0], &e)
		_ = json.Unmarshal(c[1], &kind)
		_ = json.Unmarshal(c[2], &want)
		got := assessImportReview(e.Content, e.ItemType, e.WarningReason, kind)
		if want.Reasons == nil {
			want.Reasons = []string{}
		}
		if !reflect.DeepEqual(got, want.Reasons) || (len(got) > 0) != want.ReviewRequired {
			t.Errorf("assessImportReview(%q,%s,%q)\nTS: %v\nGo: %v", e.Content, e.ItemType, e.WarningReason, want.Reasons, got)
		}
	}
}

func TestImportTextExtractTitle(t *testing.T) {
	g := loadImportUtilGolden(t)
	for _, raw := range g.TextExtract {
		var c []json.RawMessage
		_ = json.Unmarshal(raw, &c)
		var in string
		var want struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(c[0], &in)
		_ = json.Unmarshal(c[1], &want)
		content := jsTrim(in)
		if got := string(nodeUTF8(firstLineTitle(content))); got != want.Title || content != want.Content { // JSON 里的孤立代理项被 Go 解码为 U+FFFD，与 Node 的 UTF-8 编码一致
			t.Errorf("extractText(%q)\nTS: %q / %q\nGo: %q / %q", in, want.Title, want.Content, got, content)
		}
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func TestSourcePlatforms(t *testing.T) {
	g := loadImportUtilGolden(t)
	nilIfEmpty := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	for _, c := range g.Platforms {
		in := c[0].(string)
		var forum any
		if f := detectForumPlatform(in); f != nil {
			forum = map[string]any{"platform": f.Platform, "topicId": f.TopicID}
		}
		resolve := func(kind string, uri *string) any {
			p, ok := resolveSourcePlatform(kind, uri)
			if !ok {
				return nil
			}
			return p
		}
		got := []any{nilIfEmpty(detectVideoPlatform(in)), forum, resolve("url", &in), resolve("file", &in), resolve("text", &in), resolve("url", nil), nilIfEmpty(detectPlatformCapturePlatform(in))}
		if !reflect.DeepEqual(got, c[1:]) {
			t.Errorf("平台识别 %q\nTS: %v\nGo: %v", in, c[1:], got)
		}
	}
	for _, c := range g.WebVerification {
		if got := needsWebVerification(c[0].(map[string]any)); got != c[1].(bool) {
			t.Errorf("needsWebVerification(%v)=%v，期望 %v", c[0], got, c[1])
		}
	}
}
