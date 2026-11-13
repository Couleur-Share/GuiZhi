package main

// 公众号采集差分测试：对拍 golden-wechat.mts 用真实 wechat.ts 在夹具页面上录制的结果
// （GUIZHI_WECHAT_GOLDEN_DIR 或 GUIZHI_GOLDEN_DIR）。Markdown 与 Turndown 的字节差异属已接受差异，只做结构校验。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type wxGoldenCase struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Pages map[string]struct {
		HTML     string  `json:"html"`
		FinalURL *string `json:"finalUrl"`
	} `json:"pages"`
	Images map[string]struct {
		B64    *string `json:"b64"`
		Status *int    `json:"status"`
	} `json:"images"`
	Result *struct {
		Title          string         `json:"title"`
		Author         string         `json:"author"`
		PublishedAt    *int64         `json:"publishedAt"`
		DateConfidence string         `json:"dateConfidence"`
		EngineVersion  string         `json:"engineVersion"`
		Complete       bool           `json:"complete"`
		Warnings       []string       `json:"warnings"`
		FinalURL       string         `json:"finalUrl"`
		Snapshot       map[string]any `json:"snapshot"`
	} `json:"result"`
	Error *string  `json:"error"`
	Files []string `json:"files"`
}

func loadWechatGolden(t *testing.T) []wxGoldenCase {
	t.Helper()
	dir := os.Getenv("GUIZHI_WECHAT_GOLDEN_DIR")
	if dir == "" {
		dir = os.Getenv("GUIZHI_GOLDEN_DIR")
	}
	if dir == "" {
		t.Skip("未设置 GUIZHI_WECHAT_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-wechat.json"))
	if err != nil {
		t.Skip("未找到 golden-wechat.json：" + err.Error())
	}
	var g struct {
		Cases []wxGoldenCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g.Cases
}

// wxFixture 把 mp.weixin.qq.com / mmbiz.qpic.cn 解析成“公网”地址并转到本地服务器，按 host+路径返回夹具。
func wxFixture(t *testing.T, c wxGoldenCase) *safeFetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "http://" + r.Host + r.URL.RequestURI()
		for _, p := range c.Pages {
			// 夹具里的 finalUrl 对应一次跳转：入口地址 302 到它，目标地址返回同一份 HTML
			if p.FinalURL != nil && key != *p.FinalURL {
				if _, isEntry := c.Pages[key]; isEntry {
					http.Redirect(w, r, *p.FinalURL, http.StatusFound)
					return
				}
			}
			if p.FinalURL != nil && key == *p.FinalURL {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, p.HTML)
				return
			}
		}
		if p, ok := c.Pages[key]; ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, p.HTML)
			return
		}
		if img, ok := c.Images[key]; ok && img.B64 != nil && img.Status == nil {
			data, _ := base64.StdEncoding.DecodeString(*img.B64)
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	target := srv.Listener.Addr().String()
	return &safeFetcher{
		proxy: newNetProxyState(),
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		dialOverride: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
		idle: 2 * time.Second,
	}
}

func snapObjMap(o *jsObj) map[string]any {
	s, _ := jsStringify(o)
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// sortedByKey 把对象数组按给定字段排序，供不依赖并发完成顺序的比较。
func sortedByKey(v any, keys ...string) any {
	arr, ok := v.([]any)
	if !ok {
		return v
	}
	out := append([]any(nil), arr...)
	sort.SliceStable(out, func(i, j int) bool {
		var a, b string
		for _, key := range keys {
			x, _ := out[i].(map[string]any)[key].(string)
			y, _ := out[j].(map[string]any)[key].(string)
			a, b = a+"\x00"+x, b+"\x00"+y
		}
		return a < b
	})
	return out
}

func TestWechatGolden(t *testing.T) {
	cases := loadWechatGolden(t)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ud := t.TempDir()
			l := &Library{paths: Paths{UserData: ud}, webFetcher: wxFixture(t, c)}
			got, err := l.captureWechat(context.Background(), c.URL, "task-"+c.Name)
			if c.Error != nil {
				if err == nil {
					t.Fatalf("应失败：%s", *c.Error)
				}
				if err.Error() != *c.Error {
					t.Fatalf("错误文案不一致\n got: %s\nwant: %s", err.Error(), *c.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应失败：%v", err)
			}
			defer got.release()
			want := c.Result
			if got.Title != want.Title || got.Author != want.Author || got.DateConfidence != want.DateConfidence ||
				got.EngineVersion != want.EngineVersion || got.Complete != want.Complete || got.FinalURL != want.FinalURL {
				t.Errorf("元数据不一致\n got: %q %q %q %q %v %q\nwant: %q %q %q %q %v %q",
					got.Title, got.Author, got.DateConfidence, got.EngineVersion, got.Complete, got.FinalURL,
					want.Title, want.Author, want.DateConfidence, want.EngineVersion, want.Complete, want.FinalURL)
			}
			if (got.PublishedAt == nil) != (want.PublishedAt == nil) || (got.PublishedAt != nil && *got.PublishedAt != *want.PublishedAt) {
				t.Errorf("publishedAt 不一致：%v / %v", got.PublishedAt, want.PublishedAt)
			}
			if !reflect.DeepEqual(got.Warnings, want.Warnings) {
				t.Errorf("warnings 不一致：%v / %v", got.Warnings, want.Warnings)
			}
			if !strings.HasSuffix(got.Markdown, "\n\n---\n\n来源：<"+got.FinalURL+">") {
				t.Errorf("Markdown 缺少来源尾部：%q", got.Markdown)
			}
			gs, ws := snapObjMap(got.Snapshot), want.Snapshot
			for _, k := range []string{"assets", "failures"} {
				keys := map[string][]string{"assets": {"fileName", "sourceUrl"}, "failures": {"url"}}[k]
				gs[k], ws[k] = sortedByKey(gs[k], keys...), sortedByKey(ws[k], keys...)
			}
			for k := range ws {
				if !reflect.DeepEqual(gs[k], ws[k]) {
					t.Errorf("快照字段 %s 不一致\n got: %v\nwant: %v", k, gs[k], ws[k])
				}
			}
			for k := range gs {
				if _, ok := ws[k]; !ok {
					t.Errorf("Go 多出快照字段 %s", k)
				}
			}
			entries, _ := os.ReadDir(Paths{UserData: ud}.ImagesDir())
			var files []string
			for _, e := range entries {
				files = append(files, e.Name())
			}
			sort.Strings(files)
			if len(files) != len(c.Files) || (len(files) > 0 && !reflect.DeepEqual(files, c.Files)) {
				t.Errorf("落盘资产不一致：%v / %v", files, c.Files)
			}
		})
	}
}
