package main

// 在线视频平台字幕：先尝试发布者字幕，再尝试平台自动字幕。对应 import/video-captions.ts。
//
// yt-dlp 已经处理了各平台字幕接口、签名与格式差异；这里仅负责把下载到临时目录的 VTT / SRT 清成可入库的文字。
// 字幕不可用是正常分支，绝不能让它阻断后续 ASR。

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const videoCaptionTimeout = 90 * time.Second

const (
	captionSourceSubtitles = "platform-subtitles"
	captionSourceAI        = "platform-ai-captions"
)

type platformCaption struct {
	Text   string
	Source string // platform-subtitles / platform-ai-captions
	// yt-dlp 文件名里携带的语言标记，仅用于留痕，不作为语言承诺
	Language string
}

var (
	reCaptionBlockStart = regexp.MustCompile(`(?i)^(WEBVTT|NOTE|STYLE|REGION)(` + fsp + `|$)`)
	reDigitsOnly        = regexp.MustCompile(`^\d+$`)
	reAnyTagCaption     = regexp.MustCompile(`<[^>]*>`)
	reNbspEnt           = regexp.MustCompile(`(?i)&nbsp;`)
	reAmpEnt            = regexp.MustCompile(`(?i)&amp;`)
	reLtEnt             = regexp.MustCompile(`(?i)&lt;`)
	reGtEnt             = regexp.MustCompile(`(?i)&gt;`)
	reCaptionFile       = regexp.MustCompile(`(?i)\.(vtt|srt)$`)
	reCaptionOrig       = regexp.MustCompile(`(?i)-orig\.(?:vtt|srt)$`)
	reCaptionZh         = regexp.MustCompile(`(?i)\.(zh|zh-[a-z-]+|cmn|yue)\.`)
	reCaptionEn         = regexp.MustCompile(`(?i)\.en(?:-[a-z]+)?\.`)
	reCaptionLang       = regexp.MustCompile(`(?i)^caption\.([^.]+)\.(?:vtt|srt)$`)
)

// parseCaptionText 清理 WebVTT / SRT cue 标记。保留换行；自动字幕经常重复相邻 cue，连续相同的行只留一次。
func parseCaptionText(raw string) string {
	var lines []string
	skipBlock := false
	raw = strings.TrimPrefix(raw, "\uFEFF")
	for _, original := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line := jsTrim(original)
		if line == "" {
			skipBlock = false
			continue
		}
		// 文件头元数据（Kind:、Language:）与 NOTE、STYLE、REGION 块都持续到空行；缺空行时以时间轴为界
		if reCaptionBlockStart.MatchString(line) {
			skipBlock = true
			continue
		}
		if strings.Contains(line, "-->") {
			skipBlock = false
			continue
		}
		if skipBlock || reDigitsOnly.MatchString(line) {
			continue
		}
		text := reAnyTagCaption.ReplaceAllString(line, "")
		text = reNbspEnt.ReplaceAllString(text, " ")
		text = reAmpEnt.ReplaceAllString(text, "&")
		text = reLtEnt.ReplaceAllString(text, "<")
		text = reGtEnt.ReplaceAllString(text, ">")
		text = jsTrim(reSpaceRun.ReplaceAllString(text, " "))
		if text != "" && (len(lines) == 0 || lines[len(lines)-1] != text) {
			lines = append(lines, text)
		}
	}
	return jsTrim(strings.Join(lines, "\n"))
}

// localeCompareLite 近似 JS 的 localeCompare：先不分大小写比较，平手时小写在前。
func localeCompareLite(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		if la < lb {
			return -1
		}
		return 1
	}
	if a == b {
		return 0
	}
	if a > b { // 同一位置小写字母的码点更大，应排在前
		return -1
	}
	return 1
}

func preferredCaptionFiles(dir string) []string {
	// 某些 yt-dlp 错误路径会自行清理输出目录；这与「没有字幕」等价
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if reCaptionFile.MatchString(e.Name()) {
			files = append(files, e.Name())
		}
	}
	// 原始语言的自动字幕（-orig）最忠实于原文；中文是产品主场，其次英文；都没有时仍取任意可读字幕
	score := func(name string) int {
		switch {
		case reCaptionOrig.MatchString(name):
			return 0
		case reCaptionZh.MatchString(name):
			return 1
		case reCaptionEn.MatchString(name):
			return 2
		}
		return 3
	}
	sort.SliceStable(files, func(i, j int) bool {
		if si, sj := score(files[i]), score(files[j]); si != sj {
			return si < sj
		}
		return localeCompareLite(files[i], files[j]) < 0
	})
	return files
}

func captionLanguageOf(fileName string) string {
	if m := reCaptionLang.FindStringSubmatch(fileName); m != nil {
		return m[1]
	}
	return ""
}

func downloadCaptionSet(ctx context.Context, executable, url, dir string, run runCommandFunc, source string, automatic bool) (*platformCaption, error) {
	args := []string{"--ignore-config", "--no-warnings", "--no-playlist", "--skip-download", "--write-subs"}
	if automatic {
		args = append(args, "--write-auto-subs")
	}
	args = append(args, "--sub-langs", "all,-live_chat", "--sub-format", "vtt/srt/best", "-o", filepath.Join(dir, "caption.%(ext)s"), url)
	if _, err := run(ctx, executable, args, videoCaptionTimeout); err != nil {
		if ctx.Err() != nil {
			return nil, errCanceledMsg
		}
		// 没有字幕、地区限制与单个平台接口变动都不该拦住 ASR 兜底
		return nil, nil
	}
	for _, name := range preferredCaptionFiles(dir) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if text := parseCaptionText(string(raw)); text != "" {
			return &platformCaption{Text: text, Source: source, Language: captionLanguageOf(name)}, nil
		}
	}
	return nil, nil
}

// downloadPlatformCaptions 发布者字幕优先；确实不存在时才向平台请求自动字幕。
func downloadPlatformCaptions(ctx context.Context, executable, url string, run runCommandFunc) (*platformCaption, error) {
	dir, err := newVideoTempDir("guizhi-captions-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if c, err := downloadCaptionSet(ctx, executable, url, dir, run, captionSourceSubtitles, false); c != nil || err != nil {
		return c, err
	}
	return downloadCaptionSet(ctx, executable, url, dir, run, captionSourceAI, true)
}
