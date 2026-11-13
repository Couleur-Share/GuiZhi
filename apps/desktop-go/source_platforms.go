package main

// 对应 shared/utils/{video-platforms,forum-platforms,source-platforms,platform-capture,web-verification}.ts：
// 来源平台识别。判定完全由 URL 决定，由 make-golden.mts 的 importUtil 基准逐例对拍。

import (
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

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

// forumTarget 对应 ForumTarget。
type forumTarget struct{ Platform, TopicID string }

func isNgaHostname(h string) bool {
	return h == "bbs.nga.cn" || strings.HasSuffix(h, ".bbs.nga.cn") || h == "ngabbs.com" || strings.HasSuffix(h, ".ngabbs.com") ||
		h == "nga.178.com" || strings.HasSuffix(h, ".nga.178.com")
}

// detectForumPlatform 对应 detectForumPlatform。
func detectForumPlatform(raw string) *forumTarget {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return nil
	}
	host, path := strings.ToLower(u.Hostname()), u.Pathname()
	if host == "v2ex.com" || strings.HasSuffix(host, ".v2ex.com") {
		if m := v2exTopicRe.FindStringSubmatch(path); m != nil {
			return &forumTarget{"v2ex", m[1]}
		}
		return nil
	}
	if host == "nodeseek.com" || host == "www.nodeseek.com" {
		if m := nodeseekTopicRe.FindStringSubmatch(path); m != nil {
			return &forumTarget{"nodeseek", m[1]}
		}
	}
	if isNgaHostname(host) && ngaReadRe.MatchString(path) {
		tid, _ := searchParamGet(u, "tid")
		tid = jsTrim(tid)
		if digitsOnlyRe.MatchString(tid) {
			return &forumTarget{"nga", tid}
		}
	}
	if host == "linux.do" || strings.HasSuffix(host, ".linux.do") {
		if m := discourseTopicRe.FindStringSubmatch(path); m != nil {
			return &forumTarget{"linuxdo", m[1]}
		}
	}
	if host == "meta.appinn.net" {
		if m := discourseTopicRe.FindStringSubmatch(path); m != nil {
			return &forumTarget{"appinn", m[1]}
		}
	}
	if host == "2libra.com" || host == "www.2libra.com" {
		if m := twolibraPostRe.FindStringSubmatch(path); m != nil {
			return &forumTarget{"twolibra", m[2]}
		}
	}
	return nil
}

// resolveSourcePlatform 对应 resolveSourcePlatform；null 用 ok=false 表示。
func resolveSourcePlatform(sourceKind string, sourceURI *string) (string, bool) {
	if sourceKind == "file" {
		return "local", true
	}
	if sourceKind != "url" || sourceURI == nil || *sourceURI == "" {
		return "", false
	}
	u, err := whatwg.Parse(*sourceURI)
	if err != nil {
		return "", false
	}
	if u.Hostname() == "mp.weixin.qq.com" {
		return "wechat", true
	}
	if v := detectVideoPlatform(*sourceURI); v != "" {
		return v, true
	}
	if f := detectForumPlatform(*sourceURI); f != nil {
		return f.Platform, true
	}
	return "web", true
}

// ---- 登录态采集平台（platform-capture.ts / web-verification.ts） ----

var platformCaptureDomains = []struct {
	Name    string
	Domains []string
}{
	{"xiaohongshu", []string{"xiaohongshu.com", "xhslink.com", "xhslink.cn"}},
	{"douyin", []string{"douyin.com", "iesdouyin.com"}},
	{"linuxdo", []string{"linux.do"}},
	{"nodeseek", []string{"nodeseek.com"}},
}

func isAllowedPlatformURL(platform, raw string) bool {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return false
	}
	if platform == "nodeseek" && (u.Username() != "" || u.Password() != "" || u.Port() != "" ||
		(u.Hostname() != "nodeseek.com" && u.Hostname() != "www.nodeseek.com")) {
		return false
	}
	if u.Protocol() != "https:" {
		return false
	}
	for _, p := range platformCaptureDomains {
		if p.Name != platform {
			continue
		}
		for _, d := range p.Domains {
			host := strings.ToLower(u.Hostname())
			if host == d || strings.HasSuffix(host, "."+d) {
				return true
			}
		}
	}
	return false
}

// detectPlatformCapturePlatform 对应同名函数；认不出返回 ""。
func detectPlatformCapturePlatform(raw string) string {
	for _, p := range platformCaptureDomains {
		if isAllowedPlatformURL(p.Name, raw) {
			return p.Name
		}
	}
	return ""
}

var webVerifyTitleRe = regexp.MustCompile(`(?i)^(just a moment|verify you are human|人机验证|安全验证)`)

// needsWebVerification 对应 needsWebVerification（task 是 mapImportTaskRow 的结果）。
func needsWebVerification(task map[string]any) bool {
	str := func(k string) string { s, _ := task[k].(string); return s }
	if str("status") != "failed" || str("sourceKind") != "url" || detectPlatformCapturePlatform(str("sourceInput")) != "" {
		return false
	}
	return strings.Contains(str("error"), "web_verification_required") ||
		webVerifyTitleRe.MatchString(str("displayName")) ||
		strings.Contains(str("error"), "网页要求完成验证码")
}
