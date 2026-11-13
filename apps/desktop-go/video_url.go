package main

// 在线视频采集：平台链接检测 → 元数据 / 音频获取 → 可选远程转写。对应 import/video-url.ts。
//
// 元数据与音轨默认由 yt-dlp 提供（优先设置里配置的路径，否则托管版 / PATH；未安装时降级保存链接并附安装指引）。
// 抖音与小红书不依赖 yt-dlp（见 video_douyin.go / video_xiaohongshu.go）。
// 命令执行、两个平台的抓取与转写链路各环节都以字段注入，便于测试用假实现驱动。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	videoMetadataTimeout      = 90 * time.Second
	videoAudioDownloadTimeout = 15 * time.Minute
	videoAudioMaxFilesize     = "300m"
	// 简介压缩为元数据引用块单行展示，取前 300 字足够辨识
	videoDescriptionMaxLen = 300
	// 简介里的链接最多列几条：抖音的推广文案能堆十几个短链，全列进去反成噪音
	videoLinkMaxCount   = 5
	videoOutputMaxBytes = 4 * 1024 * 1024
)

var videoPlatformLabels = map[string]string{
	"bilibili": "哔哩哔哩", "youtube": "YouTube", "douyin": "抖音", "xiaohongshu": "小红书",
}

var (
	reHTTP403  = regexp.MustCompile(`HTTP` + fsp + `*403`)
	reHTTP401  = regexp.MustCompile(`HTTP` + fsp + `*401`)
	reHTTP404  = regexp.MustCompile(`HTTP` + fsp + `*404`)
	reHTTP429  = regexp.MustCompile(`HTTP` + fsp + `*429`)
	reHTTP5xx2 = regexp.MustCompile(`HTTP` + fsp + `*5\d\d`)
	reSpaceRun = regexp.MustCompile(fsp + `+`)
)

// describeTranscriptionFailure 把转写失败（多发生在下载音轨）翻译成可操作的说明；未知错误完整保留。
func describeTranscriptionFailure(err error) string {
	message := err.Error()
	switch {
	case reHTTP403.MatchString(message):
		return "文字稿生成失败：平台拒绝访问音频（HTTP 403）。视频可能仅限登录或私密可见，也可能是平台暂时限制解析；请确认链接能在未登录状态打开后稍后重试。"
	case reHTTP401.MatchString(message):
		return "文字稿生成失败：平台要求登录才能访问音频（HTTP 401）。请确认视频为公开内容，或改用本地文件采集。"
	case reHTTP404.MatchString(message):
		return "文字稿生成失败：音频资源已不可用（HTTP 404）。视频可能已删除、下架或链接已失效。"
	case reHTTP429.MatchString(message):
		return "文字稿生成失败：平台暂时限制了音频访问（HTTP 429）。请稍后重试，避免连续重复提交。"
	case reHTTP5xx2.MatchString(message):
		return "文字稿生成失败：平台或转写服务暂时不可用（服务器错误）。请稍后重试。"
	}
	return "文字稿生成失败：" + message
}

func isCanceledError(err error) bool {
	return err != nil && (err.Error() == errCanceledMsg.Error() || errors.Is(err, context.Canceled))
}

// ---- 命令执行 ----

// runCommandFunc 执行外部命令并返回 stdout；找不到可执行文件返回 errYtDlpNotFound。
type runCommandFunc func(ctx context.Context, executable string, args []string, timeout time.Duration) (string, error)

type cappedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf.Len() < c.max {
		c.buf.Write(p)
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// defaultRunCommand：执行 + 超时 / 取消 kill；失败带 stderr 末 3 行（≤300 码元）。
func defaultRunCommand(ctx context.Context, executable string, args []string, timeout time.Duration) (string, error) {
	if ctx.Err() != nil {
		return "", errCanceledMsg
	}
	cctx, cancel := context.WithTimeoutCause(ctx, timeout, errors.New("yt-dlp 执行超时"))
	defer cancel()
	cmd := exec.CommandContext(cctx, executable, args...)
	hideChildWindow(cmd)
	stdout := &cappedBuffer{max: videoOutputMaxBytes}
	stderr := &cappedBuffer{max: videoOutputMaxBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", errYtDlpNotFound
		}
		return "", err
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "", errCanceledMsg
	}
	if cause := context.Cause(cctx); cctx.Err() != nil && cause != nil && ctx.Err() == nil {
		return "", cause
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			lines := strings.Split(strings.ReplaceAll(jsTrim(stderr.String()), "\r\n", "\n"), "\n")
			if len(lines) > 3 {
				lines = lines[len(lines)-3:]
			}
			detail := utf16Slice(strings.Join(lines, " "), 300)
			return "", fmt.Errorf("yt-dlp 退出码 %d: %s", ee.ExitCode(), detail)
		}
		return "", waitErr
	}
	return stdout.String(), nil
}

// ---- 元数据 ----

type ytDlpMetadata struct {
	Title           string
	Uploader        string
	DurationSeconds *int
	Description     string
	WebpageURL      string
}

// parseYtDlpMetadata 解析 --dump-json 输出（单行 JSON；容忍前后噪声行）。
func parseYtDlpMetadata(stdout string) (ytDlpMetadata, error) {
	var last string
	for _, line := range strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n") {
		if t := jsTrim(line); strings.HasPrefix(t, "{") {
			last = t
		}
	}
	if last == "" {
		return ytDlpMetadata{}, errors.New("yt-dlp 未返回视频元数据")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(last), &parsed); err != nil {
		return ytDlpMetadata{}, err
	}
	str := func(key string) (string, bool) { s, ok := parsed[key].(string); return s, ok }
	m := ytDlpMetadata{}
	if s, ok := str("title"); ok {
		m.Title = s
	}
	if s, ok := str("uploader"); ok {
		m.Uploader = s
	} else if s, ok := str("channel"); ok {
		m.Uploader = s
	}
	if d, ok := parsed["duration"].(float64); ok && !math.IsInf(d, 0) && !math.IsNaN(d) {
		r := int(math.Floor(d + 0.5)) // Math.round：.5 向 +∞
		m.DurationSeconds = &r
	}
	if s, ok := str("description"); ok {
		m.Description = s
	}
	if s, ok := str("webpage_url"); ok && s != "" {
		m.WebpageURL = s
	}
	return m, nil
}

func formatVideoDuration(totalSeconds int) string {
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

// buildVideoContent 组装视频条目正文：只写元数据引用块（平台/作者/时长/简介/链接/文字稿来源）与可选的转写注记。
func buildVideoContent(m ytDlpMetadata, platform, transcriptionNote, transcriptionSource string) string {
	var meta []string
	meta = append(meta, "平台："+videoPlatformLabels[platform])
	if m.Uploader != "" {
		meta = append(meta, "作者："+m.Uploader)
	}
	if m.DurationSeconds != nil {
		meta = append(meta, "时长："+formatVideoDuration(*m.DurationSeconds))
	}
	quote := []string{"> " + strings.Join(meta, " · ")}
	description := reSpaceRun.ReplaceAllString(jsTrim(m.Description), " ")
	if description != "" {
		clipped := description
		if utf16Len(description) > videoDescriptionMaxLen {
			clipped = utf16Slice(description, videoDescriptionMaxLen) + "…"
		}
		quote = append(quote, "> 简介："+clipped)
	}
	// 链接从未截断的简介里抠，单独占一行
	links := extractURLsFromText(m.Description)
	if len(links) > videoLinkMaxCount {
		links = links[:videoLinkMaxCount]
	}
	if len(links) > 0 {
		quote = append(quote, "> 相关链接："+strings.Join(links, " "))
	}
	if transcriptionSource != "" {
		quote = append(quote, "> 文字稿来源："+transcriptionSource)
	}
	parts := []string{strings.Join(quote, "\n")}
	if transcriptionNote != "" {
		parts = append(parts, "> "+transcriptionNote)
	}
	return strings.Join(parts, "\n\n")
}

func videoNotInstalledReason(platform string) string {
	return "检测到" + videoPlatformLabels[platform] + "视频链接，但尚未安装 yt-dlp，无法解析视频信息。" +
		"打开「设置 → 应用设置 → 采集」一键安装后，回到本列表点击「重试」。"
}

func newVideoTempDir(prefix string) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), prefix+hex.EncodeToString(b[:]))
	return dir, os.MkdirAll(dir, 0o755)
}

