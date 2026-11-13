package main

// NGA 连接器差分测试：对拍 golden-forum.mts / nga-cases.mts 用真实 nga.ts 录制的 BBCode 转换、
// 清洗函数与脚本化整帖抓取结果（kind 以 "nga" 开头）。图片下载与入库用注入的假实现，不触网。

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForumNgaGolden(t *testing.T) {
	recs := loadForumGolden(t)
	l := &Library{}
	n := 0
	for _, r := range recs {
		if !strings.HasPrefix(r.Kind, "nga") {
			continue
		}
		n++
		want := fgDecode(t, r.Output)
		t.Run(r.Kind+"/"+r.Name, func(t *testing.T) {
			switch r.Kind {
			case "ngaBbcode":
				var in struct {
					Raw        string            `json:"raw"`
					AttachBase string            `json:"attachBase"`
					ImageMap   map[string]string `json:"imageMap"`
				}
				mustJSON(t, r.Input, &in)
				base := in.AttachBase
				if base == "" {
					base = ngaDefaultAttach
				}
				fgCompare(t, want, ngaBbcodeToMarkdown(in.Raw, base, in.ImageMap))
			case "ngaReplyCtx":
				var raw string
				mustJSON(t, r.Input, &raw)
				c := extractNgaReplyContext(raw)
				out := map[string]any{"content": c.Content, "replyTo": nil}
				if c.HasReplyTo {
					rt := map[string]any{"author": c.Author, "snippet": c.Snippet}
					if c.HasPid {
						rt["pid"] = c.Pid
					}
					out["replyTo"] = rt
				}
				fgCompare(t, want, out)
			case "ngaStore":
				var raw string
				mustJSON(t, r.Input, &raw)
				v, err := parseNgaStore(raw)
				if err != nil {
					fgCompare(t, want, map[string]any{"error": err.Error()})
					return
				}
				s, ok := jsStringify(v)
				if !ok {
					t.Fatal("jsStringify 失败")
				}
				fgCompare(t, want, map[string]any{"ok": fgDecode(t, json.RawMessage(s))})
			case "ngaEscape":
				var raw string
				mustJSON(t, r.Input, &raw)
				fgCompare(t, want, escapeControlCharsInJSONStrings(raw))
			case "ngaCookies":
				var in struct {
					Existing   *string  `json:"existing"`
					SetCookies []string `json:"setCookies"`
					Body       string   `json:"body"`
				}
				mustJSON(t, r.Input, &in)
				ex := ""
				if in.Existing != nil {
					ex = *in.Existing
				}
				fgCompare(t, want, mergeNgaCookies(ex, in.SetCookies, in.Body))
			case "ngaImgUrl":
				var in struct{ Raw, Base string }
				var raw struct {
					Raw  string `json:"raw"`
					Base string `json:"base"`
				}
				mustJSON(t, r.Input, &raw)
				in.Raw, in.Base = raw.Raw, raw.Base
				u, ok := resolveNgaImageURL(in.Raw, in.Base)
				if ok {
					fgCompare(t, want, u)
				} else {
					fgCompare(t, want, nil)
				}
			case "ngaGuest":
				var raw string
				mustJSON(t, r.Input, &raw)
				if g, ok := extractGuestJS(raw); ok {
					fgCompare(t, want, g)
				} else {
					fgCompare(t, want, nil)
				}
			case "ngaSubject":
				var raw string
				mustJSON(t, r.Input, &raw)
				fgCompare(t, want, decodeNgaSubject(raw))
			case "ngaThread":
				fgNgaThread(t, l, r, want)
			default:
				t.Fatalf("未知 NGA 基线类型 %s", r.Kind)
			}
		})
	}
	if n == 0 {
		t.Skip("基线中没有 NGA 记录，请重新生成 golden-forum.json")
	}
}

func mustJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

