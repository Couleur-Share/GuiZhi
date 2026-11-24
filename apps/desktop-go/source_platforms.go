package main

// 对应 shared/utils/{video-platforms,forum-platforms,source-platforms,platform-capture,web-verification}.ts：
// 来源平台识别。判定完全由 URL 决定，由 make-golden.mts 的 importUtil 基准逐例对拍。
//
// 判定的唯一实现在 native/services/source_platform.go（B-018 已收敛）：这里只是保留 main 包内原有函数签名的
// 转发包装，让 IPC 处理器与对拍测试不必改调用点。要改判定，改 services 那一份。

import (
	"regexp"
	"strings"

	"guizhi-mygo-shell/native/services"
)

// detectVideoPlatform 对应 detectVideoPlatform；认不出返回 ""。
func detectVideoPlatform(raw string) string { return services.DetectVideoPlatform(raw) }

// forumTarget 对应 ForumTarget。
type forumTarget = services.ForumTarget

// detectForumPlatform 对应 detectForumPlatform。
func detectForumPlatform(raw string) *forumTarget { return services.DetectForumTarget(raw) }

// resolveSourcePlatform 对应 resolveSourcePlatform；null 用 ok=false 表示。
func resolveSourcePlatform(sourceKind string, sourceURI *string) (string, bool) {
	if sourceURI == nil {
		return services.ResolveSourcePlatform(sourceKind, "")
	}
	return services.ResolveSourcePlatform(sourceKind, *sourceURI)
}

// isAllowedPlatformURL 对应同名函数（登录态采集平台的 https 与域名校验）。
func isAllowedPlatformURL(platform, raw string) bool { return services.IsAllowedPlatformURL(platform, raw) }

// detectPlatformCapturePlatform 对应同名函数；认不出返回 ""。
func detectPlatformCapturePlatform(raw string) string { return services.DetectPlatformCapturePlatform(raw) }

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
