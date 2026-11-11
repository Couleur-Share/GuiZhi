package main

// 对应 services/media/transcribe.ts 与 audio-preprocess.ts：音视频远程转写。
//
// 读取本地媒体资产，multipart 上传到 OpenAI 兼容的 /audio/transcriptions 接口，返回文字稿。
// 模型解析走 ai-models.json 的 audioText 路由（主进程直读，无需渲染进程转交凭据）。
// 上传前用 ffmpeg 转码为 16kHz 单声道 mp3（体积大幅缩小、容器统一）；ffmpeg 不可用时原文件直传。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	transcribeTimeout = 10 * time.Minute
	transcribeTestTTL = 60 * time.Second
	transcodeTimeout  = 10 * time.Minute
)

type transcriptionModelConfig struct {
	APIURL      string
	APIKey      string
	Model       string
	APIProtocol string
	Provider    string
}

// resolveTranscriptionConfig 解析 audioText 路由模型；未配置返回 nil。
func (l *Library) resolveTranscriptionConfig() *transcriptionModelConfig {
	cfg, err := l.readAIConfig()
	if err != nil {
		fmt.Printf("[media] 读取转写模型配置失败: %v\n", err)
		return nil
	}
	id, _ := cfg.Routes.vals["audioText"].(string)
	if id == "" {
		return nil
	}
	for _, m := range cfg.Models {
		if m.vals["id"] != id {
			continue
		}
		str := func(k string) string { s, _ := m.vals[k].(string); return jsTrim(s) }
		out := &transcriptionModelConfig{APIURL: str("apiUrl"), APIKey: str("apiKey"), Model: str("model")}
		if out.APIURL == "" || out.APIKey == "" || out.Model == "" {
			return nil
		}
		out.APIProtocol, _ = m.vals["apiProtocol"].(string)
		out.Provider, _ = m.vals["provider"].(string)
		return out
	}
	return nil
}

// buildTranscriptionsEndpoint 只有 OpenAI 兼容协议提供 multipart 的 /audio/transcriptions。
func buildTranscriptionsEndpoint(c transcriptionModelConfig) string {
	protocol := resolveAIProtocol(c.APIProtocol, c.Provider, c.APIURL)
	return buildTranscriptionsEndpointFromBase(resolveProtocolBase(c.APIURL, protocol))
}

// supportsDiarization 说话人分离是 Windows Python 引擎的扩展字段。
func (l *Library) supportsDiarization(apiURL string) bool {
	return l.funasr().supportsLocalDiarization(apiURL)
}

// requestTranscription 发起转写请求并返回原始文本（可能为空串——测试静音样本时属正常）。
func (l *Library) requestTranscription(ctx context.Context, filePath string, c transcriptionModelConfig, diarize bool) (string, error) {
	endpoint := buildTranscriptionsEndpoint(c)
	if endpoint == "" {
		return "", errors.New("当前模型的接口协议不支持语音转写：请改用 OpenAI 兼容的转写服务，或在设置里启用内置转写引擎")
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// 流式写 multipart：音频可能很大，不整个读进内存（对应 TS 的 openAsBlob 惰性读取）
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			part, err := mw.CreateFormFile("file", filepath.Base(filePath))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, f); err != nil {
				return err
			}
			_ = mw.WriteField("model", c.Model)
			_ = mw.WriteField("response_format", "json")
			if diarize && l.supportsDiarization(c.APIURL) {
				_ = mw.WriteField("diarize", "true")
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		pr.Close()
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", "node")
	resp, err := l.chatHTTP().Do(req)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return "", cause
		}
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := utf16Head(readBodyLossy(resp.Body), 300)
		return "", fmt.Errorf("转写请求失败 (HTTP %d): %s", resp.StatusCode, detail)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return "", cause
		}
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	text, ok := payload["text"].(string)
	if !ok {
		return "", errors.New("转写接口返回格式异常（缺少 text 字段）")
	}
	return text, nil
}

// CJK 文字与全角标点（按 UTF-16 码元的 BMP 区间；JS 里这个正则没有 u 标志，补充平面字符不在其中）。
func isTranscriptCJK(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303f, r >= 0x3040 && r <= 0x30ff, r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff, r >= 0xac00 && r <= 0xd7af, r >= 0xff00 && r <= 0xffef:
		return true
	}
	return false
}

var rePunctuationRun = regexp.MustCompile(`[，、；：。！？]{2,}`)

// cleanTranscriptText 转写文本清洗。
// CJK 与全角标点相邻处的空格是转写按 VAD 切段再拼接留下的噪音，不是分词；
// 开了 use_itn 的 SenseVoice 会在段边界叠出「高手，。」这类双标点，折叠时句末标点优先。
// 对已经排版规整的云端转写结果是幂等的，不会改动 ASCII 文本的空格。
func cleanTranscriptText(raw string) string {
	rs := []rune(raw)
	var sb strings.Builder
	for i := 0; i < len(rs); i++ {
		sb.WriteRune(rs[i])
		if !isTranscriptCJK(rs[i]) {
			continue
		}
		j := i + 1
		for j < len(rs) && (rs[j] == ' ' || rs[j] == '\t') {
			j++
		}
		if j > i+1 && j < len(rs) && isTranscriptCJK(rs[j]) {
			i = j - 1 // 吞掉这段空白
		}
	}
	out := rePunctuationRun.ReplaceAllStringFunc(sb.String(), func(run string) string {
		rr := []rune(run)
		for _, ch := range rr {
			if ch == '。' || ch == '！' || ch == '？' {
				return string(ch)
			}
		}
		return string(rr[0])
	})
	return jsTrim(out)
}

