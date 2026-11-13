package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"guizhi-mygo-shell/extract"
)

type webGolden struct {
	Web struct {
		Scopes []struct {
			Entry string  `json:"entry"`
			Dir   *string `json:"dir"`
			OK    bool    `json:"ok"`
			V     any     `json:"v"`
		} `json:"scopes"`
		InScope []struct {
			U string `json:"u"`
			R bool   `json:"r"`
		} `json:"inScope"`
		PageLinks []struct {
			U string `json:"u"`
			R bool   `json:"r"`
		} `json:"pageLinks"`
		Pages []struct {
			Name   string `json:"name"`
			HTML   string `json:"html"`
			URL    string `json:"url"`
			Status int    `json:"status"`
		} `json:"pages"`
		Extract []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
			Result struct {
				Title          string           `json:"title"`
				Author         string           `json:"author"`
				PublishedAt    *int64           `json:"publishedAt"`
				DateConfidence string           `json:"dateConfidence"`
				Markdown       string           `json:"markdown"`
				Paragraphs     []webParagraph   `json:"paragraphs"`
				ContentHash    string           `json:"contentHash"`
				Complete       bool             `json:"complete"`
				Truncated      bool             `json:"truncated"`
				Warnings       []string         `json:"warnings"`
				Error          *webCaptureError `json:"error"`
			} `json:"result"`
		} `json:"extract"`
		StaticDecisions []struct {
			Name string                 `json:"name"`
			D    extract.StaticDecision `json:"d"`
		} `json:"staticDecisions"`
		Verification []struct {
			Name string `json:"name"`
			V    bool   `json:"v"`
		} `json:"verification"`
		LinkPages []struct {
			HTML string `json:"html"`
			URL  string `json:"url"`
		} `json:"linkPages"`
		Links []struct {
			Links []string `json:"links"`
		} `json:"links"`
		Errors []struct {
			Text string          `json:"text"`
			Code *string         `json:"code"`
			R    webCaptureError `json:"r"`
		} `json:"errors"`
		Canonical []struct {
			U  string `json:"u"`
			OK bool   `json:"ok"`
			V  string `json:"v"`
		} `json:"canonical"`
	} `json:"web"`
}

func loadWebGolden(t *testing.T) webGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g webGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Web.Extract) == 0 {
		t.Fatal("golden 缺少 web 段")
	}
	return g
}

func TestWebExtractResultMatchesTS(t *testing.T) {
	g := loadWebGolden(t)
	for i, want := range g.Web.Extract {
		p := g.Web.Pages[i]
		art := extract.ExtractWebArticle(p.HTML, p.URL, p.Status)
		got := webExtractedFrom(art, p.URL)
		if art.Source != want.Source {
			t.Errorf("%s: source %q != %q", want.Name, art.Source, want.Source)
		}
		w := want.Result
		if got.Title != w.Title || got.Author != w.Author || got.DateConfidence != w.DateConfidence ||
			got.Complete != w.Complete || got.Truncated != w.Truncated || got.ContentHash != w.ContentHash {
			t.Errorf("%s: 字段不一致\n got title=%q author=%q conf=%s complete=%v trunc=%v hash=%s\nwant title=%q author=%q conf=%s complete=%v trunc=%v hash=%s",
				want.Name, got.Title, got.Author, got.DateConfidence, got.Complete, got.Truncated, got.ContentHash,
				w.Title, w.Author, w.DateConfidence, w.Complete, w.Truncated, w.ContentHash)
		}
		if !reflect.DeepEqual(got.PublishedAt, w.PublishedAt) {
			t.Errorf("%s: publishedAt %v != %v", want.Name, derefInt(got.PublishedAt), derefInt(w.PublishedAt))
		}
		if got.Markdown != w.Markdown {
			t.Errorf("%s: markdown 不一致\n got %q\nwant %q", want.Name, truncStr(got.Markdown), truncStr(w.Markdown))
		}
		if !reflect.DeepEqual(got.Paragraphs, w.Paragraphs) && !(len(got.Paragraphs) == 0 && len(w.Paragraphs) == 0) {
			t.Errorf("%s: paragraphs 不一致", want.Name)
		}
		if !reflect.DeepEqual(got.Warnings, w.Warnings) {
			t.Errorf("%s: warnings %v != %v", want.Name, got.Warnings, w.Warnings)
		}
		if !reflect.DeepEqual(got.Error, w.Error) {
			t.Errorf("%s: error %+v != %+v", want.Name, got.Error, w.Error)
		}
	}
}

func derefInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func truncStr(s string) string {
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

func TestStaticRouteMatchesTS(t *testing.T) {
	g := loadWebGolden(t)
	for i, want := range g.Web.StaticDecisions {
		p := g.Web.Pages[i]
		got := extract.AssessStaticPage(p.HTML, p.Status)
		if !reflect.DeepEqual(got, want.D) {
			t.Errorf("%s: 静态判定不一致\n got %+v\nwant %+v", want.Name, got, want.D)
		}
	}
	extra := map[string]string{
		"cf-wrapper": `<html><body><div id="cf-wrapper"></div></body></html>`,
		"chl-script": `<html><body><script src="/cdn-cgi/challenge-platform/orchestrate/chl_page/v1"></script></body></html>`,
		"zh-title":   `<html><head><title>安全验证 - x</title></head><body></body></html>`,
	}
	for i, want := range g.Web.Verification {
		html, ok := extra[want.Name]
		if !ok {
			html = g.Web.Pages[i].HTML
		}
		if got := extract.IsWebVerificationPage(html); got != want.V {
			t.Errorf("%s: 验证页判定 %v != %v", want.Name, got, want.V)
		}
	}
	for i, want := range g.Web.Links {
		in := g.Web.LinkPages[i]
		got := extract.StaticPageLinks(in.HTML, in.URL)
		if len(got) == 0 && len(want.Links) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want.Links) {
			t.Errorf("链接 #%d 不一致\n got %v\nwant %v", i, got, want.Links)
		}
	}
}

func TestWebCaptureErrorMatchesTS(t *testing.T) {
	g := loadWebGolden(t)
	for _, want := range g.Web.Errors {
		code := ""
		if want.Code != nil {
			code = *want.Code
		}
		got := classifyWebError(want.Text, code)
		if got != want.R {
			t.Errorf("%q (code=%q)\n got %+v\nwant %+v", truncStr(want.Text), code, got, want.R)
		}
	}
}

func TestCanonicalWebURLMatchesTS(t *testing.T) {
	g := loadWebGolden(t)
	for _, want := range g.Web.Canonical {
		got, err := canonicalWebURL(want.U)
		if want.OK {
			if err != nil || got != want.V {
				t.Errorf("%q: got %q err=%v want %q", want.U, got, err, want.V)
			}
		} else if err == nil || err.Error() != want.V {
			t.Errorf("%q: 应报错 %q，实际 %q err=%v", want.U, want.V, got, err)
		}
	}
}

func TestWebScopeMatchesTS(t *testing.T) {
	g := loadWebGolden(t)
	if len(g.Web.Scopes) == 0 {
		t.Fatal("golden 缺少 scope 用例")
	}
	for _, want := range g.Web.Scopes {
		got, err := newWebScope(want.Entry, want.Dir)
		if want.OK {
			v, _ := want.V.(map[string]any)
			if err != nil || got.Origin != v["origin"] || got.Directory != v["directory"] {
				t.Errorf("%q dir=%v: got %+v err=%v want %v", want.Entry, want.Dir, got, err, want.V)
			}
		} else if err == nil || err.Error() != want.V {
			t.Errorf("%q dir=%v: 应报错 %v，实际 %+v err=%v", want.Entry, want.Dir, want.V, got, err)
		}
	}
	scope := webScope{Origin: "https://example.com", Directory: "/docs/guide/"}
	for _, want := range g.Web.InScope {
		if got := inWebScope(want.U, scope); got != want.R {
			t.Errorf("inWebScope(%q)=%v want %v", want.U, got, want.R)
		}
	}
	for _, want := range g.Web.PageLinks {
		if got := isWebPageLink(want.U); got != want.R {
			t.Errorf("isWebPageLink(%q)=%v want %v", want.U, got, want.R)
		}
	}
}