// downloadBestAudio 下载最佳音轨到临时目录（不依赖 ffmpeg），返回目录与音频文件路径。
func downloadBestAudio(ctx context.Context, executable, url string, run runCommandFunc) (dir, file string, err error) {
	dir, err = newVideoTempDir("guizhi-video-")
	if err != nil {
		return "", "", err
	}
	_, err = run(ctx, executable, []string{
		"--no-warnings", "--no-playlist", "--max-filesize", videoAudioMaxFilesize,
		"-f", "bestaudio/best", "-o", filepath.Join(dir, "audio.%(ext)s"), url,
	}, videoAudioDownloadTimeout)
	if err != nil {
		return dir, "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir, "", err
	}
	if len(entries) == 0 {
		return dir, "", errors.New("音频下载失败（可能超过大小限制）")
	}
	return dir, filepath.Join(dir, entries[0].Name()), nil
}

// ---- 依赖与平台差异收敛点 ----

// mediaSource：元数据从哪来、音轨怎么下。
type mediaSource struct {
	metadata ytDlpMetadata
	// 下载可供转码转写的音轨 / 视频到临时目录（dir 由调用方清理）
	downloadAudio func(ctx context.Context) (dir, file string, err error)
	// 可选的平台字幕；只有 yt-dlp 平台实现，失败时由调用方转 ASR
	downloadCaptions func(ctx context.Context) (*platformCaption, error)
	// 图文作品仅在导入时下载配图并 OCR；重转写只需提示没有音轨
	note func() (*extractedContent, error)
}

type videoDeps struct {
	ytDlpPath  string
	ffmpegPath string
	diarize    bool
	run        runCommandFunc

	// 平台来源解析（抖音 / 小红书），默认走各自连接器
	resolveDouyin      func(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error)
	resolveXiaohongshu func(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error)
	douyin             douyinDeps
	xiaohongshu        xiaohongshuDeps
	imageNote          imageNoteDeps

	getTranscriptionConfig func() *transcriptionModelConfig
	ensureLocalService     func(apiURL string) error
	transcribe             func(ctx context.Context, filePath string, c transcriptionModelConfig, diarize bool) (string, error)
	getPlatformCaptions    func(ctx context.Context) (*platformCaption, error)
	prepareAudio           func(ctx context.Context, filePath, ffmpegExecutable string) (preparedAudio, error)
	getFormatterConfig     func() *aiClientConfig
	formatTranscript       func(ctx context.Context, raw string, cfg aiClientConfig, o formatTranscriptOptions) (formatTranscriptResult, error)
	getSummaryConfig       func() *aiClientConfig
	summarize              func(ctx context.Context, in mediaSummaryInput, cfg aiClientConfig, chat aiChatFunc) (mediaSummaryResult, error)
	// 上报当前子阶段（元数据 / 字幕 / 下载 / 转码 / 转写 / 排版 / 总结）
	onStage func(string)
}

func (d *videoDeps) stage(s string) {
	if d.onStage != nil {
		d.onStage(s)
	}
}

func (d *videoDeps) runner() runCommandFunc {
	if d.run != nil {
		return d.run
	}
	return defaultRunCommand
}

