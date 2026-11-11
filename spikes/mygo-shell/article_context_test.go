package main

// 本文问答差分：对拍 golden-video.mts 里 article-cases.mts 录制的结果
// （真实 article-context.ts / reading-source.ts / article-ask-context.ts / native-search*.ts）。

import (
	"context"
	"encoding/json"
	"testing"
)

type artSeedItem struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	Transcript *string `json:"transcript"`
	ItemType   string  `json:"itemType"`
	DeletedAt  *int64  `json:"deletedAt"`
}

func artErr(err error) any { return map[string]any{"error": err.Error()} }

func articleGolden(t *testing.T, r forumRec, want any) bool {
	t.Helper()
	switch r.Kind {
	case "artNorm":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, normalizeArticleText(in))
	case "artSelect":
		var in struct {
			Text, Question, Selection string
			Budget                    int
		}
		mustJSON(t, r.Input, &in)
		s := selectArticleContext(in.Text, in.Question, in.Selection, in.Budget)
		fgCompare(t, want, map[string]any{"text": s.Text, "clipped": s.Clipped})
	case "artImgSections":
		var in string
		mustJSON(t, r.Input, &in)
		s := splitImageNoteSections(in)
		fgCompare(t, want, map[string]any{"caption": s.Caption, "recognized": s.Recognized})
	case "artReadingSource":
		var in struct{ Content, ItemType, Kind string }
		mustJSON(t, r.Input, &in)
		text, err := readingSourceText(in.Content, in.ItemType, in.Kind)
		if err != nil {
			fgCompare(t, want, artErr(err))
		} else {
			fgCompare(t, want, map[string]any{"text": text})
		}
	case "artTarget":
		var raw any
		if string(r.Input) != `"@@undef@@"` {
			v, err := jsParse(string(r.Input))
			if err != nil {
				t.Fatal(err)
			}
			raw = v
		} else {
			raw = jsUndefined
		}
		tg, err := parseArticleTarget(raw)
		if err != nil {
			fgCompare(t, want, artErr(err))
		} else {
			fgCompare(t, want, map[string]any{"ok": tg})
		}
	case "artContext":
		var in struct {
			Items []artSeedItem   `json:"items"`
			Input json.RawMessage `json:"input"`
		}
		mustJSON(t, r.Input, &in)
		_, l := loadGolden(t)
		ctx := context.Background()
		for _, it := range in.Items {
			if _, err := l.db.Execute(ctx, "INSERT INTO knowledge_items (id,title,content,transcript,item_type,deleted_at,created_at,updated_at) VALUES (?,?,?,?,?,?,1,1)",
				it.ID, it.Title, it.Content, artNilStr(it.Transcript), it.ItemType, artNilInt(it.DeletedAt)); err != nil {
				t.Fatal(err)
			}
		}
		input, err := jsParse(string(in.Input))
		if err != nil {
			t.Fatal(err)
		}
		out, err := l.articleContext(ctx, input)
		if err != nil {
			fgCompare(t, want, artErr(err))
		} else {
			fgCompare(t, want, map[string]any{"ok": out})
		}
	case "artPublicUrl":
		v, err := jsParse(string(r.Input))
		if err != nil {
			t.Fatal(err)
		}
		_, ok := isPublicSearchURL(v)
		fgCompare(t, want, ok)
	case "artNativeDecode":
		var in string
		mustJSON(t, r.Input, &in)
		resp, err := decodeNativeSearchResponse(in)
		if err != nil {
			fgCompare(t, want, artErr(err))
		} else {
			fgCompare(t, want, map[string]any{"response": jsPlain(resp)})
		}
	case "artNativeResults":
		var in string
		mustJSON(t, r.Input, &in)
		v, err := jsParse(in)
		if err != nil {
			t.Fatal(err)
		}
		res, err := nativeSearchResults(v)
		if err != nil {
			fgCompare(t, want, artErr(err))
		} else {
			fgCompare(t, want, map[string]any{"results": res})
		}
	case "artNativeEndpoint":
		var in struct {
			Provider    string `json:"provider"`
			APIProtocol string `json:"apiProtocol"`
			APIURL      string `json:"apiUrl"`
			APIKey      string `json:"apiKey"`
			Model       string `json:"model"`
		}
		mustJSON(t, r.Input, &in)
		ep, err := nativeSearchEndpoint(aiClientConfig{Provider: in.Provider, APIProtocol: in.APIProtocol, APIURL: in.APIURL, APIKey: in.APIKey, Model: in.Model})
		if err != nil {
			fgCompare(t, want, artErr(err))
		} else {
			fgCompare(t, want, map[string]any{"endpoint": ep})
		}
	default:
		return false
	}
	return true
}

func artNilStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func artNilInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// jsPlain 把 jsvalue 序列化再解析成通用结构，便于与 TS 基线比较。
func jsPlain(v any) any {
	s, _ := jsStringify(v)
	var out any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
