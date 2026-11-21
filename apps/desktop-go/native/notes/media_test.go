package notes

import (
	"reflect"
	"testing"
)

// 资产引用：取第一个、文件名到第一个字符集外的字符为止；协议不对、没有、协议名未知都是没有。
func TestLocalAssetRef(t *testing.T) {
	cases := []struct {
		content, protocol, want string
		ok                      bool
	}{
		{"local-video://clip_01.mp4\n\n正文", LocalVideo, "clip_01.mp4", true},
		{"![图](local-image://a-b.png) 与 local-image://c.png", LocalImage, "a-b.png", true},
		{"local-video://my-clip.mp4", LocalVideo, "my-clip.mp4", true}, // 连字符与点都在文件名字符集里
		{"local-video://a b.mp4", LocalVideo, "a", true},
		{"local-video://带中文.mp4", LocalVideo, "", false},
		{"local-video://", LocalVideo, "", false},
		{"local-image://x.png", LocalVideo, "", false},
		{"没有引用", LocalVideo, "", false},
		{"", LocalImage, "", false},
		{"local-video://x.mp4", "local-audio", "", false},
		{"xlocal-video://y.mp4", LocalVideo, "y.mp4", true}, // 与 JS 一样不要求前面是边界
		{"local-video://../up.mp4", LocalVideo, "..", true}, // 「只判断有没有」，不做安全过滤
	}
	for _, c := range cases {
		got, ok := LocalAssetRef(c.content, c.protocol)
		if got != c.want || ok != c.ok {
			t.Errorf("LocalAssetRef(%q, %q) = %q, %v，期望 %q, %v", c.content, c.protocol, got, ok, c.want, c.ok)
		}
	}
}

// 总结小节：标题行整行（去首尾空白，含 \r）等于两个固定标题之一才算；正文里提到、三级标题、缩进一个字符以外的变体都不算。
func TestHasMediaSummarySection(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"## 视频总结\n\n内容", true},
		{"开头\n## 音频总结\r\n内容", true},
		{"  ## 视频总结  \n", true},
		{"\u3000## 视频总结\u3000", true}, // 全角空格也是 JS trim 的空白
		{"### 视频总结", false},
		{"## 视频总结：详情", false},
		{"这里提到 ## 视频总结 但不是标题行", false},
		{"## 总结", false},
		{"", false},
	}
	for _, c := range cases {
		if got := HasMediaSummarySection(c.content); got != c.want {
			t.Errorf("HasMediaSummarySection(%q) = %v，期望 %v", c.content, got, c.want)
		}
	}
}

func TestExtractLocalAssetRefs(t *testing.T) {
	content := "![a](local-image://a.png)\n![dup](local-image://a.png)\n![](local-image://..)\n![](local-image://.hidden.png)\n![b](local-image://b-2.jpg)"
	if got, want := ExtractLocalAssetRefs(content, LocalImage), []string{"a.png", "b-2.jpg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractLocalAssetRefs = %v，期望 %v", got, want)
	}
	if got := ExtractLocalAssetRefs("", LocalImage); got != nil {
		t.Fatalf("空串应返回 nil：%v", got)
	}
}

func TestBuildOcrSectionBodyAndApplyOcrTextToContent(t *testing.T) {
	if got := BuildOcrSectionBody([]string{"单图文字"}); got != "单图文字" {
		t.Fatalf("单图 = %q", got)
	}
	if got := BuildOcrSectionBody([]string{""}); got != "" {
		t.Fatalf("单图空 = %q", got)
	}
	if got, want := BuildOcrSectionBody([]string{"第一张", "", "第三张"}), "### 图 1\n\n第一张\n\n### 图 3\n\n第三张"; got != want {
		t.Fatalf("多图 = %q，期望 %q", got, want)
	}
	if got := BuildOcrSectionBody([]string{"", ""}); got != "" {
		t.Fatalf("全空 = %q", got)
	}

	if got, want := ApplyOcrTextToContent("文案  \n ", "新字"), "文案\n\n## 图中文字\n\n新字"; got != want {
		t.Fatalf("追加 = %q，期望 %q", got, want)
	}
	if got, want := ApplyOcrTextToContent("文案\n\n## 图中文字\n\n旧字", "新字"), "文案\n\n## 图中文字\n\n新字"; got != want {
		t.Fatalf("替换 = %q，期望 %q", got, want)
	}
}
