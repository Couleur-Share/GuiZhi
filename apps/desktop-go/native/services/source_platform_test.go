package services

import "testing"

// 与 main 包 source_platforms.go 的判定逐例一致：同一批输入，两处结论必须相同。
func TestResolveSourcePlatform(t *testing.T) {
	cases := []struct {
		name, kind, uri string
		want            string
		ok              bool
	}{
		{"file-is-local", "file", `D:\资料\季度复盘.pdf`, "local", true},
		{"file-ignores-uri", "file", "", "local", true},
		{"text-has-no-source", "text", "今天读到的一段话", "", false},
		{"url-empty", "url", "", "", false},
		{"url-unparsable", "url", "not a url", "", false},
		{"wechat", "url", "https://mp.weixin.qq.com/s/abc", "wechat", true},
		{"bilibili-video", "url", "https://www.bilibili.com/video/BV1xx", "bilibili", true},
		{"bilibili-short-link", "url", "https://b23.tv/abc", "bilibili", true},
		{"bilibili-non-video-path", "url", "https://www.bilibili.com/read/cv1", "web", true},
		{"youtube-watch", "url", "https://www.youtube.com/watch?v=dQw4", "youtube", true},
		{"youtube-shorts", "url", "https://www.youtube.com/shorts/abc", "youtube", true},
		{"youtube-short-link", "url", "https://youtu.be/abc", "youtube", true},
		{"youtube-channel-is-web", "url", "https://www.youtube.com/@someone", "web", true},
		{"douyin", "url", "https://v.douyin.com/abc/", "douyin", true},
		{"douyin-iesdouyin", "url", "https://www.iesdouyin.com/share/video/1", "douyin", true},
		{"xiaohongshu", "url", "https://www.xiaohongshu.com/explore/1", "xiaohongshu", true},
		{"xiaohongshu-short", "url", "https://xhslink.com/abc", "xiaohongshu", true},
		{"v2ex-topic", "url", "https://www.v2ex.com/t/12345", "v2ex", true},
		{"v2ex-non-topic", "url", "https://www.v2ex.com/go/qna", "web", true},
		{"nodeseek-post", "url", "https://www.nodeseek.com/post-123-1", "nodeseek", true},
		{"nodeseek-non-post", "url", "https://www.nodeseek.com/categories/tech", "web", true},
		{"nga-read", "url", "https://bbs.nga.cn/read.php?tid=12345", "nga", true},
		{"nga-read-bad-tid", "url", "https://bbs.nga.cn/read.php?tid=abc", "web", true},
		{"nga-read-no-tid", "url", "https://bbs.nga.cn/read.php", "web", true},
		{"linuxdo-topic", "url", "https://linux.do/t/topic/12345", "linuxdo", true},
		{"linuxdo-slug-topic", "url", "https://linux.do/t/some-slug/12345/2", "linuxdo", true},
		{"appinn-topic", "url", "https://meta.appinn.net/t/topic/9", "appinn", true},
		{"twolibra-post", "url", "https://2libra.com/post/tech/AbC_123", "twolibra", true},
		{"twolibra-short-id-is-web", "url", "https://2libra.com/post/tech/abc", "web", true},
		{"plain-web", "url", "https://example.com/blog/local-first", "web", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveSourcePlatform(tc.kind, tc.uri)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("ResolveSourcePlatform(%q, %q) = (%q, %v)，期望 (%q, %v)", tc.kind, tc.uri, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// 独立来源评论只对抖音与小红书的 https 链接开放：http、仿冒域名、其余登录态平台（Linux.do、NodeSeek）、解析不出来的都是空。
func TestDetectSourceCommentsPlatform(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"https://www.xiaohongshu.com/explore/1", "xiaohongshu"},
		{"https://edith.xiaohongshu.com/a", "xiaohongshu"},
		{"https://xhslink.com/abc", "xiaohongshu"},
		{"https://xhslink.cn/a", "xiaohongshu"},
		{"https://www.douyin.com/video/1", "douyin"},
		{"https://v.douyin.com/abc/", "douyin"},
		{"https://www.iesdouyin.com/share/video/1", "douyin"}, // 子域也算，与 JS 的 isHostOrSubdomain 一致
		{"https://WWW.DOUYIN.COM/video/1", "douyin"},
		{"https://www.douyin.com:8443/video/1", "douyin"},
		{"http://www.douyin.com/video/1", ""},
		{"https://evildouyin.com/", ""},
		{"https://douyin.com.evil.com/", ""},
		{"https://linux.do/t/1", ""},
		{"https://www.nodeseek.com/post-1-1", ""},
		{"https://example.com/", ""},
		{"not a url", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := DetectSourceCommentsPlatform(c.raw); got != c.want {
			t.Errorf("DetectSourceCommentsPlatform(%q) = %q，期望 %q", c.raw, got, c.want)
		}
	}
}

// 导出的两个判定与内部同名函数是同一份逻辑：认得的给平台名，认不出是空。
func TestExportedPlatformDetectors(t *testing.T) {
	if got := DetectVideoPlatform("https://www.bilibili.com/video/BV1xx"); got != "bilibili" {
		t.Errorf("DetectVideoPlatform = %q", got)
	}
	if got := DetectVideoPlatform("https://example.com/video/1"); got != "" {
		t.Errorf("DetectVideoPlatform(非视频站) = %q", got)
	}
	if got := DetectForumPlatform("https://linux.do/t/topic/12345"); got != "linuxdo" {
		t.Errorf("DetectForumPlatform = %q", got)
	}
	if got := DetectForumPlatform("https://linux.do/about"); got != "" {
		t.Errorf("DetectForumPlatform(非帖子) = %q", got)
	}
}