// videoDepsFromSettings 读取设置里的工具路径与说话人分离开关，其余走真实实现。
func (l *Library) videoDepsFromSettings(ctx context.Context, onStage func(string)) videoDeps {
	d := videoDeps{
		ytDlpPath:  l.readToolPathSetting(ctx, "ytDlpPath"),
		ffmpegPath: l.readToolPathSetting(ctx, "ffmpegPath"),
		onStage:    onStage,
	}
	if raw, ok := l.readRawSetting(ctx, "transcribeDiarize"); ok {
		var b bool
		if json.Unmarshal([]byte(raw), &b) == nil {
			d.diarize = b
		}
	}
	return d
}

func (l *Library) resolveMediaSource(ctx context.Context, url, platform string, deps *videoDeps) (*mediaSource, error) {
	switch platform {
	case "douyin":
		if deps.resolveDouyin != nil {
			return deps.resolveDouyin(ctx, url, deps)
		}
		return l.resolveDouyinSource(ctx, url, deps)
	case "xiaohongshu":
		if deps.resolveXiaohongshu != nil {
			return deps.resolveXiaohongshu(ctx, url, deps)
		}
		return l.resolveXiaohongshuSource(ctx, url, deps)
	}
	return l.resolveYtDlpSource(ctx, url, deps)
}

func (l *Library) resolveYtDlpSource(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error) {
	executable := l.resolveYtDlpExecutable(deps.ytDlpPath)
	run := deps.runner()
	out, err := run(ctx, executable, []string{"--dump-json", "--no-download", "--no-warnings", "--no-playlist", url}, videoMetadataTimeout)
	if err != nil {
		return nil, err
	}
	meta, err := parseYtDlpMetadata(out)
	if err != nil {
		return nil, err
	}
	return &mediaSource{
		metadata: meta,
		downloadAudio: func(c context.Context) (string, string, error) {
			return downloadBestAudio(c, executable, url, run)
		},
		downloadCaptions: func(c context.Context) (*platformCaption, error) {
			return downloadPlatformCaptions(c, executable, url, run)
		},
	}, nil
}

// downloadVideoAudio 已有视频重新转写：复用首次导入的平台解析与下载，临时目录由调用方清理。
// B 站 / YouTube 已有条目元数据，直接下载音轨，避免多请求一次元数据。
func (l *Library) downloadVideoAudio(ctx context.Context, url, platform string, deps *videoDeps) (dir, file string, err error) {
	if platform == "bilibili" || platform == "youtube" {
		return downloadBestAudio(ctx, l.resolveYtDlpExecutable(deps.ytDlpPath), url, deps.runner())
	}
	source, err := l.resolveMediaSource(ctx, url, platform, deps)
	if err != nil {
		return "", "", err
	}
	return source.downloadAudio(ctx)
}

func (l *Library) logVideoImportError(action, message, url string) {
	extra := newObj()
	extra.set("url", url)
	l.paths.logAppError("import", action, message, extra)
}

