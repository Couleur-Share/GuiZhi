package main

// 图文条目与 OCR 请求差分：对拍 golden-video.mts 里 imagenote-cases.mts 录制的结果（真实 image-note-entry.ts / ocr-request.ts）。

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var reIdxInName = regexp.MustCompile(`(\d+)`)

func idxOfName(p string) int {
	n, _ := strconv.Atoi(reIdxInName.FindString(filepath.Base(p)))
	return n
}

// imgnoteGolden 处理图文 / OCR 相关的基线记录；不属于本组的返回 false。
func imgnoteGolden(t *testing.T, r forumRec, want any) bool {
	t.Helper()
	switch r.Kind {
	case "imgnotePlain":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, plainTextToMarkdown(in))
	case "imgnoteMeta":
		var in struct {
			imgnoteSrcJSON
		}
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, buildImageNoteMetaLine(in.toSource()))
	case "ocrMime":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, imageMimeFromFileName(in))
	case "ocrBody":
		var in struct{ Model, URL, Protocol string }
		mustJSON(t, r.Input, &in)
		var raw struct {
			Model    string `json:"model"`
			URL      string `json:"url"`
			Protocol string `json:"protocol"`
		}
		mustJSON(t, r.Input, &raw)
		body, err := buildOcrRequestBody(raw.Model, raw.URL, raw.Protocol)
		if err != nil {
			t.Fatal(err)
		}
		fgCompare(t, want, body)
	case "ocrParse":
		var in struct {
			Protocol string `json:"protocol"`
			Body     string `json:"body"`
		}
		mustJSON(t, r.Input, &in)
		text, err := parseOcrResponse(in.Body, in.Protocol)
		if err != nil {
			fgCompare(t, want, map[string]any{"error": err.Error()})
			return true
		}
		fgCompare(t, want, map[string]any{"ok": text})
	case "imgnoteEntry":
		imgnoteEntryCase(t, r, want)
	default:
		return false
	}
	return true
}

type imgnoteSrcJSON struct {
	PlatformLabel string     `json:"platformLabel"`
	Title         string     `json:"title"`
	AuthoredTitle bool       `json:"authoredTitle"`
	Description   string     `json:"description"`
	Author        string     `json:"author"`
	ImageMirrors  [][]string `json:"imageMirrors"`
	WebpageURL    string     `json:"webpageUrl"`
}

func (s imgnoteSrcJSON) toSource() imageNoteSource {
	return imageNoteSource{PlatformLabel: s.PlatformLabel, Title: s.Title, AuthoredTitle: s.AuthoredTitle, Description: s.Description,
		Author: s.Author, ImageMirrors: s.ImageMirrors, WebpageURL: s.WebpageURL}
}

func imgnoteEntryCase(t *testing.T, r forumRec, want any) {
	var sc struct {
		Source       imgnoteSrcJSON    `json:"source"`
		DownloadFail map[string]string `json:"downloadFail"`
		SaveFail     map[string]string `json:"saveFail"`
		Ocr          *struct {
			Texts []json.RawMessage `json:"texts"`
		} `json:"ocr"`
		TitleCfg bool            `json:"titleCfg"`
		GenTitle json.RawMessage `json:"genTitle"`
	}
	mustJSON(t, r.Input, &sc)

	lib := &Library{paths: Paths{UserData: t.TempDir()}}
	stages, downloadCalls, saveCalls, recognizeCalls, titleCalls := []any{}, []any{}, []any{}, []any{}, []any{}
	dl := 0
	deps := imageNoteDeps{
		onStage: func(s string) { stages = append(stages, s) },
		downloadImage: func(ctx context.Context, m []string) (string, string, error) {
			i := dl
			dl++
			ms := []any{}
			for _, x := range m {
				ms = append(ms, x)
			}
			downloadCalls = append(downloadCalls, ms)
			if msg, ok := sc.DownloadFail[strconv.Itoa(i)]; ok {
				return "", "", errors.New(msg)
			}
			return "", filepath.Join(t.TempDir(), "dl-"+strconv.Itoa(i)+".png"), nil
		},
		saveAsset: func(fp string) (string, error) {
			i := idxOfName(fp)
			saveCalls = append(saveCalls, i)
			if msg, ok := sc.SaveFail[strconv.Itoa(i)]; ok {
				return "", errors.New(msg)
			}
			return "asset-" + strconv.Itoa(i) + ".png", nil
		},
		getOcrConfig: func() *ocrModelConfig {
			if sc.Ocr == nil {
				return nil
			}
			return &ocrModelConfig{APIURL: "https://ocr", APIKey: "k", Model: "vision"}
		},
		recognize: func(ctx context.Context, fp string, c ocrModelConfig) (string, error) {
			idx := idxOfName(fp)
			recognizeCalls = append(recognizeCalls, idx)
			if idx >= len(sc.Ocr.Texts) {
				return "", nil
			}
			raw := sc.Ocr.Texts[idx]
			if strings.HasPrefix(string(raw), `"`) {
				var s string
				_ = json.Unmarshal(raw, &s)
				return s, nil
			}
			var e struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(raw, &e)
			return "", errors.New(e.Error)
		},
		getTitleConfig: func() *aiClientConfig {
			if !sc.TitleCfg {
				return nil
			}
			return &aiClientConfig{Model: "text"}
		},
		generateTitle: func(ctx context.Context, material string, c aiClientConfig) (string, error) {
			titleCalls = append(titleCalls, material)
			raw := strings.TrimSpace(string(sc.GenTitle))
			if raw == "" || raw == "null" {
				return "", nil
			}
			if strings.HasPrefix(raw, `"`) {
				var s string
				_ = json.Unmarshal(sc.GenTitle, &s)
				return s, nil
			}
			var e struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(sc.GenTitle, &e)
			return "", errors.New(e.Error)
		},
	}
	src := sc.Source.toSource()
	src.DownloadImage = func(ctx context.Context, m []string) (string, string, error) { return "", "", nil }
	e, err := lib.buildImageNoteEntry(context.Background(), src, deps)
	if err != nil {
		fgCompare(t, want, map[string]any{"error": err.Error(), "stages": stages})
		return
	}
	var uri any
	if e.SourceURI != nil {
		uri = *e.SourceURI
	}
	fgCompare(t, want, map[string]any{"ok": map[string]any{"title": e.Title, "content": e.Content, "itemType": e.ItemType, "sourceUri": uri},
		"stages": stages, "downloadCalls": downloadCalls, "saveCalls": saveCalls, "recognizeCalls": recognizeCalls, "titleCalls": titleCalls})
}
