package main

// 对应 ipc/media.ipc.ts 里转写与总结相关的 7 个频道（工具管理见 managed_tools_ipc.go，FunASR 见 funasr_ipc.go）：
// media:transcribe / transcribeProgress（推送）/ formatTranscript / formatProgress（推送）/
// summarize / refreshForumDiscussion / testTranscription / capabilities。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	chMediaTranscribe         = "media:transcribe"
	chMediaTranscribeProgress = "media:transcribeProgress"
	chMediaFormatTranscript   = "media:formatTranscript"
	chMediaFormatProgress     = "media:formatProgress"
	chMediaSummarize          = "media:summarize"
	chMediaRefreshForum       = "media:refreshForumDiscussion"
	chMediaTestTranscription  = "media:testTranscription"
	chMediaCapabilities       = "media:capabilities"
)

// mediaResult 对应 MediaTranscribeResult：只输出被设置的键。
func mediaResult(success bool, kv ...any) map[string]any {
	m := map[string]any{"success": success}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func mediaFail(msg string) map[string]any { return mediaResult(false, "error", msg) }

// jsProp 对应 `obj?.k`：obj 不是对象时得到 undefined。
func jsProp(obj any, k string) any {
	if o, ok := obj.(*jsObj); ok {
		return o.get(k)
	}
	return jsUndefined
}

func strField(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// itemForMedia 读取条目（含最新来源链接）；不存在返回 nil。
func (l *Library) itemForMedia(ctx context.Context, id any) (map[string]any, error) {
	s, ok := id.(string)
	if !ok {
		return nil, nil
	}
	v, err := l.knowledgeGet(ctx, s)
	if err != nil || v == nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	return m, nil
}

// resolveItemGlossary 条目侧的专名表：标题 + 元数据引用块里的平台简介。
func resolveItemGlossary(item map[string]any) []string {
	desc := ""
	if meta := parseVideoMetaBlock(strField(item, "content")); meta != nil {
		desc = meta.description
	}
	return extractGlossaryTerms(strField(item, "title"), desc)
}

// formatTranscriptSafely 转写成功后顺带 AI 排版（补标点/分段）；失败或未配置文本模型时保留原始转写。
//
// 这是后台自动执行的一步，不弹提示，但失败与超长跳过都要写进 error.log：
// 不然用户看到的只是一坨没分段的文字，报「排版怎么没生效」时双方都拿不出数。
// 未配置文本模型是常态而非故障，不记。
func (l *Library) formatTranscriptSafely(ctx context.Context, raw string, item map[string]any) string {
	itemID := strField(item, "id")
	cfg := l.resolveTranscriptFormatterConfig()
	if cfg == nil {
		return raw
	}
	logIt := func(message string) {
		extra := newObj()
		extra.set("itemId", itemID)
		l.paths.logAppError("media", "文字稿排版", message, extra)
	}
	res, err := l.formatTranscript(ctx, raw, *cfg, formatTranscriptOptions{Glossary: resolveItemGlossary(item)})
	if err != nil {
		fmt.Printf("[media] 文字稿 AI 排版失败，保留原始转写: %v\n", err)
		logIt("排版失败，保留原始转写：" + err.Error())
		return raw
	}
	note := ""
	switch {
	case res.SkippedReason != "":
		note = "跳过排版：" + res.SkippedReason
	case res.PartialReason != "":
		note = "部分排版：" + res.PartialReason
	}
	if note != "" {
		fmt.Printf("[media] %s\n", note)
		logIt(note)
	}
	return res.Text
}

// applyMediaSummarySafely 转写成功后顺带生成内容总结写入正文，并应用 AI 标题（原标题记入元数据引用块）；
// 失败或未配置文本模型时跳过，只返回原正文。
func (l *Library) applyMediaSummarySafely(ctx context.Context, item map[string]any, content, transcript string) (newContent string, newTitle string, hasTitle bool) {
	cfg := l.resolveMediaSummaryConfig()
	if cfg == nil {
		return content, "", false
	}
	res, err := l.generateMediaSummary(ctx, mediaSummaryInput{Title: strField(item, "title"), Transcript: transcript}, *cfg, nil)
	if err != nil {
		fmt.Printf("[media] 内容总结生成失败，保留文字稿: %v\n", err)
		return content, "", false
	}
	fmt.Printf("[media] 内容总结完成（item=%s，%d 字）\n", strField(item, "id"), utf16Len(res.Summary))
	next := upsertMediaSummarySection(content, mediaSummaryHeading(strField(item, "itemType")), res.Summary)
	if res.HasTitle && res.Title != strField(item, "title") {
		return appendOriginalTitleNote(next, strField(item, "title")), res.Title, true
	}
	return next, "", false
}

// transcribeProgressTicker 按秒上报当前阶段与已用时长，覆盖整条链路（转写 → 排版 → 总结）：
// 这三步都以分钟计，只给转写计时的话，后两步期间界面会停在最后一次上报的数字上、看起来就像卡死了。
// 没有百分比是有意的，见 TranscribeProgress 的注释。
type transcribeProgressTicker struct {
	mu      sync.Mutex
	stage   string
	started time.Time
	emit    func()
	stop    chan struct{}
}

func (l *Library) startTranscribeProgress(itemID string, report bool) *transcribeProgressTicker {
	t := &transcribeProgressTicker{stage: "transcribing", started: time.Now(), stop: make(chan struct{})}
	if !report {
		t.emit = func() {}
		return t
	}
	t.emit = func() {
		t.mu.Lock()
		stage := t.stage
		t.mu.Unlock()
		p := map[string]any{"itemId": itemID, "stage": stage, "elapsedMs": time.Since(t.started).Milliseconds()}
		// 心跳来自本地 ASR 服务，只有转写阶段有；云端转写也没有，报出来会变成「一直卡着」
		if at := l.funasr().transcriptionActivityAt(); stage == "transcribing" && at.After(t.started) {
			p["stalledMs"] = time.Since(at).Milliseconds()
		}
		l.pushMain(chMediaTranscribeProgress, p)
	}
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				t.emit()
			case <-t.stop:
				return
			}
		}
	}()
	return t
}

