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

// DetectForumTarget 返回平台名与帖子标识：各论坛的标识提取、NGA tid 的 ECMAScript trim 与解析边界。
func TestDetectForumTarget(t *testing.T) {
	cases := []struct{ raw, platform, topic string }{
		{"https://www.v2ex.com/t/12345#reply3", "v2ex", "12345"},
		{"https://v2ex.com/t/9?p=2", "v2ex", "9"},
		{"https://www.nodeseek.com/post-123-1", "nodeseek", "123"},
		{"https://nodeseek.com/post-45-2/", "nodeseek", "45"},
		{"https://www.nodeseek.com/post-0-1", "", ""},
		{"https://bbs.nga.cn/read.php?tid=12345", "nga", "12345"},
		{"https://nga.178.com/read.php?x=1&tid=777", "nga", "777"},
		{"https://ngabbs.com/READ.PHP?tid=8", "nga", "8"},
		{"https://bbs.nga.cn/read.php?tid=%20123%20", "nga", "123"},       // 首尾空白被 trim
		{"https://bbs.nga.cn/read.php?tid=+123", "nga", "123"},            // + 解码为空格后被 trim
		{"https://bbs.nga.cn/read.php?tid=%C2%A0123%C2%A0", "nga", "123"}, // NBSP 属于 ECMAScript 空白
		{"https://bbs.nga.cn/read.php?tid=%EF%BB%BF12", "nga", "12"},      // U+FEFF 属于 ECMAScript 空白
		{"https://bbs.nga.cn/read.php?tid=%E3%80%80123", "nga", "123"},    // 全角空格
		{"https://bbs.nga.cn/read.php?tid=123&tid=456", "nga", "123"},     // 重复参数取第一个
		{"https://bbs.nga.cn/read.php?tid=%C2%8512", "", ""},              // U+0085 不是 ECMAScript 空白
		{"https://bbs.nga.cn/read.php?tid=%FF", "", ""},
		{"https://bbs.nga.cn/read.php?tid=1_2", "", ""},
		{"https://bbs.nga.cn/read.php?tid=", "", ""},
		{"https://bbs.nga.cn/read.php", "", ""},
		{"https://bbs.nga.cn/thread.php?tid=1", "", ""},
		{"https://linux.do/t/topic/12345", "linuxdo", "12345"},
		{"https://linux.do/t/some-slug/678/2", "linuxdo", "678"},
		{"https://www.linux.do/t/5", "linuxdo", "5"},
		{"https://meta.appinn.net/t/topic/9", "appinn", "9"},
		{"https://2libra.com/post/tech/AbC_123", "twolibra", "AbC_123"},
		{"https://www.2libra.com/post/tech/AbC_123/", "twolibra", "AbC_123"},
		{"https://2libra.com/post/tech/abc", "", ""},
		{"https://www.v2ex.com/go/qna", "", ""},
		{"https://example.com/t/1", "", ""},
		{"not a url", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		got := DetectForumTarget(c.raw)
		if c.platform == "" {
			if got != nil {
				t.Errorf("DetectForumTarget(%q) = %+v，期望 nil", c.raw, *got)
			}
			if p := DetectForumPlatform(c.raw); p != "" {
				t.Errorf("DetectForumPlatform(%q) = %q，期望空", c.raw, p)
			}
			continue
		}
		if got == nil || got.Platform != c.platform || got.TopicID != c.topic {
			t.Errorf("DetectForumTarget(%q) = %+v，期望 {%s %s}", c.raw, got, c.platform, c.topic)
			continue
		}
		if p := DetectForumPlatform(c.raw); p != c.platform {
			t.Errorf("DetectForumPlatform(%q) = %q，期望 %q（与 DetectForumTarget 不一致）", c.raw, p, c.platform)
		}
	}
}

// IsAllowedPlatformURL：必须 https、域名是平台域名或子域；NodeSeek 额外拒绝端口、用户名密码与非 nodeseek.com / www 主机。
func TestIsAllowedPlatformURL(t *testing.T) {
	cases := []struct {
		platform, raw string
		want          bool
	}{
		{"xiaohongshu", "https://www.xiaohongshu.com/explore/1", true},
		{"xiaohongshu", "https://xhslink.com/abc", true},
		{"xiaohongshu", "https://XHSLINK.CN/abc", true},
		{"xiaohongshu", "http://www.xiaohongshu.com/explore/1", false},
		{"xiaohongshu", "https://evilxiaohongshu.com/", false},
		{"xiaohongshu", "https://xiaohongshu.com.evil.com/", false},
		{"douyin", "https://v.douyin.com/abc/", true},
		{"douyin", "https://www.iesdouyin.com/share/video/1", true},
		{"douyin", "https://www.xiaohongshu.com/", false},
		{"linuxdo", "https://linux.do/t/1", true},
		{"linuxdo", "https://meta.linux.do/t/1", true},
		{"linuxdo", "https://notlinux.do/t/1", false},
		{"linuxdo", "http://linux.do/t/1", false},
		{"nodeseek", "https://www.nodeseek.com/post-1-1", true},
		{"nodeseek", "https://nodeseek.com/post-1-1", true},
		{"nodeseek", "https://www.nodeseek.com:8443/post-1-1", false},
		{"nodeseek", "https://user@www.nodeseek.com/post-1-1", false},
		{"nodeseek", "https://user:pw@nodeseek.com/", false},
		{"nodeseek", "https://api.nodeseek.com/post-1-1", false}, // 子域在 NodeSeek 上不放行
		{"nodeseek", "http://www.nodeseek.com/post-1-1", false},
		{"nodeseek", "https://www.nodeseek.com.evil.com/", false},
		{"unknown", "https://www.douyin.com/", false},
		{"douyin", "not a url", false},
		{"douyin", "", false},
	}
	for _, c := range cases {
		if got := IsAllowedPlatformURL(c.platform, c.raw); got != c.want {
			t.Errorf("IsAllowedPlatformURL(%q, %q) = %v，期望 %v", c.platform, c.raw, got, c.want)
		}
	}
}

// DetectPlatformCapturePlatform 覆盖四个登录态平台；DetectSourceCommentsPlatform 只取其中抖音与小红书。
func TestDetectPlatformCapturePlatform(t *testing.T) {
	cases := []struct{ raw, capture, comments string }{
		{"https://www.xiaohongshu.com/explore/1", "xiaohongshu", "xiaohongshu"},
		{"https://xhslink.cn/a", "xiaohongshu", "xiaohongshu"},
		{"https://v.douyin.com/abc/", "douyin", "douyin"},
		{"https://linux.do/t/topic/1", "linuxdo", ""},
		{"https://www.nodeseek.com/post-1-1", "nodeseek", ""},
		{"https://www.nodeseek.com:8443/post-1-1", "", ""},
		{"http://www.douyin.com/video/1", "", ""},
		{"https://evildouyin.com/", "", ""},
		{"https://example.com/", "", ""},
		{"not a url", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := DetectPlatformCapturePlatform(c.raw); got != c.capture {
			t.Errorf("DetectPlatformCapturePlatform(%q) = %q，期望 %q", c.raw, got, c.capture)
		}
		if got := DetectSourceCommentsPlatform(c.raw); got != c.comments {
			t.Errorf("DetectSourceCommentsPlatform(%q) = %q，期望 %q", c.raw, got, c.comments)
		}
	}
}
