package main

// 对应 services/media/ytdlp-manager.ts：yt-dlp 托管安装（官方源 + 镜像回退、官方校验和、走网络代理）、
// 状态探测与移除。托管副本位于 %userData%/tools/；生效顺序：自定义路径 → 内置托管版 → 系统 PATH。

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

func managedYtDlpBinaryName(platform string) string {
	if platform == "win32" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

func (l *Library) managedYtDlpPath(platform string) string {
	return filepath.Join(l.paths.ToolsDir(), managedYtDlpBinaryName(platform))
}

// ytDlpAssetName 是下载的资产文件名（校验清单按它匹配行）。
func ytDlpAssetName(platform string) string {
	switch platform {
	case "win32":
		return "yt-dlp.exe"
	case "darwin":
		return "yt-dlp_macos"
	}
	return "yt-dlp_linux"
}

func withMirrors(official string) []string {
	return []string{official, "https://ghfast.top/" + official, "https://gh-proxy.com/" + official, "https://hub.gitmirror.com/" + official}
}

func ytDlpDownloadURLs(platform string) []string {
	return withMirrors("https://github.com/yt-dlp/yt-dlp/releases/latest/download/" + ytDlpAssetName(platform))
}

// ytDlpChecksumURLs：官方 SHA2-256SUMS 优先（与下载镜像不同源，代理替换文件就对不上），官方不可达才退镜像。
func ytDlpChecksumURLs() []string {
	return withMirrors("https://github.com/yt-dlp/yt-dlp/releases/latest/download/SHA2-256SUMS")
}

func ytDlpLatestReleaseURLs() []string {
	return withMirrors("https://github.com/yt-dlp/yt-dlp/releases/latest")
}

var reReleaseTag = regexp.MustCompile(`/releases/tag/([^/?#]+)`)

// parseReleaseTag 从 302 的 Location（或重定向终点）里取出 release tag；yt-dlp 的 tag 就是 --version 输出。
func parseReleaseTag(location string) string {
	m := reReleaseTag.FindStringSubmatch(location)
	if m == nil {
		return ""
	}
	if tag, err := url.PathUnescape(m[1]); err == nil {
		return tag
	}
	return m[1]
}

// fetchLatestYtDlpVersion 只读 302 的 Location，不拉正文；全部失败返回空串（调用方照常走下载）。
func (l *Library) fetchLatestYtDlpVersion(ctx context.Context, urls []string) string {
	for _, u := range urls {
		if tag := l.latestYtDlpFrom(ctx, u); tag != "" {
			return tag
		}
	}
	logAppErrorf("main", "ytdlp", "无法获取远端最新版本号，将直接下载")
	return ""
}

func (l *Library) latestYtDlpFrom(ctx context.Context, u string) string {
	ctx, cancel := context.WithTimeout(ctx, releaseRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := l.toolHTTPClient(true).Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close() // 镜像可能自行跟随重定向并回正文，尽早断流
	if tag := parseReleaseTag(resp.Header.Get("Location")); tag != "" {
		return tag
	}
	if resp.Request != nil && resp.Request.URL != nil {
		return parseReleaseTag(resp.Request.URL.String())
	}
	return ""
}

// numericLocaleCompare 近似 a.localeCompare(b, undefined, {numeric: true})：数字段按数值比较，其余按 ICU 序。
func numericLocaleCompare(a, b string) int {
	ta, tb := numericTokens(a), numericTokens(b)
	for i := 0; i < len(ta) && i < len(tb); i++ {
		x, y := ta[i], tb[i]
		xd, yd := isDigitToken(x), isDigitToken(y)
		switch {
		case xd && yd:
			if c := compareDigitRuns(x, y); c != 0 {
				return c
			}
		case xd != yd:
			// ICU 一级权重：标点/空白 < 数字 < 字母
			if tokenClass(x) != tokenClass(y) {
				if tokenClass(x) < tokenClass(y) {
					return -1
				}
				return 1
			}
		default:
			if c := localeCompare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(ta) > len(tb):
		return 1
	case len(ta) < len(tb):
		return -1
	}
	return 0
}

func tokenClass(tok string) int {
	r := []rune(tok)[0]
	switch {
	case unicode.IsDigit(r):
		return 1
	case unicode.IsLetter(r):
		return 2
	}
	return 0
}

func isDigitToken(tok string) bool { return tok != "" && tok[0] >= '0' && tok[0] <= '9' }

func numericTokens(s string) []string {
	var out []string
	var cur []rune
	curDigit := false
	for _, r := range s {
		d := r >= '0' && r <= '9'
		if len(cur) > 0 && d != curDigit {
			out = append(out, string(cur))
			cur = cur[:0]
		}
		cur, curDigit = append(cur, r), d
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func compareDigitRuns(x, y string) int {
	x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
	if len(x) != len(y) {
		if len(x) < len(y) {
			return -1
		}
		return 1
	}
	return strings.Compare(x, y)
}

// checkYtDlpUpdate 只查版本号，不下载；比较沿用数值感知写法，避免把 nightly 判成需要回退。
func checkYtDlpUpdate(current *string, latest string) toolUpdateCheck {
	check := toolUpdateCheck{Current: current}
	if latest != "" {
		check.Latest = &latest
	}
	check.UpdateAvailable = latest != "" && current != nil && numericLocaleCompare(latest, *current) > 0
	return check
}

// probeYtDlpVersion 运行 `--version`，取首行。
func probeYtDlpVersion(ctx context.Context, executable string) (string, bool) {
	return probeExecutable(ctx, executable, "--version", func(out string) string {
		first, _, _ := strings.Cut(out, "\n")
		return strings.TrimFunc(first, jsSpace)
	})
}

func (l *Library) resolveYtDlpExecutable(configured string) string {
	if c := strings.TrimFunc(configured, jsSpace); c != "" {
		return c
	}
	if p := l.managedYtDlpPath(nodePlatform()); fileExists(p) {
		return p
	}
	return "yt-dlp"
}

func (l *Library) ytDlpStatus(ctx context.Context, configured string, probe versionProbe, platform string) toolStatus {
	managed := l.managedYtDlpPath(platform)
	done := func(s toolStatus) toolStatus {
		s.ManagedPath, s.InstallSupported = managed, true // win / mac / linux 均有官方资产
		return s
	}
	if custom := strings.TrimFunc(configured, jsSpace); custom != "" {
		v, ok := probe(ctx, custom)
		s := toolStatus{Installed: ok, Path: custom}
		if ok {
			s.Source, s.Version = strp("custom"), v
		}
		return done(s)
	}
	if fileExists(managed) {
		if v, ok := probe(ctx, managed); ok {
			return done(toolStatus{Installed: true, Source: strp("managed"), Version: v, Path: managed})
		}
	}
	if v, ok := probe(ctx, "yt-dlp"); ok {
		return done(toolStatus{Installed: true, Source: strp("path"), Version: v, Path: "yt-dlp"})
	}
	return done(toolStatus{})
}

// installYtDlp 下载并安装托管版：逐源尝试 → 校验和 → 临时文件校验可执行 → 原子替换，返回版本号。
func (l *Library) installYtDlp(ctx context.Context, platform string, urls, checksumURLs []string, onProgress func(toolDownloadProgress)) (string, error) {
	if !claimInstall(&toolInstall.ytdlp) {
		return "", errors.New("已有安装任务进行中")
	}
	defer releaseInstall(&toolInstall.ytdlp)
	managed := l.managedYtDlpPath(platform)
	dir := filepath.Dir(managed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	temp := filepath.Join(dir, "yt-dlp.partial")
	if platform == "win32" { // Windows 的 spawn 要求 .exe 扩展名
		temp += ".exe"
	}
	// 校验和先取：拿不到也继续（正需要镜像的网络里官方多半也不通），拿到了就必须对上
	expected := l.fetchExpectedSha256(ctx, checksumURLs, ytDlpAssetName(platform))
	if expected == "" {
		logAppErrorf("main", "ytdlp", "未能获取官方校验和，本次安装跳过哈希校验")
	}
	var failures []string
	for _, u := range urls {
		version, err := l.installYtDlpFrom(ctx, u, temp, managed, expected, platform, onProgress)
		if err == nil {
			return version, nil
		}
		_ = os.Remove(temp)
		failures = append(failures, hostOf(u)+": "+err.Error())
		logAppErrorf("main", "ytdlp", "下载源失败（%s），尝试下一个", hostOf(u))
	}
	return "", toolInstallFailure(failures)
}

func (l *Library) installYtDlpFrom(ctx context.Context, u, temp, managed, expected, platform string, onProgress func(toolDownloadProgress)) (string, error) {
	if err := l.downloadToFile(ctx, u, temp, onProgress); err != nil {
		return "", err
	}
	if expected != "" {
		actual, err := sha256File(temp)
		if err != nil {
			return "", err
		}
		if actual != expected {
			return "", errors.New("校验和不匹配（期望 " + expected[:12] + "…，实际 " + actual[:12] + "…）")
		}
	}
	if platform != "win32" {
		if err := os.Chmod(temp, 0o755); err != nil {
			return "", err
		}
	}
	version, ok := probeYtDlpVersion(ctx, temp)
	if !ok {
		return "", errors.New("下载的文件无法运行")
	}
	_ = os.Remove(managed)
	if err := os.Rename(temp, managed); err != nil {
		return "", err
	}
	return version, nil
}

func (l *Library) removeManagedYtDlp() (bool, error) {
	p := l.managedYtDlpPath(nodePlatform())
	if !fileExists(p) {
		return false, nil
	}
	if err := os.Remove(p); err != nil {
		return false, err
	}
	return true, nil
}
