package main

// 对应 services/media/ffmpeg-manager.ts：ffmpeg 托管安装（FFmpeg-Builds 官方源 + 镜像回退、走网络代理）、
// 状态探测与移除。托管副本位于 %userData%/tools/；生效顺序：自定义路径 → 内置托管版 → 系统 PATH。

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type toolStatus struct {
	Installed          bool    `json:"installed"`
	Source             *string `json:"source"`
	Version            string  `json:"version,omitempty"`
	Path               string  `json:"path,omitempty"`
	ManagedPath        string  `json:"managedPath"`
	InstallSupported   bool    `json:"installSupported"`
	InstallHintCommand string  `json:"installHintCommand,omitempty"`
}

type toolUpdateCheck struct {
	Current         *string `json:"current"`
	Latest          *string `json:"latest"`
	UpdateAvailable bool    `json:"updateAvailable"`
}

func managedFfmpegBinaryName(platform string) string {
	if platform == "win32" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

func (l *Library) managedFfmpegPath(platform string) string {
	return filepath.Join(l.paths.ToolsDir(), managedFfmpegBinaryName(platform))
}

// ffmpegDownloadURLs 对应 getFfmpegDownloadUrls：只有 Windows 构建；没有哈希校验是有意的
// （FFmpeg-Builds 的 release 不发校验清单，tag 恒为 latest），只能靠「解出来能跑」兜底。
func ffmpegDownloadURLs(platform string) []string {
	if platform != "win32" {
		return nil
	}
	official := "https://github.com/yt-dlp/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip"
	return []string{official, "https://ghfast.top/" + official, "https://gh-proxy.com/" + official, "https://hub.gitmirror.com/" + official}
}

func isFfmpegInstallSupported(platform string) bool { return platform == "win32" }

func ffmpegInstallHintCommand(platform string) string {
	if platform == "darwin" {
		return "brew install ffmpeg"
	}
	return ""
}

var reFfmpegBuildDate = regexp.MustCompile(`-(\d{8})$`)

// parseFfmpegBuildDate 取版本串尾部的构建日期（N-125753-g6095372a70-20260724 → 20260724）。
func parseFfmpegBuildDate(version *string) *string {
	if version == nil {
		return nil
	}
	if m := reFfmpegBuildDate.FindStringSubmatch(*version); m != nil {
		return &m[1]
	}
	return nil
}

// toBuildDate 把 Last-Modified 转成 UTC 的 YYYYMMDD；无法解析返回 nil。
func toBuildDate(lastModified string) *string {
	if lastModified == "" {
		return nil
	}
	t, err := http.ParseTime(lastModified)
	if err != nil {
		return nil
	}
	return strp(t.UTC().Format("20060102"))
}

// fetchLatestFfmpegBuildDate 只发 HEAD 读响应头，逐源尝试；全部失败返回 nil。
func (l *Library) fetchLatestFfmpegBuildDate(ctx context.Context, urls []string) *string {
	for _, u := range urls {
		if d := l.ffmpegBuildDateFrom(ctx, u); d != nil {
			return d
		}
	}
	logAppErrorf("main", "ffmpeg", "无法获取远端构建日期")
	return nil
}

func (l *Library) ffmpegBuildDateFrom(ctx context.Context, url string) *string {
	ctx, cancel := context.WithTimeout(ctx, headRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil
	}
	resp, err := l.toolHTTPClient(false).Do(req)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil
	}
	return toBuildDate(resp.Header.Get("Last-Modified"))
}

// checkFfmpegUpdate 检查内置版是否有新构建，构建日期（YYYYMMDD）可直接按字符串比较。
func checkFfmpegUpdate(currentVersion *string, latest *string) toolUpdateCheck {
	current := parseFfmpegBuildDate(currentVersion)
	return toolUpdateCheck{Current: current, Latest: latest, UpdateAvailable: latest != nil && current != nil && *latest > *current}
}

var reFfmpegVersion = regexp.MustCompile(`^ffmpeg version (\S+)`)

// probeFfmpegVersion 运行 `-version`，首行形如 "ffmpeg version 8.1-essentials_build-www.gyan.dev Copyright ..."。
func probeFfmpegVersion(ctx context.Context, executable string) (string, bool) {
	return probeExecutable(ctx, executable, "-version", func(out string) string {
		if m := reFfmpegVersion.FindStringSubmatch(out); m != nil {
			return m[1]
		}
		return ""
	})
}