func (t *transcribeProgressTicker) setStage(stage string) {
	t.mu.Lock()
	t.stage = stage
	t.mu.Unlock()
	t.emit()
}

func (t *transcribeProgressTicker) close() {
	select {
	case <-t.stop:
	default:
		close(t.stop)
	}
}

func optStr(s string) optString { return optString{Set: true, Val: s} }

// transcribeAndSave 预处理（ffmpeg 转码，不可用时原样直传）→ 转写 → AI 排版 → 内容总结 → 写回条目。
// 文字稿存 transcript 字段；总结写入正文总结小节并应用 AI 标题，历史转写状态注记一并清除。
func (l *Library) transcribeAndSave(ctx context.Context, item map[string]any, sourceFile, ffmpegExe string, cfg transcriptionModelConfig, diarize, report bool) (map[string]any, error) {
	prepared, err := prepareAudioForTranscription(ctx, sourceFile, ffmpegExe)
	if err != nil {
		return nil, err
	}
	defer prepared.Cleanup()
	itemID := strField(item, "id")
	progress := l.startTranscribeProgress(itemID, report)
	defer progress.close()

	rawText, err := l.transcribeMediaFile(ctx, prepared.FilePath, cfg, diarize)
	if err != nil {
		return nil, err
	}
	progress.setStage("formatting")
	text := l.formatTranscriptSafely(ctx, rawText, item)
	progress.setStage("summarizing")
	sourceContent := upsertTranscriptionSourceNote(strField(item, "content"), "音频识别（"+cfg.Model+"）")
	content, title, hasTitle := l.applyMediaSummarySafely(ctx, item, sourceContent, text)

	in := UpdateInput{Transcript: optStr(text)}
	if content != strField(item, "content") {
		in.Content = optStr(content)
	}
	if hasTitle {
		in.Title = optStr(title)
	}
	updated, err := l.knowledgeUpdate(ctx, itemID, in)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[media] 转写完成（item=%s，%d 字）\n", itemID, utf16Len(text))
	// 要求了分离却只出来一个说话人：聚类判定「只有一个人」。实测短音频（15 秒 3 轮）会塌成单说话人，
	// 而用户会以为功能没生效
	res := mediaResult(true)
	if updated != nil {
		res["item"] = updated
	}
	if diarize && len(listSpeakers(text)) <= 1 {
		res["warning"] = "只识别出一个说话人。音频较短或换人间隔太小时分不出来，全程单人说话时这也是正确结果。"
	}
	return res, nil
}

