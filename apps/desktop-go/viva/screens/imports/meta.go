// Package imports 是「采集」模块（渲染层 components/imports/*）：任务队列、筛选、批量处理、清理确认与任务详情。
//
// 本文件是 import-task-meta.ts 与 packages/shared 里相关工具函数的对应物：阶段与状态文案、耗时口径、
// 来源标记、错误与警告的可读化、登录态重试与网页验证的判定。它们是纯函数，不依赖界面，便于逐条对照渲染层的 vitest 用例。
//
// Package imports is the capture queue module of the Viva UI.
package imports

import (
	"regexp"
	"strconv"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// StallThresholdMs：单个阶段超过这个时长仍无进展，行内给出「本阶段已 x」提示。
const StallThresholdMs int64 = 90_000

// StageLabel 取子阶段文案。视频链路会跑元数据 → 下载 → 转码 → 转写 → 排版 → 总结六步，全程可达几十分钟，
// 只显示「抓取中」用户无法判断是在推进还是卡死了；这里刻意不给「第 N 步 / 共 M 步」（实际步数取决于运行时条件）。
// 未知阶段与空阶段回落到「抓取中」，与渲染层 getStageLabel 一致。
func StageLabel(stage string) string {
	switch stage {
	case "web-preparing":
		return locales.ImportsStageWebPreparing()
	case "web-verifying":
		return locales.ImportsStageWebVerifying()
	case "extracting":
		return locales.ImportsStageExtracting()
	case "saving":
		return locales.ImportsStageSaving()
	case "video-metadata":
		return locales.ImportsStageVideoMetadata()
	case "video-captions":
		return locales.ImportsStageVideoCaptions()
	case "video-audio":
		return locales.ImportsStageVideoAudio()
	case "transcoding":
		return locales.ImportsStageTranscoding()
	case "transcribing":
		return locales.ImportsStageTranscribing()
	case "formatting":
		return locales.ImportsStageFormatting()
	case "summarizing":
		return locales.ImportsStageSummarizing()
	case "image-download":
		return locales.ImportsStageImageDownload()
	case "image-ocr":
		return locales.ImportsStageImageOcr()
	case "forum-replies":
		return locales.ImportsStageForumReplies()
	case "browser-capture":
		return locales.ImportsStageBrowserCapture()
	case "comments":
		return locales.ImportsStageComments()
	}
	return locales.ImportsStageFetching()
}

// StatusLabel 取状态文案（详情弹窗与诊断文本用；行内徽标按状态自己挑样式）。
func StatusLabel(status string) string {
	switch status {
	case services.ImportStatusPending:
		return locales.ImportsStatusPending()
	case services.ImportStatusProcessing:
		return locales.ImportsStatusProcessing()
	case services.ImportStatusCompleted:
		return locales.ImportsStatusCompleted()
	case services.ImportStatusDuplicate:
		return locales.ImportsStatusDuplicate()
	case services.ImportStatusCanceled:
		return locales.ImportsStatusCanceled()
	case services.ImportStatusFailed:
		return locales.ImportsStatusFailed()
	}
	return status
}

// ResolveStageStats 补算快照之后的当前阶段耗时：列表、详情与诊断使用相同口径。
func ResolveStageStats(task services.ImportTask, now int64) []services.ImportStageStat {
	stats := task.StageStats
	if task.Status != services.ImportStatusProcessing || task.Stage == "" {
		return stats
	}
	pending := max(0, now-task.UpdatedAt)
	for _, entry := range stats {
		if entry.Stage == task.Stage {
			out := make([]services.ImportStageStat, len(stats))
			for i, e := range stats {
				if e.Stage == task.Stage {
					e.Ms += pending
				}
				out[i] = e
			}
			return out
		}
	}
	return append(append([]services.ImportStageStat(nil), stats...), services.ImportStageStat{Stage: task.Stage, Ms: pending})
}

// ResolveWorkElapsed 是处理中任务的**实际工作**时长，不含排队等待。
//
// 不能用 now - createdAt：那是入队时刻，而并发只有 2——一次丢进三十条链接，最后一条能在队列里躺一个钟头。
// 等式是「已结算的各阶段之和 + 当前阶段的已用」；后半段取 updatedAt 成立，是因为处理期间每一次写库都带着阶段变更。
// 加统计之前的老任务没有这一列，退回 now - createdAt。
func ResolveWorkElapsed(task services.ImportTask, now int64) int64 {
	if len(task.StageStats) == 0 {
		return max(0, now-task.CreatedAt)
	}
	var total int64
	for _, entry := range ResolveStageStats(task, now) {
		total += entry.Ms
	}
	return total
}

// FormatDuration 把毫秒格式化为 M:SS / H:MM:SS：走时钟写法而不是「3 分 7 秒」，免掉一套单复数与中英差异的文案。
func FormatDuration(ms int64) string {
	total := max(0, ms/1000)
	seconds, minutes, hours := total%60, (total/60)%60, total/3600
	pad := func(v int64) string {
		if v < 10 {
			return "0" + strconv.FormatInt(v, 10)
		}
		return strconv.FormatInt(v, 10)
	}
	if hours > 0 {
		return strconv.FormatInt(hours, 10) + ":" + pad(minutes) + ":" + pad(seconds)
	}
	return strconv.FormatInt(minutes, 10) + ":" + pad(seconds)
}

var wwwPrefix = regexp.MustCompile(`^www\.`)

// ResolveTaskHost 取网址任务的来源域名（去掉 www.）；非网址任务与非法网址返回 false。
// 一列 v2ex / 抖音链接混在一起时，这是最省版面的辨识标记。解析走 WHATWG URL（与浏览器的 new URL 同一套规则）。
func ResolveTaskHost(task services.ImportTask) (string, bool) {
	if task.SourceKind != "url" {
		return "", false
	}
	u, err := whatwg.Parse(task.SourceInput)
	if err != nil {
		return "", false
	}
	host := wwwPrefix.ReplaceAllString(u.Hostname(), "")
	return host, host != ""
}

var pathSeparator = regexp.MustCompile(`[\\/]`)

// ResolveTaskFolder 取文件任务的所在目录，作为副标题（列表里只显示文件名，路径靠这里补）。
func ResolveTaskFolder(task services.ImportTask) (string, bool) {
	if task.SourceKind != "file" {
		return "", false
	}
	segments := pathSeparator.Split(task.SourceInput, -1)
	segments = segments[:len(segments)-1]
	if len(segments) == 0 {
		return "", false
	}
	return strings.Join(segments, "/"), true
}

// 下面几组正则匹配的是 Go 后端写进任务 error / warning 的中文原文（渲染层同样靠子串识别），不是界面文案；
// 为通过「界面不得出现中日韩字面量」的门禁，汉字一律写成 \u 转义，注释里给出可读原文。
var (
	toolNamePattern = regexp.MustCompile(`(?i)yt-dlp|ffmpeg`)
	// 未安装|not installed|尚未安装
	toolMissingPattern = regexp.MustCompile("(?i)\u672a\u5b89\u88c5|not installed|\u5c1a\u672a\u5b89\u88c5")
)

// NeedsCaptureToolSetup 报告失败原因是否指向未安装的外部工具：这类失败靠重试永远好不了，得先去设置页装工具。
func NeedsCaptureToolSetup(err string) bool {
	return err != "" && toolNamePattern.MatchString(err) && toolMissingPattern.MatchString(err)
}

// ---- 平台解析错误码（packages/shared/utils/platform-parse-error.ts） ----

var parseCodes = map[string]bool{
	"structure_missing": true, "note_unavailable": true, "token_invalid": true, "guest_denied": true, "network": true,
}

var parseCodePrefix = regexp.MustCompile(`^\[([a-z_]+)\]\s*`)

// SplitParseError 把 `[code] 正文` 拆开；没有稳定前缀时 code 为空、body 为原文。
func SplitParseError(message string) (code, body string) {
	m := parseCodePrefix.FindStringSubmatch(message)
	if m == nil || !parseCodes[m[1]] {
		return "", message
	}
	body = strings.TrimSpace(message[len(m[0]):])
	if body == "" {
		body = message
	}
	return m[1], body
}

func parseCodeLabel(code string) string {
	switch code {
	case "structure_missing":
		return locales.ImportsParseErrorStructureMissing()
	case "note_unavailable":
		return locales.ImportsParseErrorNoteUnavailable()
	case "token_invalid":
		return locales.ImportsParseErrorTokenInvalid()
	case "guest_denied":
		return locales.ImportsParseErrorGuestDenied()
	}
	return locales.ImportsParseErrorNetwork()
}

var (
	// FlareSolverr|未通过|取消|超时|失败|拒绝|预算|超过
	solverFailure = regexp.MustCompile("FlareSolverr|\u672a\u901a\u8fc7|\u53d6\u6d88|\u8d85\u65f6|\u5931\u8d25|\u62d2\u7edd|\u9884\u7b97|\u8d85\u8fc7")
	// ^web_verification_required[:：]?\s*
	verificationPrefix = regexp.MustCompile("^web_verification_required[:\uff1a]?\\s*")
	// ^文字稿生成失败：
	transcriptionFailed = regexp.MustCompile("^\u6587\u5b57\u7a3f\u751f\u6210\u5931\u8d25\uff1a")
	httpStatus403       = regexp.MustCompile(`HTTP\s*403`)
	httpStatus401       = regexp.MustCompile(`HTTP\s*401`)
	httpStatus404       = regexp.MustCompile(`HTTP\s*404`)
	httpStatus429       = regexp.MustCompile(`HTTP\s*429`)
)

// FormatTaskError 把任务 error 里的 `[structure_missing]` 等前缀翻成可读标签；没有稳定码时原样返回。
func FormatTaskError(err string) string {
	if err == "" {
		return ""
	}
	if strings.Contains(err, "web_verification_required") {
		if solverFailure.MatchString(err) {
			return verificationPrefix.ReplaceAllString(err, "")
		}
		return locales.ImportsWebVerificationRequired()
	}
	if strings.Contains(err, "nodeseek_verification_required") {
		return locales.ImportsNodeseekVerificationRequired()
	}
	code, body := SplitParseError(err)
	if code == "" {
		return err
	}
	label := parseCodeLabel(code)
	if body == "" || body == err {
		return label
	}
	if strings.HasPrefix(body, label) {
		return body
	}
	return label + "\uff1a" + body
}

// FormatTaskErrorForReport 是诊断文本用的版本：可读文案 + 稳定错误码（排查要靠后者）。
func FormatTaskErrorForReport(err string) string {
	if err == "" {
		return ""
	}
	code, _ := SplitParseError(err)
	display := FormatTaskError(err)
	if code == "" {
		return display
	}
	return display + "\n" + locales.ImportsParseErrorCode(code)
}

// FormatTaskWarning 翻译 warning：它代表条目已经入库、只有一部分内容没拿到。老版本直接把 `HTTP 403` 放给用户，
// 既看不出是权限、下架还是平台风控，也不知道下一步该做什么；原始状态码保留在说明里。
func FormatTaskWarning(warning string) string {
	if warning == "" {
		return ""
	}
	if !transcriptionFailed.MatchString(warning) {
		return warning
	}
	switch {
	case httpStatus403.MatchString(warning):
		return locales.ImportsTranscriptionFailure403()
	case httpStatus401.MatchString(warning):
		return locales.ImportsTranscriptionFailure401()
	case httpStatus404.MatchString(warning):
		return locales.ImportsTranscriptionFailure404()
	case httpStatus429.MatchString(warning):
		return locales.ImportsTranscriptionFailure429()
	}
	return warning
}

// ---- 平台识别（packages/shared/utils/platform-capture.ts、web-verification.ts） ----

// 登录态采集支持的平台。
const (
	PlatformXiaohongshu = "xiaohongshu"
	PlatformDouyin      = "douyin"
	PlatformLinuxdo     = "linuxdo"
	PlatformNodeseek    = "nodeseek"
)

var platformDomains = []struct {
	platform string
	domains  []string
}{
	{PlatformXiaohongshu, []string{"xiaohongshu.com", "xhslink.com", "xhslink.cn"}},
	{PlatformDouyin, []string{"douyin.com", "iesdouyin.com"}},
	{PlatformLinuxdo, []string{"linux.do"}},
	{PlatformNodeseek, []string{"nodeseek.com"}},
}

func isHostOrSubdomain(hostname, domain string) bool {
	host, expected := strings.ToLower(hostname), strings.ToLower(domain)
	return host == expected || strings.HasSuffix(host, "."+expected)
}

func isAllowedPlatformURL(platform, value string, domains []string) bool {
	u, err := whatwg.Parse(value)
	if err != nil {
		return false
	}
	if platform == PlatformNodeseek {
		if u.Username() != "" || u.Password() != "" || u.Port() != "" {
			return false
		}
		if h := u.Hostname(); h != "nodeseek.com" && h != "www.nodeseek.com" {
			return false
		}
	}
	if u.Protocol() != "https:" {
		return false
	}
	for _, domain := range domains {
		if isHostOrSubdomain(u.Hostname(), domain) {
			return true
		}
	}
	return false
}

// DetectPlatformCapturePlatform 判断网址属于哪个登录态采集平台；都不是返回空串。
func DetectPlatformCapturePlatform(value string) string {
	for _, p := range platformDomains {
		if isAllowedPlatformURL(p.platform, value, p.domains) {
			return p.platform
		}
	}
	return ""
}

var (
	// nodeseek_verification_required|拒绝访问|验证|403|Just a moment
	nodeseekFailure = regexp.MustCompile("(?i)nodeseek_verification_required|\u62d2\u7edd\u8bbf\u95ee|\u9a8c\u8bc1|403|Just a moment")
	// ^(just a moment|verify you are human|人机验证|安全验证)
	verificationTitle = regexp.MustCompile("(?i)^(just a moment|verify you are human|\u4eba\u673a\u9a8c\u8bc1|\u5b89\u5168\u9a8c\u8bc1)")
	// 网页要求完成验证码
	captchaRequiredError = regexp.MustCompile("\u7f51\u9875\u8981\u6c42\u5b8c\u6210\u9a8c\u8bc1\u7801")
)

// AuthenticatedRetryPlatform 返回该失败任务可用「登录态重试」的平台；没有返回空串。
// 只有匿名（standard）策略下、平台解析类错误码为 guest_denied / token_invalid / structure_missing 的网址任务才有；
// NodeSeek 例外，它的失败多是 Cloudflare 验证，按错误文本判断。
func AuthenticatedRetryPlatform(task services.ImportTask) string {
	if task.Status == services.ImportStatusFailed && task.SourceKind == "url" &&
		DetectPlatformCapturePlatform(task.SourceInput) == PlatformNodeseek && nodeseekFailure.MatchString(task.Error) {
		return PlatformNodeseek
	}
	strategy := task.CaptureStrategy
	if strategy == "" {
		strategy = "standard"
	}
	if task.Status != services.ImportStatusFailed || task.SourceKind != "url" || strategy != "standard" || task.Error == "" {
		return ""
	}
	switch code, _ := SplitParseError(task.Error); code {
	case "guest_denied", "token_invalid", "structure_missing":
		return DetectPlatformCapturePlatform(task.SourceInput)
	}
	return ""
}

// NeedsWebVerification 报告失败任务是否需要通用网页验证（兼容旧任务；403 与限流本身不能证明网站要求人机验证）。
func NeedsWebVerification(task services.ImportTask) bool {
	return task.Status == services.ImportStatusFailed && task.SourceKind == "url" &&
		DetectPlatformCapturePlatform(task.SourceInput) == "" &&
		(strings.Contains(task.Error, "web_verification_required") ||
			verificationTitle.MatchString(task.DisplayName) ||
			captchaRequiredError.MatchString(task.Error))
}

// SummarizeStages 求各阶段的总耗时与最慢的那个阶段（渲染层 summarizeStages）。stats 不能为空。
func SummarizeStages(stats []services.ImportStageStat) (totalMs int64, slowest services.ImportStageStat) {
	slowest = stats[0]
	for _, entry := range stats {
		totalMs += entry.Ms
		if entry.Ms > slowest.Ms {
			slowest = entry
		}
	}
	return totalMs, slowest
}
