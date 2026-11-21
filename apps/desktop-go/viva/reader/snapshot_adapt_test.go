package reader

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 网页快照文档适配（snapshot_adapt.go）与阅读岛内容安全策略（serve.go）的测试：
// 地址与 <meta> 策略的改写规则、改写后的地址确实落在宿主提供媒体的路径上、策略里只放行桥接脚本的那一个哈希。
// 真 WebView2 里的实际效果由 readersmoke 的 snapshot 场景实测（台账 R-15）。

const snapshotMetaDoc = `<!doctype html><html data-instance="i"><head><meta http-equiv="Content-Security-Policy" ` +
	`content="default-src &#39;none&#39;; script-src &#39;sha256-abc&#39;; style-src &#39;unsafe-inline&#39;; img-src local-image:; connect-src &#39;none&#39;"></head>` +
	`<body><img src="local-image://a.png"></body></html>`

func TestAdaptSnapshotDocumentSwapsTheMetaImageSource(t *testing.T) {
	got := AdaptSnapshotDocument(snapshotMetaDoc)
	if strings.Contains(got, "img-src local-image:") || !strings.Contains(got, "img-src data:;") {
		t.Errorf("<meta> 策略里的图片来源应换成岛的 scheme：%s", got)
	}
	// 策略里其余的部分一个字不动：脚本仍然只许桥接脚本的哈希，不因适配而放宽。
	for _, keep := range []string{"default-src &#39;none&#39;;", "script-src &#39;sha256-abc&#39;;", "style-src &#39;unsafe-inline&#39;;", "connect-src &#39;none&#39;"} {
		if !strings.Contains(got, keep) {
			t.Errorf("适配不该动策略的其余部分，丢了 %q：%s", keep, got)
		}
	}
	if strings.Contains(got, "http:") || strings.Contains(got, "https:") || strings.Contains(got, "'unsafe-inline'") {
		t.Errorf("适配不能放开外部域或 unsafe-inline：%s", got)
	}
}

func TestAdaptSnapshotDocumentRewritesLocalImageAddresses(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"属性":       {`<img src="local-image://a.png">`, `<img src="./media/local-image/a.png">`},
		"单引号属性":    {`<img src='local-image://a.png'>`, `<img src='./media/local-image/a.png'>`},
		"CSS 无引号":  {`a{background:url(local-image://b.webp)}`, `a{background:url(./media/local-image/b.webp)}`},
		"CSS 带引号":  {`a{background:url("local-image://b.webp")}`, `a{background:url("./media/local-image/b.webp")}`},
		"一处多个":     {`<img src="local-image://a.png"><img src="local-image://b.jpg">`, `<img src="./media/local-image/a.png"><img src="./media/local-image/b.jpg">`},
		"已编码的名称":   {`<img src="local-image://a%20b.png">`, `<img src="./media/local-image/a%20b.png">`},
		"子目录":      {`<img src="local-image://sub/a.png">`, `<img src="./media/local-image/sub/a.png">`},
		"中文文件名":    {`<img src="local-image://图一.png">`, `<img src="./media/local-image/%E5%9B%BE%E4%B8%80.png">`},
		"没有本地图的文档": {`<p>正文</p><img src="https://example.com/a.png">`, `<p>正文</p><img src="https://example.com/a.png">`},
	}
	for name, c := range cases {
		got := AdaptSnapshotDocument(c.in)
		if got != c.want {
			t.Errorf("%s：\n 实际 %s\n 期望 %s", name, got, c.want)
		}
	}
}

func TestAdaptSnapshotDocumentRefusesPathEscapes(t *testing.T) {
	for _, in := range []string{
		`<img src="local-image://../index.html">`,
		`<img src="local-image://sub/../../x.png">`,
		`<img src="local-image://./a.png">`,
		`<img src="local-image://%2e%2e/index.html">`,
		`<img src="local-image://..%2fx.png">`,
		`<img src="local-image:///">`,
	} {
		got := AdaptSnapshotDocument(in)
		if strings.Contains(got, "./media/") {
			t.Errorf("含 . / .. 段或空名称的地址不能改写成岛里的路径：%s → %s", in, got)
		}
	}
}

func TestAdaptSnapshotDocumentIsIdempotentAndOnlyTouchesTheFirstMeta(t *testing.T) {
	once := AdaptSnapshotDocument(snapshotMetaDoc)
	if twice := AdaptSnapshotDocument(once); twice != once {
		t.Errorf("适配两次与一次相同：\n%s\n%s", once, twice)
	}
	// 正文里恰好有同样的一段文字（文章讲 CSP 之类）时只换第一处（<meta> 在 <head>，总在正文前）。
	body := snapshotMetaDoc + `<p>img-src local-image:;</p>`
	got := AdaptSnapshotDocument(body)
	if strings.Count(got, "img-src local-image:;") != 1 || strings.Count(got, "img-src data:;") != 1 {
		t.Errorf("只该换第一处：%s", got)
	}
}

