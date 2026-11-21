package reader

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func mustEvent(t *testing.T, text string) Event {
	t.Helper()
	ev, err := ParseEvent(text)
	if err != nil {
		t.Fatalf("ParseEvent(%s) 不该失败：%v", text, err)
	}
	return ev
}

func TestParseEventAcceptsEveryKind(t *testing.T) {
	cases := []struct {
		name string
		text string
		want Event
	}{
		{"ready", `{"t":"ready","v":1}`, Event{Kind: EventReady, Version: 1}},
		{"rendered", `{"t":"rendered","seq":3}`, Event{Kind: EventRendered, Seq: 3}},
		{"matches", `{"t":"matches","seq":2,"count":12}`, Event{Kind: EventMatches, Seq: 2, Count: 12}},
		{"scroll", `{"t":"scroll","seq":2,"top":144.5}`, Event{Kind: EventScroll, Seq: 2, Top: 144.5}},
		{"catalog", `{"t":"catalog","seq":2,"open":true}`, Event{Kind: EventCatalog, Seq: 2, Open: true}},
		{"toc 关", `{"t":"toc","seq":2,"open":false}`, Event{Kind: EventToc, Seq: 2}},
		{"link", `{"t":"link","seq":4,"url":"https://example.com"}`, Event{Kind: EventLink, Seq: 4, URL: "https://example.com"}},
		{"copy 空串", `{"t":"copy","seq":4,"text":""}`, Event{Kind: EventCopy, Seq: 4}},
		{"copy", `{"t":"copy","seq":4,"text":"a\nb"}`, Event{Kind: EventCopy, Seq: 4, Text: "a\nb"}},
		{"toast", `{"t":"toast","seq":4,"kind":"warning","text":"x"}`, Event{Kind: EventToast, Seq: 4, Text: "x"}},
		{"key 无修饰键", `{"t":"key","seq":4,"key":"arrowleft"}`, Event{Kind: EventKey, Seq: 4, Key: "arrowleft"}},
		{"key 全修饰键", `{"t":"key","seq":4,"key":"z","ctrl":true,"alt":true,"shift":true}`,
			Event{Kind: EventKey, Seq: 4, Key: "z", Ctrl: true, Alt: true, Shift: true}},
		{"findReset", `{"t":"findReset","seq":5}`, Event{Kind: EventFindReset, Seq: 5}},
		{"error", `{"t":"error","seq":5,"message":"boom"}`, Event{Kind: EventError, Seq: 5, Message: "boom"}},
		{"lightbox", `{"t":"lightbox","seq":6,"index":1,"images":[{"src":"local-image://a.png"},{"src":"local-image://b.png","alt":"图"}]}`,
			Event{Kind: EventLightbox, Seq: 6, Index: 1, Images: []Image{{Src: "local-image://a.png"}, {Src: "local-image://b.png", Alt: "图"}}}},
	}
	for _, c := range cases {
		if got := mustEvent(t, c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s：\n  实际 %+v\n  期望 %+v", c.name, got, c.want)
		}
	}
}