// resolveFfmpegExecutable 解析生效的可执行文件：自定义路径 → 内置托管版 → 系统 PATH。
func (l *Library) resolveFfmpegExecutable(configured string) string {
	if c := strings.TrimFunc(configured, jsSpace); c != "" {
		return c
	}
	if p := l.managedFfmpegPath(nodePlatform()); fileExists(p) {
		return p
	}
	return "ffmpeg"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (l *Library) ffmpegStatus(ctx context.Context, configured string, probe versionProbe, platform string) toolStatus {
	managed := l.managedFfmpegPath(platform)
	withCaps := func(s toolStatus) toolStatus {
		s.ManagedPath = managed
		s.InstallSupported = isFfmpegInstallSupported(platform)
		s.InstallHintCommand = ffmpegInstallHintCommand(platform)
		return s
	}
	if custom := strings.TrimFunc(configured, jsSpace); custom != "" {
		v, ok := probe(ctx, custom)
		s := toolStatus{Installed: ok, Path: custom}
		if ok {
			s.Source, s.Version = strp("custom"), v
		}
		return withCaps(s)
	}
	if fileExists(managed) {
		if v, ok := probe(ctx, managed); ok {
			return withCaps(toolStatus{Installed: true, Source: strp("managed"), Version: v, Path: managed})
		}
		// 托管副本损坏：继续探测系统 PATH
	}
	if v, ok := probe(ctx, "ffmpeg"); ok {
		return withCaps(toolStatus{Installed: true, Source: strp("path"), Version: v, Path: "ffmpeg"})
	}
	return withCaps(toolStatus{})
}

// extractFfmpegFromZip 从下载的 zip 中解出 bin/ffmpeg.exe 到目标路径。
func extractFfmpegFromZip(zipPath, target string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ReplaceAll(f.Name, `\`, "/"), "/bin/ffmpeg.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, rc)
		if cerr := out.Close(); copyErr == nil {
			copyErr = cerr
		}
		return copyErr
	}
	return errors.New("压缩包中未找到 bin/ffmpeg.exe")
}

var toolInstall = struct {
	sync.Mutex
	ffmpeg, ytdlp bool
}{}

func claimInstall(flag *bool) bool {
	toolInstall.Lock()
	defer toolInstall.Unlock()
	if *flag {
		return false
	}
	*flag = true
	return true
}

func releaseInstall(flag *bool) {
	toolInstall.Lock()
	*flag = false
	toolInstall.Unlock()
}

// installFfmpeg 下载并安装托管版：逐源尝试 → 解出可执行文件 → 校验可运行 → 原子替换，返回版本号。
func (l *Library) installFfmpeg(ctx context.Context, platform string, urls []string, onProgress func(toolDownloadProgress)) (string, error) {
	if !claimInstall(&toolInstall.ffmpeg) {
		return "", errors.New("已有安装任务进行中")
	}
	defer releaseInstall(&toolInstall.ffmpeg)
	if platform != "win32" {
		return "", errors.New("当前平台请使用系统包管理器安装 ffmpeg")
	}
	managed := l.managedFfmpegPath(platform)
	dir := filepath.Dir(managed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, "ffmpeg.download.zip")
	temp := filepath.Join(dir, "ffmpeg.partial.exe") // Windows 的 spawn 要求 .exe 扩展名
	var failures []string
	for _, u := range urls {
		version, err := l.installFfmpegFrom(ctx, u, zipPath, temp, managed, onProgress)
		if err == nil {
			return version, nil
		}
		_ = os.Remove(temp)
		failures = append(failures, hostOf(u)+": "+err.Error())
		logAppErrorf("main", "ffmpeg", "下载源失败（%s），尝试下一个", hostOf(u))
	}
	return "", toolInstallFailure(failures)
}

func (l *Library) installFfmpegFrom(ctx context.Context, url, zipPath, temp, managed string, onProgress func(toolDownloadProgress)) (string, error) {
	defer os.Remove(zipPath)
	if err := l.downloadToFile(ctx, url, zipPath, onProgress); err != nil {
		return "", err
	}
	if err := extractFfmpegFromZip(zipPath, temp); err != nil {
		return "", err
	}
	version, ok := probeFfmpegVersion(ctx, temp)
	if !ok {
		return "", errors.New("下载的文件无法运行")
	}
	_ = os.Remove(managed)
	if err := os.Rename(temp, managed); err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return version, nil
}

func (l *Library) removeManagedFfmpeg() (bool, error) {
	p := l.managedFfmpegPath(nodePlatform())
	if !fileExists(p) {
		return false, nil
	}
	if err := os.Remove(p); err != nil {
		return false, err
	}
	return true, nil
}
