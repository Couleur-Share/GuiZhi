package services

// 来源平台识别：对应 packages/shared/utils/{video-platforms,forum-platforms,source-platforms}.ts。
// 判定完全由 URL 决定。
//
// 桌面外壳（main 包）的 source_platforms.go 里有同一份判定（由 make-golden.mts 的 importUtil 基准逐例对拍）；
// main 包不能被界面层引用，所以这里留一份给原生界面（任务详情的平台图标与名称）。两处一起改，
// source_platform_test.go 的用例与外壳侧的对拍用例是同一批输入（台账登记：重复实现的代价与收敛时机见 B-018）。

import (
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

// isHostOrSubdomain 报告 hostname 是否等于某个域名或它的子域名。
func isHostOrSubdomain(hostname string, domains ...string) bool {
	for _, d := range domains {
		if hostname == d || strings.HasSuffix(hostname, "."+d) {
			return true
		}
	}
	return false
}

// detectVideoPlatform 对应 detectVideoPlatform；认不出返回 ""。
func detectVideoPlatform(raw string) string {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return ""
	}
	host, path := strings.ToLower(u.Hostname()), u.Pathname()
	switch {
	case host == "b23.tv":
		return "bilibili"
	case strings.HasSuffix(host, "bilibili.com"):
		if strings.HasPrefix(path, "/video/") {
			return "bilibili"
		}
		return ""
	case host == "youtu.be":
		return "youtube"
	case strings.HasSuffix(host, "youtube.com"):
		if strings.HasPrefix(path, "/watch") || strings.HasPrefix(path, "/shorts/") {
			return "youtube"
		}
		return ""
	case isHostOrSubdomain(host, "douyin.com", "iesdouyin.com"):
		return "douyin"
	case isHostOrSubdomain(host, "xiaohongshu.com", "xhslink.com", "xhslink.cn"):
		return "xiaohongshu"
	}
	return ""
}

var (
	v2exTopicRe      = regexp.MustCompile(`^/t/(\d+)`)
	ngaReadRe        = regexp.MustCompile(`(?i)/read\.php$`)
	digitsOnlyRe     = regexp.MustCompile(`^\d+$`)
	discourseTopicRe = regexp.MustCompile(`^/t/(?:topic/)?(?:[^/]+/)?(\d+)`)
	twolibraPostRe   = regexp.MustCompile(`^/post/([^/]+)/([A-Za-z0-9_-]{7})/?$`)
	nodeseekTopicRe  = regexp.MustCompile(`^/post-([1-9]\d*)-[1-9]\d*/?$`)
)

func isNgaHostname(h string) bool {
	return h == "bbs.nga.cn" || strings.HasSuffix(h, ".bbs.nga.cn") || h == "ngabbs.com" || strings.HasSuffix(h, ".ngabbs.com") ||
		h == "nga.178.com" || strings.HasSuffix(h, ".nga.178.com")
}

// detectForumPlatform 对应 detectForumPlatform 的平台部分；认不出返回 ""。
func detectForumPlatform(raw string) string {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return ""
	}
	host, path := strings.ToLower(u.Hostname()), u.Pathname()
	if host == "v2ex.com" || strings.HasSuffix(host, ".v2ex.com") {
		if v2exTopicRe.MatchString(path) {
			return "v2ex"
		}
		return ""
	}
	if host == "nodeseek.com" || host == "www.nodeseek.com" {
		if nodeseekTopicRe.MatchString(path) {
			return "nodeseek"
		}
	}
	if isNgaHostname(host) && ngaReadRe.MatchString(path) {
		tid := strings.TrimFunc(u.SearchParams().Get("tid"), isJSSpace)
		if digitsOnlyRe.MatchString(tid) {
			return "nga"
		}
	}
	if host == "linux.do" || strings.HasSuffix(host, ".linux.do") {
		if discourseTopicRe.MatchString(path) {
			return "linuxdo"
		}
	}
	if host == "meta.appinn.net" {
		if discourseTopicRe.MatchString(path) {
			return "appinn"
		}
	}
	if host == "2libra.com" || host == "www.2libra.com" {
		if twolibraPostRe.MatchString(path) {
			return "twolibra"
		}
	}
	return ""
}

// DetectVideoPlatform 是 detectVideoPlatform 的导出：认不出返回 ""。详情据此判断在线视频能不能转写。
func DetectVideoPlatform(raw string) string { return detectVideoPlatform(raw) }

// DetectForumPlatform 是 detectForumPlatform 的导出：认不出返回 ""。详情据此判断论坛讨论能不能刷新。
func DetectForumPlatform(raw string) string { return detectForumPlatform(raw) }

// sourceCommentsDomains 是能采集来源评论的平台与域名：platform-capture.ts 的 PLATFORM_DOMAINS 里抖音与小红书两项。
var sourceCommentsDomains = []struct {
	platform string
	domains  []string
}{
	{"xiaohongshu", []string{"xiaohongshu.com", "xhslink.com", "xhslink.cn"}},
	{"douyin", []string{"douyin.com", "iesdouyin.com"}},
}

// DetectSourceCommentsPlatform 对应 detectSourceCommentsPlatform：独立来源评论只对抖音与小红书开放，
// 且必须是 https 链接、域名是平台域名或其子域；认不出返回 ""。
// 重复实现的登记见文件头（B-018）：与 main 包 detectPlatformCapturePlatform 的这两项判定一致。
func DetectSourceCommentsPlatform(raw string) string {
	u, err := whatwg.Parse(raw)
	if err != nil || u.Protocol() != "https:" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range sourceCommentsDomains {
		if isHostOrSubdomain(host, p.domains...) {
			return p.platform
		}
	}
	return ""
}

// ResolveSourcePlatform 判定条目（任务）的来源平台，对应 resolveSourcePlatform：
// 文件来源是 local；网址来源按域名与路径归到具体平台，认不出的是 web；手工粘贴的文本没有来源（ok 为假）；
// 解析不出来的网址也不算网页（旧版迁移带进来的 source_uri 什么都可能是）。
func ResolveSourcePlatform(sourceKind, sourceURI string) (platform string, ok bool) {
	if sourceKind == "file" {
		return "local", true
	}
	if sourceKind != "url" || sourceURI == "" {
		return "", false
	}
	u, err := whatwg.Parse(sourceURI)
	if err != nil {
		return "", false
	}
	if u.Hostname() == "mp.weixin.qq.com" {
		return "wechat", true
	}
	if v := detectVideoPlatform(sourceURI); v != "" {
		return v, true
	}
	if f := detectForumPlatform(sourceURI); f != "" {
		return f, true
	}
	return "web", true
}
