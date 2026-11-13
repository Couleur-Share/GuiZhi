package main

// 小红书连接器差分：对拍 golden-video.mts 里 xhs-cases.mts 录制的结果（真实 xiaohongshu.ts）。

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xhsProject(n *xiaohongshuNote) map[string]any {
	var dur any
	if n.DurationSeconds != nil {
		dur = *n.DurationSeconds
	}
	play := []any{}
	for _, u := range n.PlayURLs {
		play = append(play, u)
	}
	mirrors := []any{}
	for _, m := range n.ImageMirrors {
		row := []any{}
		for _, u := range m {
			row = append(row, u)
		}
		mirrors = append(mirrors, row)
	}
	return map[string]any{"noteId": n.NoteID, "kind": n.Kind, "title": n.Title, "authoredTitle": n.AuthoredTitle, "description": n.Description,
		"author": n.Author, "durationSeconds": dur, "playUrls": play, "imageMirrors": mirrors, "webpageUrl": n.WebpageURL}
}

func xhsGolden(t *testing.T, r forumRec, want any) bool {
	t.Helper()
	switch r.Kind {
	case "xhsId":
		var in string
		mustJSON(t, r.Input, &in)
		var out any
		if id := extractXhsNoteID(in); id != "" {
			out = id
		}
		fgCompare(t, want, out)
	case "xhsUrl":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, xiaohongshuNoteURL(in))
	case "xhsClean":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, cleanNoteText(in))
	case "xhsSlice":
		var in string
		mustJSON(t, r.Input, &in)
		var out any
		if s, ok := sliceInitialStateJSON(in); ok {
			out = s
		}
		fgCompare(t, want, out)
	case "xhsParse":
		var in struct {
			HTML string  `json:"html"`
			ID   *string `json:"id"`
		}
		mustJSON(t, r.Input, &in)
		id := ""
		if in.ID != nil {
			id = *in.ID
		}
		l := &Library{paths: Paths{UserData: t.TempDir()}}
		n, err := l.parseXiaohongshuNote(in.HTML, id)
		if err != nil {
			fgCompare(t, want, map[string]any{"error": err.Error(), "code": errCode(err)})
			return true
		}
		fgCompare(t, want, map[string]any{"ok": xhsProject(n)})
	case "xhsFetch":
		var sc struct {
			URL   string `json:"url"`
			Pages map[string]struct {
				HTML     string `json:"html"`
				FinalURL string `json:"finalUrl"`
				Error    string `json:"error"`
			} `json:"pages"`
		}
		mustJSON(t, r.Input, &sc)
		fetchCalls := []any{}
		deps := xiaohongshuDeps{fetchPage: func(ctx context.Context, u string) (string, string, error) {
			fetchCalls = append(fetchCalls, u)
			p, ok := sc.Pages[u]
			if !ok {
				return "", "", errors.New("未脚本化的地址 " + u)
			}
			if p.Error != "" {
				return "", "", errors.New(p.Error)
			}
			return p.HTML, p.FinalURL, nil
		}}
		l := &Library{paths: Paths{UserData: t.TempDir()}}
		n, err := l.fetchXiaohongshuNote(context.Background(), sc.URL, deps)
		if err != nil {
			fgCompare(t, want, map[string]any{"error": err.Error(), "code": errCode(err), "fetchCalls": fetchCalls})
			return true
		}
		fgCompare(t, want, map[string]any{"ok": xhsProject(n), "fetchCalls": fetchCalls})
	default:
		return articleGolden(t, r, want)
	}
	return true
}

func TestXiaohongshuImageNonArrayErrors(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	id := "65f1a2b3c4d5e6f7a8b9c0d1"
	for _, body := range []string{`"imageList":"x"`, `"imageList":[{"infoList":"x"}]`} {
		html := `<script>window.__INITIAL_STATE__={"note":{"noteDetailMap":{"` + id + `":{"note":{"type":"normal",` + body + `}}}}}</script>`
		if _, err := l.parseXiaohongshuNote(html, id); err == nil || errCode(err) != nil {
			t.Fatalf("%s 应报普通错误: %v", body, err)
		}
	}
}

