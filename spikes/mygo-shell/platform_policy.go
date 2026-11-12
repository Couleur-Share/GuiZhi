package main

// 登录态采集平台的网络与导航策略：对应 platform-capture/browser-capture-domains.ts 与 browser-capture-policy.ts。
// 平台浏览器只允许访问官方登录流程与页面运行必需的资源域；其余请求一律拦截。

import (
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

var platformIDs = []string{"xiaohongshu", "douyin", "linuxdo", "nodeseek"}

var platformLoginCookieNames = map[string][]string{
	"xiaohongshu": {"web_session", "web_session_v2"},
	"douyin":      {"LOGIN_STATUS", "sessionid", "sessionid_ss", "sid_guard", "sid_ucp_virtual"},
	"linuxdo":     {"_t"},
	"nodeseek":    {}, // 验证成功以实际帖子正文为准，不以 Cookie 存在代替
}

var platformLoginFlowDomains = map[string][]string{
	"xiaohongshu": {"xiaohongshu.com"},
	"douyin":      {"douyin.com", "iesdouyin.com", "zijieapi.com", "snssdk.com", "rmc.bytedance.com"},
	"nodeseek":    {"nodeseek.com", "challenges.cloudflare.com"},
	"linuxdo":     {"linux.do", "cloudflare.com", "challenges.cloudflare.com", "github.com", "google.com"},
}

var platformResourceDomains = map[string][]string{
	"xiaohongshu": {"xhscdn.com", "xhsimg.com"},
	"nodeseek":    {"nodeseek.com", "challenges.cloudflare.com"},
	"douyin": {
		// 只放行页面运行必需的字节官方资源域；遥测域刻意不在白名单内
		"douyinstatic.com", "bytetos.com", "bytescm.com", "bytegoofy.com", "douyinvod.com", "douyinpic.com",
		"byteimg.com", "byted-static.com", "zijieapi.com", "snssdk.com",
		"lf-ucenter-web.yhgfb-cn-static.com",
		"lf-rc1.yhgfb-cn-static.com", "lf-rc2.yhgfb-cn-static.com", "lf-headquarters-speed.yhgfb-cn-static.com",
		"ttwid.bytedance.com",
	},
	"linuxdo": {
		"linux.do", "cdn.linux.do", "cloudflare.com", "challenges.cloudflare.com", "cloudflareinsights.com",
		"cf-assets.net", "discourse.org", "discourse-cdn.com", "amazonaws.com", "googleapis.com", "gstatic.com",
		"jsdelivr.net", "unpkg.com", "hcaptcha.com",
	},
}

func hostInDomains(host string, domains []string) bool {
	host = strings.ToLower(host)
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func isSecureProtocol(p string) bool { return p == "https:" || p == "wss:" }

// platformPolicy 汇总平台 URL 判定；生产用真实域名表，测试可整体替换为本地模拟站点。
type platformPolicy struct {
	allowedPlatformURL func(platform, raw string) bool
	officialFlowURL    func(platform, raw string) bool
	resourceURL        func(platform, raw string) bool
	loginURL           func(platform string) string
}

func defaultPlatformPolicy() platformPolicy {
	return platformPolicy{
		allowedPlatformURL: isAllowedPlatformURL,
		officialFlowURL:    isOfficialLoginFlowURL,
		resourceURL:        isAllowedBrowserResourceURL,
		loginURL:           platformLoginURL,
	}
}

// isOfficialLoginFlowURL 对应 isOfficialLoginFlowUrl。
func isOfficialLoginFlowURL(platform, raw string) bool {
	if platform == "nodeseek" {
		if isAllowedPlatformURL(platform, raw) {
			return true
		}
		u, err := whatwg.Parse(raw)
		return err == nil && u.Protocol() == "https:" && u.Hostname() == "challenges.cloudflare.com" &&
			u.Port() == "" && u.Username() == "" && u.Password() == ""
	}
	if isAllowedPlatformURL(platform, raw) {
		return true
	}
	u, err := whatwg.Parse(raw)
	if err != nil || !isSecureProtocol(u.Protocol()) {
		return false
	}
	return hostInDomains(u.Hostname(), platformLoginFlowDomains[platform])
}

var reInlineScheme = func(raw string) bool {
	l := strings.ToLower(raw)
	return strings.HasPrefix(l, "about:blank") || strings.HasPrefix(l, "data:") || strings.HasPrefix(l, "blob:")
}

// isAllowedBrowserResourceURL 对应 isAllowedBrowserResourceUrl。
func isAllowedBrowserResourceURL(platform, raw string) bool {
	if reInlineScheme(raw) {
		return true
	}
	if platform == "nodeseek" {
		return isOfficialLoginFlowURL(platform, raw)
	}
	if platform == "linuxdo" {
		u, err := whatwg.Parse(raw)
		return err == nil && isSecureProtocol(u.Protocol())
	}
	if isOfficialLoginFlowURL(platform, raw) {
		return true
	}
	u, err := whatwg.Parse(raw)
	if err != nil || !isSecureProtocol(u.Protocol()) {
		return false
	}
	return hostInDomains(u.Hostname(), platformResourceDomains[platform])
}

// shouldBlockLoginPageRequest 对应 shouldBlockLoginPageRequest：登录窗口不加载信息流音视频与抖音推荐封面。
func shouldBlockLoginPageRequest(platform, raw, resourceType string) bool {
	if resourceType == "media" {
		return true
	}
	if platform != "douyin" {
		return false
	}
	u, err := whatwg.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if resourceType == "image" && (host == "douyinpic.com" || strings.HasSuffix(host, ".douyinpic.com")) {
		return true
	}
	return reDouyinFeedPath.MatchString(u.Pathname())
}

// platformLoginURL 对应 platformLoginUrl。
func platformLoginURL(platform string) string {
	switch platform {
	case "nodeseek":
		return "https://www.nodeseek.com/"
	case "xiaohongshu":
		return "https://www.xiaohongshu.com/explore"
	case "linuxdo":
		return "https://linux.do/login"
	}
	return "https://www.douyin.com/"
}

func isPlatformCapturePlatform(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	for _, p := range platformIDs {
		if p == s {
			return s, true
		}
	}
	return "", false
}

var reDouyinFeedPath = regexp.MustCompile(`(?i)^/aweme/v1/web/(?:tab|follow)/feed/`)
