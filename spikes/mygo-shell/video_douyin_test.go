package main

// 抖音连接器差分：对拍 golden-video.mts 里 douyin-cases.mts 录制的结果（真实 douyin.ts）。

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func douyinProject(a *douyinAweme) map[string]any {
	var dur, play any
	if a.DurationSeconds != nil {
		dur = *a.DurationSeconds
	}
	if a.PlayURL != "" {
		play = a.PlayURL
	}
	mirrors := []any{}
	for _, m := range a.ImageMirrors {
		row := []any{}
		for _, u := range m {
			row = append(row, u)
		}
		mirrors = append(mirrors, row)
	}
	return map[string]any{"awemeId": a.AwemeID, "kind": a.Kind, "title": a.Title, "description": a.Description, "author": a.Author,
		"durationSeconds": dur, "playUrl": play, "imageMirrors": mirrors, "webpageUrl": a.WebpageURL}
}

func errCode(err error) any {
	var ppe *platformParseError
	if errors.As(err, &ppe) {
		return ppe.Code
	}
	return nil
}

func douyinGolden(t *testing.T, r forumRec, want any) bool {
	t.Helper()
	switch r.Kind {
	case "douyinId":
		var in string
		mustJSON(t, r.Input, &in)
		var out any
		if id := extractAwemeID(in); id != "" {
			out = id
		}
		fgCompare(t, want, out)
	case "douyinShare":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, douyinShareURL(in))
	case "douyinExt":
		var in string
		mustJSON(t, r.Input, &in)
		fgCompare(t, want, imageExtensionFromURL(in))
	case "douyinParse":
		var in struct{ HTML, ID string }
		mustJSON(t, r.Input, &struct {
			HTML *string `json:"html"`
			ID   *string `json:"id"`
		}{&in.HTML, &in.ID})
		l := &Library{paths: Paths{UserData: t.TempDir()}}
		a, err := l.parseDouyinRouterData(in.HTML, in.ID)
		if err != nil {
			fgCompare(t, want, map[string]any{"error": err.Error(), "code": errCode(err)})
			return true
		}
		fgCompare(t, want, map[string]any{"ok": douyinProject(a)})
	case "douyinFetch":
		var sc struct {
			URL   string `json:"url"`
			Pages map[string]struct {
				HTML     string `json:"html"`
				FinalURL string `json:"finalUrl"`
				Error    string `json:"error"`
			} `json:"pages"`
			Capture *struct {
				HTML  string `json:"html"`
				Error string `json:"error"`
			} `json:"capture"`
		}
		mustJSON(t, r.Input, &sc)
		fetchCalls, captureCalls := []any{}, []any{}
		deps := douyinDeps{
			fetchPage: func(ctx context.Context, u string) (string, string, error) {
				fetchCalls = append(fetchCalls, u)
				p, ok := sc.Pages[u]
				if !ok {
					return "", "", errors.New("未脚本化的地址 " + u)
				}
				if p.Error != "" {
					return "", "", errors.New(p.Error)
				}
				return p.HTML, p.FinalURL, nil
			},
		}
		if sc.Capture != nil {
			deps.captureDetail = func(ctx context.Context, id string) (string, error) {
				captureCalls = append(captureCalls, id)
				if sc.Capture.Error != "" {
					return "", errors.New(sc.Capture.Error)
				}
				return sc.Capture.HTML, nil
			}
		}
		l := &Library{paths: Paths{UserData: t.TempDir()}}
		a, err := l.fetchDouyinAweme(context.Background(), sc.URL, deps)
		if err != nil {
			fgCompare(t, want, map[string]any{"error": err.Error(), "code": errCode(err), "fetchCalls": fetchCalls, "captureCalls": captureCalls})
			return true
		}
		fgCompare(t, want, map[string]any{"ok": douyinProject(a), "fetchCalls": fetchCalls, "captureCalls": captureCalls})
	default:
		return xhsGolden(t, r, want)
	}
	return true
}