// 图片按文件头定扩展名（小红书图片地址没有扩展名），下载失败逐镜像降级并汇总原因；请求带桌面 UA 与站内 Referer。
func TestXiaohongshuDownloadImageSniffAndFallback(t *testing.T) {
	jpeg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF")
	var gotUA, gotRef string
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			w.WriteHeader(404)
			return
		}
		gotUA, gotRef = r.Header.Get("User-Agent"), r.Header.Get("Referer")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpeg)
	}))
	l := &Library{paths: Paths{UserData: t.TempDir()}, webFetcher: fx.f}
	base := "http://site.test:" + fx.port()
	dir, file, err := l.downloadXiaohongshuImage(context.Background(), []string{base + "/missing", base + "/ok"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if filepath.Base(file) != "image.jpg" || gotUA != xhsDesktopUA || gotRef != "https://www.xiaohongshu.com/" {
		t.Fatalf("文件=%s ua=%q ref=%q", file, gotUA, gotRef)
	}
	if _, _, err := l.downloadXiaohongshuImage(context.Background(), []string{base + "/a", base + "/b"}); err == nil || !strings.Contains(err.Error(), "；") {
		t.Fatalf("全部失败应汇总每个镜像的原因: %v", err)
	}
	if _, _, err := l.downloadXiaohongshuImage(context.Background(), nil); err == nil || err.Error() != "没有可用的下载地址" {
		t.Fatalf("空镜像: %v", err)
	}
}

// 视频 / 图文两条路径经 resolveXiaohongshuSource 的组装。
func TestResolveXiaohongshuSource(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	vid := &xiaohongshuNote{NoteID: "n", Kind: "video", Title: "t", PlayURLs: []string{"https://v/1", "https://v/2"}, WebpageURL: "https://www.xiaohongshu.com/explore/n"}
	var got []string
	deps := &videoDeps{xiaohongshu: xiaohongshuDeps{
		fetch: func(context.Context, string) (*xiaohongshuNote, error) { return vid, nil },
		download: func(ctx context.Context, urls []string) (string, string, error) {
			got = urls
			return "d", "f", nil
		},
	}}
	src, err := l.resolveXiaohongshuSource(context.Background(), "u", deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := src.downloadAudio(context.Background()); err != nil || len(got) != 2 || src.note != nil {
		t.Fatalf("视频下载未走注入: %v %v", got, err)
	}
	vid.PlayURLs = nil
	if _, err := l.resolveXiaohongshuSource(context.Background(), "u", deps); err == nil || !strings.Contains(err.Error(), "未能取到视频播放地址") {
		t.Fatalf("缺播放地址应报错: %v", err)
	}

	note := &xiaohongshuNote{NoteID: "n", Kind: "note", Title: "作者标题", AuthoredTitle: true, Description: "文案", Author: "作者", ImageMirrors: [][]string{{"https://i/1"}}, WebpageURL: "https://www.xiaohongshu.com/explore/n"}
	deps = &videoDeps{
		xiaohongshu: xiaohongshuDeps{fetch: func(context.Context, string) (*xiaohongshuNote, error) { return note, nil }},
		imageNote: imageNoteDeps{
			downloadImage: func(context.Context, []string) (string, string, error) {
				return "", filepath.Join(t.TempDir(), "x.png"), nil
			},
			saveAsset:      func(string) (string, error) { return "a.png", nil },
			getOcrConfig:   func() *ocrModelConfig { return nil },
			getTitleConfig: func() *aiClientConfig { return &aiClientConfig{Model: "m"} }, // 作者标题不重拟
			generateTitle: func(context.Context, string, aiClientConfig) (string, error) {
				t.Fatal("作者标题不应调用拟题")
				return "", nil
			},
		},
	}
	src, err = l.resolveXiaohongshuSource(context.Background(), "u", deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := src.downloadAudio(context.Background()); err == nil || err.Error() != "图文笔记没有音轨" {
		t.Fatalf("图文无音轨: %v", err)
	}
	e, err := src.note()
	if err != nil || e.Title != "作者标题" || e.ItemType != "image" {
		t.Fatalf("图文条目不符: %+v %v", e, err)
	}
}