// recordTranscriptionUsage 转写记账。内置本地引擎不记——它跑在用户自己的 CPU 上，一分钱不花。
func (l *Library) recordTranscriptionUsage(ctx context.Context, c transcriptionModelConfig, failed bool) {
	if isManagedFunasrURL(c.APIURL) {
		return
	}
	l.recordMainAIUsage(ctx, mainAIUsage{Scenario: "transcription", Model: c.Model, Failed: failed})
}

func (l *Library) transcribeMediaFile(ctx context.Context, filePath string, c transcriptionModelConfig, diarize bool) (string, error) {
	// 超时预算在真正开始发请求时才起算：内置引擎一次只做一条，排队等前一条做完的时间不该记在自己头上
	send := func() (string, error) {
		tctx, cancel := context.WithTimeoutCause(ctx, transcribeTimeout, errors.New("转写请求超时"))
		defer cancel()
		return l.requestTranscription(tctx, filePath, c, diarize)
	}
	var raw string
	var err error
	if isManagedFunasrURL(c.APIURL) {
		raw, err = runExclusiveLocalTranscription(l.funasr(), send)
	} else {
		raw, err = send()
	}
	if err != nil {
		l.recordTranscriptionUsage(ctx, c, true)
		return "", err
	}
	l.recordTranscriptionUsage(ctx, c, false) // 转写接口不回报 token，只记调用次数
	text := cleanTranscriptText(raw)
	if text == "" {
		return "", errors.New("转写接口未返回有效文本")
	}
	return text, nil
}

// silentWav 生成指定时长的 16kHz 单声道 16bit 静音 WAV（连通性测试样本）。
func silentWav(seconds float64) []byte {
	const sampleRate = 16000
	sampleCount := max(1, int(seconds*sampleRate+0.5))
	dataSize := sampleCount * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	return buf
}

// testTranscriptionConfig 用静音样本发起一次真实转写请求，走与生产完全相同的链路（端点拼接 / 鉴权 / 代理）。
// 接口正常应答即视为成功（静音返回空文本属正常）。返回延迟毫秒。
func (l *Library) testTranscriptionConfig(ctx context.Context, c transcriptionModelConfig) (int64, error) {
	dir := filepath.Join(os.TempDir(), "guizhi-transcribe-test-"+randomHex(4))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	sample := filepath.Join(dir, "sample.wav")
	if err := os.WriteFile(sample, silentWav(0.5), 0o644); err != nil {
		return 0, err
	}
	start := time.Now()
	tctx, cancel := context.WithTimeoutCause(ctx, transcribeTestTTL, errors.New("转写测试超时"))
	defer cancel()
	if _, err := l.requestTranscription(tctx, sample, c, false); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// ── 音频预处理（audio-preprocess.ts）──

type preparedAudio struct {
	FilePath string
	Cleanup  func()
}

var errCanceledMsg = errors.New("已取消")

func runFfmpegTranscode(ctx context.Context, executable, input, output string) error {
	cctx, cancel := context.WithTimeoutCause(ctx, transcodeTimeout, errors.New("ffmpeg 转码超时"))
	defer cancel()
	cmd := exec.CommandContext(cctx, executable, "-y", "-loglevel", "error", "-i", input, "-vn", "-ac", "1", "-ar", "16000", "-b:a", "48k", output)
	hideChildWindow(cmd)
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return errCanceledMsg
	}
	if cause := context.Cause(cctx); cause != nil && cctx.Err() != nil {
		return cause
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Errorf("ffmpeg 退出码 %d: %s", ee.ExitCode(), utf16Head(jsTrim(stderr.String()), 300))
	}
	return err
}

// limitedBuffer 对应 TS 里 `if (stderr.length < 4096) stderr += chunk`：超过 4096 字符后不再收集。
type limitedBuffer struct{ b bytes.Buffer }

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.b.Len() < 4096 {
		w.b.Write(p)
	}
	return len(p), nil
}
func (w *limitedBuffer) String() string { return lossyUTF8(w.b.Bytes()) }

// prepareAudioForTranscription 转码到 16kHz 单声道 mp3；失败时返回原文件（Cleanup 为空操作）。
// 「已取消」除外——取消需要向上传播中断整个任务。
func prepareAudioForTranscription(ctx context.Context, filePath, ffmpegExecutable string) (preparedAudio, error) {
	if ctx.Err() != nil {
		return preparedAudio{}, errCanceledMsg
	}
	dir := filepath.Join(os.TempDir(), "guizhi-transcode-"+randomHex(4))
	output := filepath.Join(dir, "audio-16k.mp3")
	err := os.MkdirAll(dir, 0o755)
	if err == nil {
		err = runFfmpegTranscode(ctx, ffmpegExecutable, filePath, output)
	}
	if err == nil {
		return preparedAudio{FilePath: output, Cleanup: func() { _ = os.RemoveAll(dir) }}, nil
	}
	_ = os.RemoveAll(dir)
	if err == errCanceledMsg {
		return preparedAudio{}, err
	}
	fmt.Printf("[media] 音频转码不可用，原文件直传: %s\n", err.Error())
	return preparedAudio{FilePath: filePath, Cleanup: func() {}}, nil
}