func TestParseEventRejects(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct{ name, text string }{
		{"不是 JSON", `nope`},
		{"数组", `[1]`},
		{"空对象", `{}`},
		{"未知种类", `{"t":"nope","seq":1}`},
		{"未知种类且没有序号", `{"t":"nope"}`},
		{"ready 没有版本", `{"t":"ready"}`},
		{"ready 版本是字符串", `{"t":"ready","v":"1"}`},
		{"ready 版本是小数", `{"t":"ready","v":1.5}`},
		{"ready 版本是负数", `{"t":"ready","v":-1}`},
		{"没有序号", `{"t":"rendered"}`},
		{"序号是字符串", `{"t":"rendered","seq":"1"}`},
		{"序号是负数", `{"t":"rendered","seq":-1}`},
		{"序号是小数", `{"t":"rendered","seq":1.5}`},
		{"序号超出安全整数", `{"t":"rendered","seq":9007199254740993}`},
		{"matches 没有数量", `{"t":"matches","seq":1}`},
		{"matches 数量为负", `{"t":"matches","seq":1,"count":-1}`},
		{"matches 数量过大", `{"t":"matches","seq":1,"count":2000000000}`},
		{"matches 数量是小数", `{"t":"matches","seq":1,"count":1.5}`},
		{"scroll 没有位置", `{"t":"scroll","seq":1}`},
		{"scroll 位置是字符串", `{"t":"scroll","seq":1,"top":"9"}`},
		{"scroll 位置溢出", `{"t":"scroll","seq":1,"top":1e999}`},
		{"catalog 没有开合", `{"t":"catalog","seq":1}`},
		{"toc 开合是数字", `{"t":"toc","seq":1,"open":1}`},
		{"link 没有地址", `{"t":"link","seq":1}`},
		{"link 空地址", `{"t":"link","seq":1,"url":""}`},
		{"link 超长", `{"t":"link","seq":1,"url":"` + long(MaxLinkBytes+1) + `"}`},
		{"copy 没有文本", `{"t":"copy","seq":1}`},
		{"copy 超长", `{"t":"copy","seq":1,"text":"` + long(MaxCopyBytes+1) + `"}`},
		{"toast 不是 warning", `{"t":"toast","seq":1,"kind":"error","text":"x"}`},
		{"toast 没有 kind", `{"t":"toast","seq":1,"text":"x"}`},
		{"toast 空文字", `{"t":"toast","seq":1,"kind":"warning","text":""}`},
		{"toast 超长", `{"t":"toast","seq":1,"kind":"warning","text":"` + long(maxToastBytes+1) + `"}`},
		{"key 没有键名", `{"t":"key","seq":1}`},
		{"key 不在白名单", `{"t":"key","seq":1,"key":"Delete"}`},
		{"key 大写不算", `{"t":"key","seq":1,"key":"F"}`},
		{"key 修饰键是字符串", `{"t":"key","seq":1,"key":"f","ctrl":"yes"}`},
		{"error 没有说明", `{"t":"error","seq":1}`},
		{"error 超长", `{"t":"error","seq":1,"message":"` + long(maxErrorBytes+1) + `"}`},
		{"lightbox 没有图", `{"t":"lightbox","seq":1,"index":0,"images":[]}`},
		{"lightbox 没有 images", `{"t":"lightbox","seq":1,"index":0}`},
		{"lightbox 没有序号", `{"t":"lightbox","seq":1,"images":[{"src":"local-image://a.png"}]}`},
		{"lightbox 序号越界", `{"t":"lightbox","seq":1,"index":1,"images":[{"src":"local-image://a.png"}]}`},
		{"lightbox 序号为负", `{"t":"lightbox","seq":1,"index":-1,"images":[{"src":"local-image://a.png"}]}`},
		{"lightbox 远程图片", `{"t":"lightbox","seq":1,"index":0,"images":[{"src":"https://example.com/a.png"}]}`},
		{"lightbox 其它 scheme", `{"t":"lightbox","seq":1,"index":0,"images":[{"src":"file:///C:/a.png"}]}`},
		{"lightbox 空名称", `{"t":"lightbox","seq":1,"index":0,"images":[{"src":"local-image://"}]}`},
		{"lightbox 没有 src", `{"t":"lightbox","seq":1,"index":0,"images":[{"alt":"x"}]}`},
		{"lightbox src 超长", `{"t":"lightbox","seq":1,"index":0,"images":[{"src":"local-image://` + long(maxImageSrcBytes) + `"}]}`},
		{"lightbox alt 超长", `{"t":"lightbox","seq":1,"index":0,"images":[{"src":"local-image://a.png","alt":"` + long(maxImageAltBytes+1) + `"}]}`},
		{"整条超长", `{"t":"copy","seq":1,"text":"` + long(MaxEventBytes) + `"}`},
		{"非法 UTF-8 的文本", "{\"t\":\"copy\",\"seq\":1,\"text\":\"\xff\xfe\"}"},
	}
	for _, c := range cases {
		_, err := ParseEvent(c.text)
		if !errors.Is(err, ErrBadEvent) {
			t.Errorf("%s：应当以 ErrBadEvent 拒绝，实际 %v", c.name, err)
		}
	}
}