// retranscribeOnlineVideo 在线视频条目：按来源链接重新下载音轨并转写（重新生成文字稿）。
func (l *Library) retranscribeOnlineVideo(ctx context.Context, item map[string]any, ffmpegExe string, cfg transcriptionModelConfig, diarize, report bool) (map[string]any, error) {
	dir, file, err := l.downloadItemVideoAudio(ctx, item)
	if dir != "" {
		defer os.RemoveAll(dir)
	}
	if err != nil {
		if errors.Is(err, errYtDlpNotFound) {
			return mediaFail("尚未安装 yt-dlp——打开「设置 → 应用设置 → 采集」一键安装后重试"), nil
		}
		return nil, err
	}
	return l.transcribeAndSave(ctx, item, file, ffmpegExe, cfg, diarize, report)
}

func (l *Library) mediaTranscribe(ctx context.Context, itemID any, diarize bool) (map[string]any, error) {
	item, err := l.itemForMedia(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return mediaFail("条目不存在"), nil
	}
	if t := strField(item, "itemType"); t != "audio" && t != "video" {
		return mediaFail("仅音频 / 视频条目支持转写"), nil
	}
	cfg := l.resolveTranscriptionConfig()
	if cfg == nil {
		return mediaResult(false, "notConfigured", true, "error", "未配置语音转写模型"), nil
	}
	// 说话人分离是内置引擎的扩展能力，云端转写接口给不出来——
	// 静默忽略的话用户会以为分了、结果里却一个说话人都没有
	if diarize && !l.supportsDiarization(cfg.APIURL) {
		return mediaFail("区分说话人只有内置本地转写引擎支持，当前「语音转写」路由指向的是外部接口"), nil
	}
	ffmpegExe := l.resolveFfmpegExecutable(l.readToolPathSetting(ctx, "ffmpegPath"))

	run := func() (map[string]any, error) {
		// 目标是托管本地引擎时先确保服务已启动（首次约 10-20 秒）
		if err := l.funasr().ensureLocalTranscriptionService(cfg.APIURL); err != nil {
			return nil, err
		}
		asset, ok := extractLocalVideoRef(strField(item, "content"))
		if !ok {
			// 无本地媒体文件：尝试按来源链接重新下载音轨（在线视频）
			return l.retranscribeOnlineVideo(ctx, item, ffmpegExe, *cfg, diarize, true)
		}
		filePath := filepath.Join(l.paths.VideosDir(), asset)
		if _, err := os.Stat(filePath); err != nil {
			return mediaFail("媒体文件已不存在"), nil
		}
		return l.transcribeAndSave(ctx, item, filePath, ffmpegExe, *cfg, diarize, true)
	}
	res, err := run()
	if err != nil {
		return mediaFail(err.Error()), nil
	}
	return res, nil
}

func (l *Library) mediaFormatTranscript(ctx context.Context, itemID any, allowLong bool) (map[string]any, error) {
	item, err := l.itemForMedia(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return mediaFail("条目不存在"), nil
	}
	transcript := jsTrim(strField(item, "transcript"))
	if transcript == "" {
		return mediaFail("该条目还没有文字稿"), nil
	}
	cfg := l.resolveTranscriptFormatterConfig()
	if cfg == nil {
		return mediaResult(false, "notConfigured", true, "error", "未配置可用的文本模型"), nil
	}
	id := strField(item, "id")
	opts := formatTranscriptOptions{
		Glossary: resolveItemGlossary(item),
		OnProgress: func(current, total int) {
			l.pushMain(chMediaFormatProgress, map[string]any{"itemId": id, "current": current, "total": total})
		},
	}
	if allowLong { // 用户已在确认框里看过字数与预估请求次数，这里不再拦
		opts.MaxTotalChars = math.MaxInt32
	}
	res, err := l.formatTranscript(ctx, transcript, *cfg, opts)
	if err != nil {
		return mediaFail(err.Error()), nil
	}
	// 手动点的操作不能静默返回成功——没排版就得说清为什么
	if res.SkippedReason != "" {
		return mediaFail(res.SkippedReason), nil
	}
	updated, err := l.knowledgeUpdate(ctx, strField(item, "id"), UpdateInput{Transcript: optStr(res.Text)})
	if err != nil {
		return mediaFail(err.Error()), nil
	}
	partial := ""
	if res.PartialReason != "" {
		partial = "，部分排版"
	}
	fmt.Printf("[media] 文字稿排版完成（item=%s，%d 字%s）\n", id, utf16Len(res.Text), partial)
	// 排到一半照样落库（这些请求的钱已经花了），但不能报成完整成功
	out := mediaResult(true)
	if updated != nil {
		out["item"] = updated
	}
	if res.PartialReason != "" {
		out["warning"] = res.PartialReason
	}
	return out, nil
}