// extractVideoURL 视频链接采集主流程：元数据 → 平台字幕（优先）→ 语音转写 → 排版 → 总结。
func (l *Library) extractVideoURL(ctx context.Context, url, platform string, deps videoDeps) (*extractedContent, error) {
	deps.stage("video-metadata")
	source, err := l.resolveMediaSource(ctx, url, platform, &deps)
	if err == nil && source.note != nil {
		// 图文作品：配图下载 + OCR 的失败与解析失败同样降级（TS 里整段在同一个 try 内）
		ex, nerr := source.note()
		if nerr == nil {
			return ex, nil
		}
		err = nerr
	}
	if err != nil {
		if isCanceledError(err) {
			return nil, err
		}
		if errors.Is(err, errYtDlpNotFound) {
			return &extractedContent{Title: url, ItemType: "video", DegradedReason: videoNotInstalledReason(platform)}, nil
		}
		return &extractedContent{Title: url, ItemType: "video", DegradedReason: "视频信息解析失败：" + err.Error()}, nil
	}
	metadata := source.metadata

	// 先试平台字幕：它已带时间轴与原始标点，且无需下载音频或耗费转写额度。
	var transcript *string
	transcriptionNote := ""
	transcriptionSource := "" // platform-subtitles / platform-ai-captions / asr
	captionLanguage := ""
	getCaptions := deps.getPlatformCaptions
	if getCaptions == nil {
		getCaptions = source.downloadCaptions
	}
	if getCaptions != nil {
		deps.stage("video-captions")
		captions, err := getCaptions(ctx)
		if err != nil {
			if isCanceledError(err) {
				return nil, err
			}
			// 字幕是优化路径，不能因为平台接口波动让整条视频连 ASR 都走不了
			fmt.Printf("[import] 平台字幕获取失败，改用语音转写: %v\n", err)
		} else if captions != nil {
			t := captions.Text
			transcript = &t
			transcriptionSource = captions.Source
			captionLanguage = captions.Language
		}
	}

	var tcfg *transcriptionModelConfig
	if deps.getTranscriptionConfig != nil {
		tcfg = deps.getTranscriptionConfig()
	} else {
		tcfg = l.resolveTranscriptionConfig()
	}
	transcriptEmpty := transcript == nil || *transcript == ""
	switch {
	case transcriptEmpty && tcfg != nil:
		var terr error
		transcript, transcriptionSource, terr = l.videoTranscribe(ctx, url, source, &deps, *tcfg, &metadata)
		if terr != nil {
			if isCanceledError(terr) {
				return nil, terr
			}
			transcriptionNote = describeTranscriptionFailure(terr)
			// 转写失败会掏空整条条目——不留痕的话用户报「怎么没有文字稿」时双方都拿不出可查的东西
			fmt.Printf("[import] 语音转写失败: %v\n", terr)
			l.logVideoImportError("语音转写", transcriptionNote, url)
		}
	case transcriptEmpty:
		transcriptionNote = "未配置「语音转写」模型，本次仅保存视频信息；配置后可在任务列表重试生成文字稿。"
	}

	// 有文字稿则生成结构化视频总结与 AI 标题；失败只保留文字稿，不阻断导入
	summary, aiTitle, hasAITitle := "", "", false
	if transcript != nil && *transcript != "" {
		var scfg *aiClientConfig
		if deps.getSummaryConfig != nil {
			scfg = deps.getSummaryConfig()
		} else {
			scfg = l.resolveMediaSummaryConfig()
		}
		if scfg != nil {
			deps.stage("summarizing")
			summarize := deps.summarize
			if summarize == nil {
				summarize = l.generateMediaSummary
			}
			title := metadata.Title
			if title == "" {
				title = url
			}
			res, err := summarize(ctx, mediaSummaryInput{Title: title, Context: metadata.Description, Transcript: *transcript}, *scfg, nil)
			if err != nil {
				if ctx.Err() != nil {
					return nil, errCanceledMsg
				}
				fmt.Printf("[import] 视频总结生成失败，保留文字稿: %v\n", err)
			} else {
				summary, aiTitle, hasAITitle = res.Summary, res.Title, res.HasTitle
			}
		}
	}

	title := metadata.Title
	if title == "" {
		title = url
	}
	sourceNote := ""
	switch transcriptionSource {
	case "platform-subtitles":
		sourceNote = "发布者字幕"
		if captionLanguage != "" {
			sourceNote += "（" + captionLanguage + "）"
		}
	case "platform-ai-captions":
		sourceNote = "平台 AI 字幕"
		if captionLanguage != "" {
			sourceNote += "（" + captionLanguage + "）"
		}
	case "asr":
		model := "已配置模型"
		if tcfg != nil {
			model = tcfg.Model
		}
		sourceNote = "音频识别（" + model + "）"
	}
	content := buildVideoContent(metadata, platform, transcriptionNote, sourceNote)
	if summary != "" {
		content = upsertMediaSummarySection(content, mediaSummaryHeading("video"), summary)
	}
	// AI 标题替换平台原标题，原标题记入元数据引用块（仍可检索）
	if hasAITitle && aiTitle != "" && aiTitle != title {
		if metadata.Title != "" {
			content = appendOriginalTitleNote(content, metadata.Title)
		}
		title = aiTitle
	}
	sourceURI := metadata.WebpageURL
	if sourceURI == "" {
		sourceURI = url
	}
	return &extractedContent{
		Title: title, Content: content, ItemType: "video", SourceURI: &sourceURI,
		Transcript: transcript, WarningReason: transcriptionNote,
	}, nil
}