func TestParseEventLimitsAreInclusive(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	for _, text := range []string{
		`{"t":"link","seq":1,"url":"` + long(MaxLinkBytes) + `"}`,
		`{"t":"copy","seq":1,"text":"` + long(MaxCopyBytes) + `"}`,
		`{"t":"toast","seq":1,"kind":"warning","text":"` + long(maxToastBytes) + `"}`,
		`{"t":"error","seq":1,"message":"` + long(maxErrorBytes) + `"}`,
		`{"t":"matches","seq":1,"count":1073741824}`,
		`{"t":"rendered","seq":9007199254740991}`,
	} {
		if _, err := ParseEvent(text); err != nil {
			t.Errorf("上限本身应当放行：%.60s…：%v", text, err)
		}
	}
	var images strings.Builder
	images.WriteString(`{"t":"lightbox","seq":1,"index":0,"images":[`)
	for i := 0; i < MaxLightboxImages; i++ {
		if i > 0 {
			images.WriteString(",")
		}
		images.WriteString(`{"src":"local-image://a.png"}`)
	}
	images.WriteString(`]}`)
	if ev := mustEvent(t, images.String()); len(ev.Images) != MaxLightboxImages {
		t.Errorf("图片数上限放行：%d", len(ev.Images))
	}
	over := strings.Replace(images.String(), `]}`, `,{"src":"local-image://a.png"}]}`, 1)
	if _, err := ParseEvent(over); !errors.Is(err, ErrBadEvent) {
		t.Errorf("超过图片数上限要拒绝：%v", err)
	}
}

func TestParseEventClampsScrollTop(t *testing.T) {
	if ev := mustEvent(t, `{"t":"scroll","seq":1,"top":-50}`); ev.Top != 0 {
		t.Errorf("负位置夹到 0：%v", ev.Top)
	}
	if ev := mustEvent(t, `{"t":"scroll","seq":1,"top":1e18}`); ev.Top != maxScrollTop {
		t.Errorf("过大位置夹到上限：%v", ev.Top)
	}
	if ev := mustEvent(t, `{"t":"scroll","seq":1,"top":0}`); ev.Top != 0 {
		t.Errorf("0 就是 0：%v", ev.Top)
	}
}

func TestParseEventErrorsNeverEchoPageContent(t *testing.T) {
	secret := "ZZ-SECRET-ZZ"
	for _, text := range []string{
		`{"t":"` + secret + `","seq":1}`,
		`{"t":"` + secret + `"}`,
		`{"t":"copy","seq":"` + secret + `"}`,
		`{"t":"key","seq":1,"key":"` + secret + `"}`,
		`{"t":"link","seq":1,"url":"` + secret + strings.Repeat("a", MaxLinkBytes) + `"}`,
		`{"t":"lightbox","seq":1,"index":0,"images":[{"src":"` + secret + `"}]}`,
		`{"t":"toast","seq":1,"kind":"` + secret + `","text":"x"}`,
		secret,
	} {
		_, err := ParseEvent(text)
		if err == nil || strings.Contains(err.Error(), secret) || len(err.Error()) > 200 {
			t.Errorf("错误文字不能回显页面内容：%v", err)
		}
	}
}