// 改写后的地址经页面（相对文档解析）与宿主的 serve 之后，应当落在 Media 回调的 (local-image, 名称) 上。
func TestAdaptedAddressesReachTheMediaCallback(t *testing.T) {
	var gotScheme, gotName string
	h := &Host{opts: Options{Media: func(scheme, name string) (string, []byte, bool) {
		gotScheme, gotName = scheme, name
		return "image/png", []byte("png"), true
	}}}
	adapted := AdaptSnapshotDocument(`<img src="local-image://sub/a%20b.png">`)
	const prefix = `<img src="./`
	rel := strings.TrimSuffix(strings.TrimPrefix(adapted, prefix), `">`)
	if rel == adapted {
		t.Fatalf("形状不对：%s", adapted)
	}
	// 页面地址是 https://gzreader.viva-app.invalid/doc/index.html，相对地址 ./media/… 解析成 …/doc/media/…，宿主收到去掉主机段（doc）的 media/…。
	mime, data, ok := h.serve(rel)
	if !ok || mime != "image/png" || string(data) != "png" {
		t.Fatalf("宿主应当取到图片：%q %q %v", mime, data, ok)
	}
	if gotScheme != "local-image" || gotName != "sub/a%20b.png" {
		t.Errorf("媒体回调收到 (%q, %q)", gotScheme, gotName)
	}
}

func TestReaderCSPPinsExactlyTheBridgeHash(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(SnapshotBridgeSHA256)
	if err != nil || len(raw) != 32 {
		t.Fatalf("SnapshotBridgeSHA256 应是 32 字节摘要的 base64：%v %d", err, len(raw))
	}
	directive := func(csp, name string) string {
		for _, part := range strings.Split(csp, ";") {
			if part = strings.TrimSpace(part); strings.HasPrefix(part, name+" ") {
				return part
			}
		}
		return ""
	}
	if got, want := directive(readerCSP, "script-src"), "script-src 'self' 'sha256-"+SnapshotBridgeSHA256+"'"; got != want {
		t.Errorf("阅读岛的 script-src 应只在自有 scheme 之外放行桥接脚本的哈希：\n 实际 %s\n 期望 %s", got, want)
	}
	if got := directive(viewerCSP, "script-src"); got != "script-src 'self'" {
		t.Errorf("图片查看岛不放行任何内联脚本：%s", got)
	}
	for name, csp := range map[string]string{"阅读岛": readerCSP, "查看岛": viewerCSP} {
		if strings.Contains(directive(csp, "script-src"), "unsafe-inline") || strings.Contains(directive(csp, "script-src"), "unsafe-eval") {
			t.Errorf("%s不得放开 unsafe-inline / unsafe-eval 脚本", name)
		}
		if got := directive(csp, "connect-src"); got != "connect-src 'self'" {
			t.Errorf("%s的连接只许自有 scheme：%s", name, got)
		}
		if got := directive(csp, "frame-src"); got != "frame-src about:" {
			t.Errorf("%s的内嵌框架只许 about:（srcdoc）：%s", name, got)
		}
	}
	if strings.Replace(readerCSP, " 'sha256-"+SnapshotBridgeSHA256+"'", "", 1) != viewerCSP {
		t.Error("阅读岛与查看岛的策略除了桥接脚本的哈希之外应完全相同")
	}
}

// 引用在 `&`（HTML 实体的起点）处截止：实体之后的 `/../` 不算名称里的 `..` 段，前面的图片地址照常改写。
func TestAdaptSnapshotDocumentStopsAReferenceAtAnEntityBoundary(t *testing.T) {
	in := `<img src="local-image://a.png&amp;/../x">`
	want := `<img src="./media/local-image/a.png&amp;/../x">`
	if got := AdaptSnapshotDocument(in); got != want {
		t.Errorf("引用应在 & 处截止：\n 实际 %s\n 期望 %s", got, want)
	}
}

// 文档大小上限按适配后的算：原文刚好不超限、改写本地图片地址后变长超限的文档要被拒绝。
func TestSnapshotViewMarshalLimitIsCheckedAfterAdaptation(t *testing.T) {
	refs := strings.Repeat(`<img src="local-image://a.png">`, 3)
	doc := refs + strings.Repeat("x", MaxSnapshotDocBytes-len(refs))
	if len(doc) != MaxSnapshotDocBytes || len(AdaptSnapshotDocument(doc)) <= MaxSnapshotDocBytes {
		t.Fatalf("前提不成立：原文 %d 字节，适配后 %d 字节", len(doc), len(AdaptSnapshotDocument(doc)))
	}
	if _, err := (View{Kind: ViewSnapshot, SnapshotDoc: doc, SnapshotInstanceID: "i"}).MarshalJSON(); err == nil {
		t.Fatal("适配后超过上限的文档应被拒绝")
	}
}