func (l *Library) mediaSummarize(ctx context.Context, itemID any) (map[string]any, error) {
	item, err := l.itemForMedia(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return mediaFail("条目不存在"), nil
	}
	itemType := strField(item, "itemType")
	if itemType == "forum" {
		return l.regenerateForumSummary(ctx, item), nil
	}
	if itemType != "audio" && itemType != "video" {
		return mediaFail("仅音频 / 视频条目支持内容总结"), nil
	}
	transcript := jsTrim(strField(item, "transcript"))
	if transcript == "" {
		return mediaFail("该条目还没有文字稿，请先生成文字稿"), nil
	}
	cfg := l.resolveMediaSummaryConfig()
	if cfg == nil {
		return mediaResult(false, "notConfigured", true, "error", "未配置可用的文本模型"), nil
	}
	res, err := l.generateMediaSummary(ctx, mediaSummaryInput{Title: strField(item, "title"), Transcript: transcript}, *cfg, nil)
	if err != nil {
		return mediaFail(err.Error()), nil
	}
	content := upsertMediaSummarySection(strField(item, "content"), mediaSummaryHeading(itemType), res.Summary)
	in := UpdateInput{Content: optStr(content)}
	if res.HasTitle && res.Title != strField(item, "title") {
		in.Content = optStr(appendOriginalTitleNote(content, strField(item, "title")))
		in.Title = optStr(res.Title)
	}
	updated, err := l.knowledgeUpdate(ctx, strField(item, "id"), in)
	if err != nil {
		return mediaFail(err.Error()), nil
	}
	fmt.Printf("[media] 内容总结完成（item=%s，%d 字）\n", strField(item, "id"), utf16Len(res.Summary))
	out := mediaResult(true)
	if updated != nil {
		out["item"] = updated
	}
	return out, nil
}

func (l *Library) registerMedia(b *Bridge) {
	b.handlers[chMediaTranscribe] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.mediaTranscribe(ctx, jsArg(a, 0), jsProp(jsArg(a, 1), "diarize") == true)
	}
	// 「语音转写」路由支持哪些可选能力：界面据此决定要不要摆出入口
	b.handlers[chMediaCapabilities] = func(context.Context, []json.RawMessage) (any, error) {
		cfg := l.resolveTranscriptionConfig()
		return map[string]any{"diarization": cfg != nil && l.supportsDiarization(cfg.APIURL)}, nil
	}
	// 已有文字稿的 AI 排版：不重新转写，直接整理 transcript 字段
	b.handlers[chMediaFormatTranscript] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.mediaFormatTranscript(ctx, jsArg(a, 0), jsProp(jsArg(a, 1), "allowLong") == true)
	}
	// 基于文字稿生成结构化「视频/音频总结」，写入正文的总结小节（已有则替换）
	b.handlers[chMediaSummarize] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.mediaSummarize(ctx, jsArg(a, 0))
	}
	b.handlers[chMediaRefreshForum] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		item, err := l.itemForMedia(ctx, jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		if item == nil {
			return mediaFail("条目不存在"), nil
		}
		return l.refreshForumDiscussion(ctx, item), nil
	}
	b.handlers[chMediaTestTranscription] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := jsArg(a, 0)
		get := func(k string) string { s, _ := jsProp(in, k).(string); return jsTrim(s) }
		apiURL, apiKey, model := get("apiUrl"), get("apiKey"), get("model")
		if apiURL == "" || apiKey == "" || model == "" {
			return map[string]any{"success": false, "error": "转写模型配置不完整"}, nil
		}
		if err := l.funasr().ensureLocalTranscriptionService(apiURL); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		latency, err := l.testTranscriptionConfig(ctx, transcriptionModelConfig{APIURL: apiURL, APIKey: apiKey, Model: model})
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true, "latency": latency}, nil
	}
}