func fgNgaThread(t *testing.T, l *Library, r forumRec, want any) {
	var in struct {
		Tid            string                     `json:"tid"`
		Fixture        map[string]json.RawMessage `json:"fixture"`
		Images         map[string]string          `json:"images"`
		RetryDelays    []int                      `json:"retryDelays"`
		ImageLimit     *int                       `json:"imageLimit"`
		MaxSummaryPage *int                       `json:"maxSummaryPages"`
		MaxOpPages     *int                       `json:"maxOpPages"`
		MaxParentFetch *int                       `json:"maxParentFetches"`
	}
	mustJSON(t, r.Input, &in)

	calls := []any{}
	imageCalls := []string{}
	counters := map[string]int{}
	fetch := func(_ context.Context, url string, o safeRequestOptions) (fetchRawTextResult, error) {
		var cookie any
		if o.Cookie != "" {
			cookie = o.Cookie
		}
		accept403 := false
		for _, s := range o.AcceptStatuses {
			if s == 403 {
				accept403 = true
			}
		}
		calls = append(calls, map[string]any{"url": url, "cookie": cookie, "accept403": accept403, "ua": o.UserAgent, "referer": o.Referer})
		raw, ok := in.Fixture[url]
		if !ok {
			return fetchRawTextResult{}, errors.New("HTTP 404")
		}
		var list []json.RawMessage
		if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
			mustJSON(t, raw, &list)
		} else {
			list = []json.RawMessage{raw}
		}
		i := min(counters[url], len(list)-1)
		counters[url]++
		var oc struct {
			Status     *int     `json:"status"`
			Text       string   `json:"text"`
			SetCookies []string `json:"setCookies"`
			Error      *string  `json:"error"`
		}
		mustJSON(t, list[i], &oc)
		if oc.Error != nil {
			return fetchRawTextResult{}, errors.New(*oc.Error)
		}
		status := 200
		if oc.Status != nil {
			status = *oc.Status
		}
		return fetchRawTextResult{Status: status, Text: oc.Text, ContentType: "text/plain", FinalURL: url, SetCookies: oc.SetCookies}, nil
	}
	download := func(_ context.Context, url string) (string, string, error) {
		imageCalls = append(imageCalls, url)
		b64, ok := in.Images[url]
		if !ok {
			return "", "", errors.New("HTTP 404")
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", "", err
		}
		dir := t.TempDir()
		fp := filepath.Join(dir, "image.png")
		if err := os.WriteFile(fp, data, 0o600); err != nil {
			return "", "", err
		}
		return dir, fp, nil
	}
	save := func(fp string) (string, error) {
		data, err := os.ReadFile(fp)
		if err != nil {
			return "", err
		}
		sum := sha1.Sum(data)
		return "img-" + hex.EncodeToString(sum[:])[:8] + ".png", nil
	}
	deps := ngaDeps{
		fetchRawText: fetch, downloadImage: download, saveImageAsset: save,
		retryDelays: make([]time.Duration, len(in.RetryDelays)),
		imageLimit:  in.ImageLimit, maxSummaryPage: in.MaxSummaryPage, maxOpPages: in.MaxOpPages, maxParentFetch: in.MaxParentFetch,
	}
	th, err := l.fetchNgaThread(context.Background(), in.Tid, deps)
	out := map[string]any{"thread": nil, "error": nil, "calls": calls, "imageCalls": imageCalls}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["thread"] = fgThread(th)
	}
	fgCompare(t, want, out)
}

func TestNgaSniffImageExt(t *testing.T) {
	cases := map[string][]byte{
		".jpg":  {0xff, 0xd8, 0xff, 0xe0},
		".png":  []byte("\x89PNG\r\n\x1a\n\x00\x00"),
		".gif":  []byte("GIF89a"),
		".webp": []byte("RIFF\x00\x00\x00\x00WEBP"),
		".bmp":  []byte("BM\x00\x00"),
	}
	for want, head := range cases {
		fp := filepath.Join(t.TempDir(), "x.bin")
		if err := os.WriteFile(fp, head, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := sniffImageExt(fp); got != want {
			t.Errorf("%q → %s，期望 %s", head, got, want)
		}
	}
	// 未识别 / 文件缺失都回落 .jpg（与 TS 一致）
	fp := filepath.Join(t.TempDir(), "y.bin")
	_ = os.WriteFile(fp, []byte("????"), 0o600)
	if got := sniffImageExt(fp); got != ".jpg" {
		t.Errorf("未知头应回落 .jpg，得到 %s", got)
	}
	if got := sniffImageExt(filepath.Join(t.TempDir(), "none")); got != ".jpg" {
		t.Errorf("缺失文件应回落 .jpg，得到 %s", got)
	}
}
