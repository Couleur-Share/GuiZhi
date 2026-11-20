package notes

import "testing"

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