func TestViewMarshalShapes(t *testing.T) {
	cases := []struct {
		name string
		view View
		want string
	}{
		{"none", View{Kind: ViewNone}, `{"kind":"none"}`},
		{"original body", View{Kind: ViewOriginal, Content: "a<b>&", SourceKind: "body"}, `{"kind":"original","content":"a<b>&","sourceKind":"body"}`},
		{"original summary 空内容", View{Kind: ViewOriginal, SourceKind: "summary"}, `{"kind":"original","content":"","sourceKind":"summary"}`},
		{"markdown", View{Kind: ViewMarkdown, Content: "x", Tab: TabRecognized}, `{"kind":"markdown","content":"x","tab":"recognized"}`},
		{"replies", View{Kind: ViewReplies, Content: "{}"}, `{"kind":"replies","content":"{}"}`},
		{"gallery", View{Kind: ViewGallery, Content: "![a](local-image://a.png)"}, `{"kind":"gallery","content":"![a](local-image://a.png)"}`},
	}
	for _, c := range cases {
		data, err := json.Marshal(c.view)
		if err != nil {
			t.Errorf("%s：%v", c.name, err)
			continue
		}
		var got, want any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("%s：编码结果不是 JSON：%v", c.name, err)
		}
		_ = json.Unmarshal([]byte(c.want), &want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s：\n  实际 %s\n  期望 %s", c.name, data, c.want)
		}
	}
}

func TestViewMarshalRejectsMalformedViews(t *testing.T) {
	for name, view := range map[string]View{
		"未知种类":          {Kind: "weird"},
		"空种类":           {},
		"原文缺来源":         {Kind: ViewOriginal, Content: "x"},
		"原文来源未知":        {Kind: ViewOriginal, Content: "x", SourceKind: "other"},
		"markdown 缺标签":  {Kind: ViewMarkdown, Content: "x"},
		"markdown 标签未知": {Kind: ViewMarkdown, Content: "x", Tab: "other"},
	} {
		if _, err := json.Marshal(view); err == nil {
			t.Errorf("%s：应当拒绝编码", name)
		}
	}
}