// 渲染宿主迁移前：分享页缺数据且没有离屏回退时，如实返回 structure_missing 原因，不静默产出空条目。
func TestDouyinNoFallbackReportsStructureMissing(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	_, err := l.fetchDouyinAweme(context.Background(), "https://www.douyin.com/video/7000000000001", douyinDeps{
		fetchPage: func(context.Context, string) (string, string, error) { return "<html></html>", "", nil },
	})
	if err == nil || errCode(err) != "structure_missing" || !strings.Contains(err.Error(), "分享页未返回作品数据") {
		t.Fatalf("err = %v", err)
	}
}

func TestDouyinImagesNotArrayIsError(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	html := routerHTMLForTest(`{"loaderData":{"a":{"videoInfoRes":{"item_list":[{"aweme_id":"7000000000001","images":"x"}]}}}}`)
	if _, err := l.parseDouyinRouterData(html, "7000000000001"); err == nil || errCode(err) != nil {
		t.Fatalf("images 非数组应报普通错误: %v", err)
	}
}

func routerHTMLForTest(json string) string {
	return "<script>window._ROUTER_DATA = " + json + ";</script>"
}

// 视频 / 图文两条路径经 resolveDouyinSource 的组装：视频走无水印下载，图文走配图 OCR 条目。
func TestResolveDouyinSource(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	vid := &douyinAweme{AwemeID: "1", Kind: "video", Title: "t", Author: "a", PlayURL: "https://p/play/", WebpageURL: "https://www.douyin.com/video/1"}
	var downloaded string
	deps := &videoDeps{douyin: douyinDeps{
		fetch: func(context.Context, string) (*douyinAweme, error) { return vid, nil },
		download: func(ctx context.Context, u string) (string, string, error) {
			downloaded = u
			return "d", "f", nil
		},
	}}
	src, err := l.resolveDouyinSource(context.Background(), "u", deps)
	if err != nil {
		t.Fatal(err)
	}
	if dir, file, err := src.downloadAudio(context.Background()); err != nil || dir != "d" || file != "f" || downloaded != "https://p/play/" {
		t.Fatalf("下载未走注入: %q %q %v %q", dir, file, err, downloaded)
	}
	if src.note != nil {
		t.Fatal("视频作品不应有图文条目")
	}

	vid.PlayURL = ""
	if _, err := l.resolveDouyinSource(context.Background(), "u", deps); err == nil || !strings.Contains(err.Error(), "未能取到视频播放地址") {
		t.Fatalf("缺播放地址应报错: %v", err)
	}

	note := &douyinAweme{AwemeID: "2", Kind: "note", Title: "图文", Description: "文案", Author: "作者", ImageMirrors: [][]string{{"https://i/1.jpg"}}, WebpageURL: "https://www.douyin.com/note/2"}
	var stages []string
	deps = &videoDeps{
		onStage: func(s string) { stages = append(stages, s) },
		douyin:  douyinDeps{fetch: func(context.Context, string) (*douyinAweme, error) { return note, nil }},
		imageNote: imageNoteDeps{
			downloadImage: func(context.Context, []string) (string, string, error) {
				return "", filepath.Join(t.TempDir(), "x.png"), nil
			},
			saveAsset:      func(string) (string, error) { return "a.png", nil },
			getOcrConfig:   func() *ocrModelConfig { return nil },
			getTitleConfig: func() *aiClientConfig { return nil },
		},
	}
	src, err = l.resolveDouyinSource(context.Background(), "u", deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := src.downloadAudio(context.Background()); err == nil || err.Error() != "图文作品没有音轨" {
		t.Fatalf("图文无音轨: %v", err)
	}
	e, err := src.note()
	if err != nil || e.ItemType != "image" || e.Title != "图文" || !strings.Contains(e.Content, "![图 1](local-image://a.png)") {
		t.Fatalf("图文条目不符: %+v %v", e, err)
	}
	if strings.Join(stages, ",") != "image-download" {
		t.Fatalf("阶段 = %v", stages)
	}
}