// videoTranscribe 下载音轨 → 转码 → 转写 →（ASR 输出才）AI 排版。返回文字稿与来源；
// 排版失败只留痕、保留原始转写；转写前的任一步失败整体返回错误由调用方写注记。
func (l *Library) videoTranscribe(ctx context.Context, url string, source *mediaSource, deps *videoDeps, cfg transcriptionModelConfig, metadata *ytDlpMetadata) (*string, string, error) {
	var tempDir string
	var prepared *preparedAudio
	defer func() {
		if prepared != nil && prepared.Cleanup != nil {
			prepared.Cleanup()
		}
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	}()
	// 目标是托管本地引擎时先确保服务已启动
	ensure := deps.ensureLocalService
	if ensure == nil {
		ensure = func(apiURL string) error { return l.funasr().ensureLocalTranscriptionService(apiURL) }
	}
	if err := ensure(cfg.APIURL); err != nil {
		return nil, "", err
	}
	deps.stage("video-audio")
	dir, file, err := source.downloadAudio(ctx)
	tempDir = dir
	if err != nil {
		return nil, "", err
	}
	deps.stage("transcoding")
	prepare := deps.prepareAudio
	if prepare == nil {
		prepare = prepareAudioForTranscription
	}
	p, err := prepare(ctx, file, l.resolveFfmpegExecutable(deps.ffmpegPath))
	if err != nil {
		return nil, "", err
	}
	prepared = &p
	deps.stage("transcribing")
	transcribe := deps.transcribe
	if transcribe == nil {
		transcribe = l.transcribeMediaFile
	}
	text, err := transcribe(ctx, p.FilePath, cfg, deps.diarize)
	if err != nil {
		return nil, "", err
	}
	// ASR 输出才需要补标点与分段；平台字幕本身已有结构
	if text != "" {
		var ferr error
		if text, ferr = l.videoFormatTranscript(ctx, url, deps, text, metadata); ferr != nil {
			return nil, "", ferr
		}
	}
	return &text, "asr", nil
}

func (l *Library) videoFormatTranscript(ctx context.Context, url string, deps *videoDeps, text string, metadata *ytDlpMetadata) (string, error) {
	var fcfg *aiClientConfig
	if deps.getFormatterConfig != nil {
		fcfg = deps.getFormatterConfig()
	} else {
		fcfg = l.resolveTranscriptFormatterConfig()
	}
	if fcfg == nil {
		return text, nil
	}
	deps.stage("formatting")
	format := deps.formatTranscript
	if format == nil {
		format = l.formatTranscript
	}
	res, err := format(ctx, text, *fcfg, formatTranscriptOptions{Glossary: extractGlossaryTerms(metadata.Title, metadata.Description)})
	if err != nil {
		if ctx.Err() != nil {
			return "", errCanceledMsg
		}
		fmt.Printf("[import] 文字稿 AI 排版失败，保留原始转写: %v\n", err)
		l.logVideoImportError("文字稿排版", "排版失败，保留原始转写："+err.Error(), url)
		return text, nil
	}
	note := ""
	switch {
	case res.SkippedReason != "":
		note = "跳过排版：" + res.SkippedReason
	case res.PartialReason != "":
		note = "部分排版：" + res.PartialReason
	}
	if note != "" {
		fmt.Printf("[import] %s\n", note)
		l.logVideoImportError("文字稿排版", note, url)
	}
	return res.Text, nil
}