func TestViewMessageEncodesNullRestoreAndKeepsOrderIndependentFields(t *testing.T) {
	text, err := encodeMessage(viewMessage{T: "view", Seq: 3, ItemID: "i", Tab: TabBody,
		View: View{Kind: ViewReplies, Content: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatal(err)
	}
	if v, present := m["restoreTop"]; !present || v != nil {
		t.Errorf("restoreTop 要显式为 null：%s", text)
	}
	for _, key := range []string{"t", "seq", "itemId", "tab", "view", "catalogOpen", "tocOpen"} {
		if _, present := m[key]; !present {
			t.Errorf("缺字段 %s：%s", key, text)
		}
	}
	if _, err := encodeMessage(viewMessage{View: View{Kind: "weird"}}); err == nil {
		t.Error("坏视图要让整条消息编码失败")
	}
}

func TestMessageContentSurvivesEncodingExactly(t *testing.T) {
	content := "含 \"引号\"、\\反斜杠、</script>、\u2028行分隔符、\x00空字节与 emoji 😀\n换行"
	text, err := encodeMessage(viewMessage{T: "view", Seq: 1, View: View{Kind: ViewMarkdown, Content: content, Tab: TabBody}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "</script>") {
		t.Error("编码结果不该含原样的 </script>")
	}
	var back struct{ View struct{ Content string } }
	if err := json.Unmarshal([]byte(text), &back); err != nil {
		t.Fatal(err)
	}
	if back.View.Content != content {
		t.Errorf("内容经编码往返后应当逐字节相同：%q", back.View.Content)
	}
}

func TestThemeValidate(t *testing.T) {
	ok := Theme{Dark: true, Hue: 0, Saturation: 0, BasePx: 8, Motion: MotionOff}
	if err := ok.Validate(); err != nil {
		t.Errorf("下界应当通过：%v", err)
	}
	ok.BasePx = 40
	if err := ok.Validate(); err != nil {
		t.Errorf("上界应当通过：%v", err)
	}
	for name, th := range map[string]Theme{
		"字号太小":     {BasePx: 7.9, Motion: MotionStandard},
		"字号太大":     {BasePx: 40.1, Motion: MotionStandard},
		"色相 NaN":   {Hue: math.NaN(), BasePx: 16, Motion: MotionStandard},
		"饱和度无穷":    {Saturation: math.Inf(1), BasePx: 16, Motion: MotionStandard},
		"字号无穷":     {BasePx: math.Inf(-1), Motion: MotionStandard},
		"动效未知":     {BasePx: 16, Motion: "fast"},
		"动效为空":     {BasePx: 16},
		"窗口太窄":     {BasePx: 16, Motion: MotionStandard, ViewportWidth: 99},
		"窗口太宽":     {BasePx: 16, Motion: MotionStandard, ViewportWidth: 20001},
		"窗口宽度 NaN": {BasePx: 16, Motion: MotionStandard, ViewportWidth: math.NaN()},
		"窗口宽度无穷":   {BasePx: 16, Motion: MotionStandard, ViewportWidth: math.Inf(1)},
	} {
		if th.Validate() == nil {
			t.Errorf("%s：应当不通过", name)
		}
	}
	for _, m := range []Motion{MotionStandard, MotionReduced, MotionOff} {
		if err := (Theme{BasePx: 16, Motion: m}).Validate(); err != nil {
			t.Errorf("动效 %s：%v", m, err)
		}
	}
	// 窗口宽度：0 表示不给，100 与 20000 是两个边界。
	for _, w := range []float64{0, 100, 800, 1280, 20000} {
		if err := (Theme{BasePx: 16, Motion: MotionStandard, ViewportWidth: w}).Validate(); err != nil {
			t.Errorf("窗口宽度 %v 应当通过：%v", w, err)
		}
	}
}

func TestThemeJSONCarriesTheWindowWidthOnlyWhenGiven(t *testing.T) {
	without, err := json.Marshal(Theme{BasePx: 16, Motion: MotionStandard})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "viewportWidth") {
		t.Errorf("没给窗口宽度时不该出现这个字段（页面据此取自身宽度）：%s", without)
	}
	with, err := json.Marshal(Theme{BasePx: 16, Motion: MotionStandard, ViewportWidth: 1280})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"viewportWidth":1280`) {
		t.Errorf("给了窗口宽度要带上：%s", with)
	}
}

func TestSafeLink(t *testing.T) {
	good := map[string]string{
		"https://example.com":              "https://example.com",
		"http://example.com/a b":           "http://example.com/a%20b",
		"https://example.com:8443/x?y=1#z": "https://example.com:8443/x?y=1#z",
	}
	for in, want := range good {
		got, ok := SafeLink(in)
		if !ok {
			t.Errorf("SafeLink(%q) 应当放行", in)
			continue
		}
		if got != want {
			t.Errorf("SafeLink(%q) = %q，期望 %q", in, got, want)
		}
	}
	if _, ok := SafeLink("https://例子.中国/路径?查询=1#片段"); !ok {
		t.Error("带非 ASCII 主机名与路径的 https 链接应当放行")
	}
	for _, in := range []string{
		"", "javascript:alert(1)", "JAVASCRIPT:alert(1)", "data:text/html,x", "file:///C:/x", "ftp://example.com", "mailto:a@b.c",
		"gzreader://doc/x", "//example.com", "example.com", "/relative", "https://", "https:///x", "http://user@example.com",
		"https://user:pw@example.com", "https://exa\nmple.com", "https://example.com/\r\nX: y", "https://example.com/a\tb",
		"https://example.com/\x00", "https://exa mple.com", "https://[::1", strings.Repeat("a", MaxLinkBytes+1),
	} {
		if got, ok := SafeLink(in); ok {
			t.Errorf("SafeLink(%.40q) 应当拒绝，实际放行 %q", in, got)
		}
	}
	if _, ok := SafeLink("https://example.com/" + strings.Repeat("a", MaxLinkBytes-len("https://example.com/"))); !ok {
		t.Error("正好等于上限的链接应当放行")
	}
}
