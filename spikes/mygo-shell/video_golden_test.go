package main

// 视频连接器差分测试：对拍 golden-video.mts 用真实 video-url.ts / video-captions.ts 录制的结果
// （GUIZHI_VIDEO_GOLDEN_DIR 或 GUIZHI_GOLDEN_DIR）。命令执行、转写、排版、总结全部是注入的脚本化替身。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadVideoGolden(t *testing.T) []forumRec {
	t.Helper()
	dir := os.Getenv("GUIZHI_VIDEO_GOLDEN_DIR")
	if dir == "" {
		dir = os.Getenv("GUIZHI_GOLDEN_DIR")
	}
	if dir == "" {
		t.Skip("未设置 GUIZHI_VIDEO_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-video.json"))
	if err != nil {
		t.Skip("未找到 golden-video.json：" + err.Error())
	}
	var g struct {
		Recs []forumRec `json:"recs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g.Recs
}

func TestVideoGolden(t *testing.T) {
	for _, r := range loadVideoGolden(t) {
		want := fgDecode(t, r.Output)
		t.Run(r.Kind+"/"+r.Name, func(t *testing.T) {
			switch r.Kind {
			case "describeFailure":
				var in string
				mustJSON(t, r.Input, &in)
				fgCompare(t, want, describeTranscriptionFailure(errors.New(in)))
			case "formatDuration":
				var in int
				mustJSON(t, r.Input, &in)
				fgCompare(t, want, formatVideoDuration(in))
			case "ytMeta":
				var in string
				mustJSON(t, r.Input, &in)
				m, err := parseYtDlpMetadata(in)
				if err != nil {
					fgCompare(t, want, map[string]any{"error": err.Error()})
					return
				}
				var dur any
				if m.DurationSeconds != nil {
					dur = *m.DurationSeconds
				}
				fgCompare(t, want, map[string]any{"ok": map[string]any{
					"title": m.Title, "uploader": m.Uploader, "durationSeconds": dur, "description": m.Description, "webpageUrl": m.WebpageURL,
				}})
			case "videoContent":
				var in struct {
					Meta struct {
						Title           string `json:"title"`
						Uploader        string `json:"uploader"`
						DurationSeconds *int   `json:"durationSeconds"`
						Description     string `json:"description"`
						WebpageURL      string `json:"webpageUrl"`
					} `json:"meta"`
					Platform string `json:"platform"`
					Note     string `json:"note"`
					Source   string `json:"source"`
				}
				mustJSON(t, r.Input, &in)
				m := ytDlpMetadata{Title: in.Meta.Title, Uploader: in.Meta.Uploader, DurationSeconds: in.Meta.DurationSeconds, Description: in.Meta.Description, WebpageURL: in.Meta.WebpageURL}
				fgCompare(t, want, buildVideoContent(m, in.Platform, in.Note, in.Source))
			case "captionText":
				var in string
				mustJSON(t, r.Input, &in)
				fgCompare(t, want, parseCaptionText(in))
			case "captions":
				vgCaptions(t, r, want)
			case "extract":
				vgExtract(t, r, want)
			default:
				if !imgnoteGolden(t, r, want) {
					t.Fatalf("未知基线类型 %s", r.Kind)
				}
			}
		})
	}
}

// vgReplaceDir 把参数里的临时目录换成占位符，与 TS 侧一致。
func vgReplaceDir(args []string, dir string) []any {
	out := []any{}
	for _, a := range args {
		if dir != "" {
			a = strings.ReplaceAll(a, dir, "<DIR>")
		}
		out = append(out, a)
	}
	return out
}

func vgDirOf(args []string) string {
	for i, a := range args {
		if a == "-o" && i+1 < len(args) {
			return filepath.Dir(args[i+1])
		}
	}
	return ""
}

func vgCaptions(t *testing.T, r forumRec, want any) {
	var in struct {
		Steps []struct {
			Error     string            `json:"error"`
			Files     map[string]string `json:"files"`
			RemoveDir bool              `json:"removeDir"`
		} `json:"steps"`
	}
	mustJSON(t, r.Input, &in)
	calls := []any{}
	n := 0
	run := func(_ context.Context, exe string, args []string, timeout time.Duration) (string, error) {
		dir := vgDirOf(args)
		calls = append(calls, map[string]any{"exe": exe, "args": vgReplaceDir(args, dir), "timeoutMs": int(timeout / time.Millisecond)})
		step := in.Steps[min(n, len(in.Steps)-1)]
		n++
		if step.RemoveDir {
			_ = os.RemoveAll(dir)
		}
		if step.Error != "" {
			return "", errors.New(step.Error)
		}
		for name, content := range step.Files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return "", nil
	}
	got, err := downloadPlatformCaptions(context.Background(), "/fake/yt-dlp", "https://v.example/1", run)
	if err != nil {
		t.Fatal(err)
	}
	var res any
	if got != nil {
		var lang any
		if got.Language != "" {
			lang = got.Language
		}
		res = map[string]any{"text": got.Text, "source": got.Source, "language": lang}
	}
	fgCompare(t, want, map[string]any{"result": res, "calls": calls})
}

func vgExtract(t *testing.T, r forumRec, want any) {
	var in struct {
		URL      string `json:"url"`
		Platform string `json:"platform"`
		Metadata *struct {
			Stdout   string `json:"stdout"`
			Error    string `json:"error"`
			NotFound bool   `json:"notFound"`
		} `json:"metadata"`
		Captions *struct {
			Published map[string]string `json:"published"`
			Auto      map[string]string `json:"auto"`
			Error     bool              `json:"error"`
		} `json:"captions"`
		Audio *struct {
			Files map[string]string `json:"files"`
			Error string            `json:"error"`
		} `json:"audio"`
		Transcription bool            `json:"transcription"`
		Transcribe    json.RawMessage `json:"transcribe"`
		Diarize       bool            `json:"diarize"`
		Formatter     *struct {
			Output  string `json:"output"`
			Error   string `json:"error"`
			Skipped string `json:"skipped"`
			Partial string `json:"partial"`
		} `json:"formatter"`
		Summary json.RawMessage `json:"summary"`
	}
	mustJSON(t, r.Input, &in)

	stages := []any{}
	runCalls := []any{}
	transcribeCalls := []any{}
	formatCalls := []any{}
	summarizeCalls := []any{}
	prepareCalls := []any{}

	run := func(_ context.Context, exe string, args []string, timeout time.Duration) (string, error) {
		dir := vgDirOf(args)
		runCalls = append(runCalls, map[string]any{"exe": exe, "args": vgReplaceDir(args, dir), "timeoutMs": int(timeout / time.Millisecond)})
		has := func(flag string) bool {
			for _, a := range args {
				if a == flag {
					return true
				}
			}
			return false
		}
		write := func(files map[string]string) {
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		switch {
		case has("--dump-json"):
			if in.Metadata != nil && in.Metadata.NotFound {
				return "", errYtDlpNotFound
			}
			if in.Metadata != nil && in.Metadata.Error != "" {
				return "", errors.New(in.Metadata.Error)
			}
			if in.Metadata != nil {
				return in.Metadata.Stdout, nil
			}
			return "", nil
		case has("--skip-download"):
			if in.Captions != nil && in.Captions.Error {
				return "", errors.New("no captions")
			}
			if in.Captions != nil {
				if has("--write-auto-subs") {
					write(in.Captions.Auto)
				} else {
					write(in.Captions.Published)
				}
			}
			return "", nil
		}
		if in.Audio != nil && in.Audio.Error != "" {
			return "", errors.New(in.Audio.Error)
		}
		if in.Audio != nil {
			write(in.Audio.Files)
		}
		return "", nil
	}

	cfg := transcriptionModelConfig{Model: "asr-model", APIURL: "http://example.test/v1", APIKey: "k", APIProtocol: "openai", Provider: "custom"}
	chatCfg := func(model string) *aiClientConfig {
		return &aiClientConfig{Provider: "custom", APIProtocol: "openai", APIKey: "k", APIURL: "http://x", Model: model}
	}
	deps := videoDeps{
		ytDlpPath: "/fake/yt-dlp", ffmpegPath: "/fake/ffmpeg", diarize: in.Diarize, run: run,
		onStage:            func(s string) { stages = append(stages, s) },
		ensureLocalService: func(string) error { return nil },
		getTranscriptionConfig: func() *transcriptionModelConfig {
			if in.Transcription {
				return &cfg
			}
			return nil
		},
		transcribe: func(_ context.Context, filePath string, _ transcriptionModelConfig, diarize bool) (string, error) {
			transcribeCalls = append(transcribeCalls, map[string]any{"file": filepath.Base(filePath), "diarize": diarize})
			var s string
			if len(in.Transcribe) == 0 {
				return "默认转写", nil
			}
			if json.Unmarshal(in.Transcribe, &s) == nil {
				return s, nil
			}
			var e struct {
				Error string `json:"error"`
			}
			mustJSON(t, in.Transcribe, &e)
			return "", errors.New(e.Error)
		},
		prepareAudio: func(_ context.Context, filePath, ffmpeg string) (preparedAudio, error) {
			prepareCalls = append(prepareCalls, map[string]any{"file": filepath.Base(filePath), "ffmpeg": ffmpeg})
			return preparedAudio{FilePath: filePath, Cleanup: func() {}}, nil
		},
		getFormatterConfig: func() *aiClientConfig {
			if in.Formatter != nil {
				return chatCfg("fm")
			}
			return nil
		},
		formatTranscript: func(_ context.Context, raw string, _ aiClientConfig, o formatTranscriptOptions) (formatTranscriptResult, error) {
			g := o.Glossary
			if g == nil {
				g = []string{}
			}
			formatCalls = append(formatCalls, map[string]any{"raw": raw, "glossary": g})
			if in.Formatter.Error != "" {
				return formatTranscriptResult{}, errors.New(in.Formatter.Error)
			}
			text := in.Formatter.Output
			if text == "" {
				text = raw
			}
			return formatTranscriptResult{Text: text, SkippedReason: in.Formatter.Skipped, PartialReason: in.Formatter.Partial}, nil
		},
		getSummaryConfig: func() *aiClientConfig {
			if s := strings.TrimSpace(string(in.Summary)); s != "" && s != "null" {
				return chatCfg("sm")
			}
			return nil
		},
		summarize: func(_ context.Context, si mediaSummaryInput, _ aiClientConfig, _ aiChatFunc) (mediaSummaryResult, error) {
			summarizeCalls = append(summarizeCalls, map[string]any{"title": si.Title, "context": si.Context, "transcript": si.Transcript})
			var s struct {
				Summary string  `json:"summary"`
				Title   *string `json:"title"`
				Error   string  `json:"error"`
			}
			mustJSON(t, in.Summary, &s)
			if s.Error != "" {
				return mediaSummaryResult{}, errors.New(s.Error)
			}
			res := mediaSummaryResult{Summary: s.Summary}
			if s.Title != nil {
				res.Title, res.HasTitle = *s.Title, true
			}
			return res, nil
		},
	}
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	ex, err := l.extractVideoURL(context.Background(), in.URL, in.Platform, deps)
	out := map[string]any{"result": nil, "error": nil, "stages": stages, "runCalls": runCalls, "transcribeCalls": transcribeCalls,
		"formatCalls": formatCalls, "summarizeCalls": summarizeCalls, "prepareCalls": prepareCalls}
	if err != nil {
		out["error"] = err.Error()
	} else {
		res := map[string]any{"title": ex.Title, "content": ex.Content, "itemType": ex.ItemType}
		if ex.SourceURI != nil {
			res["sourceUri"] = *ex.SourceURI
		} else {
			res["sourceUri"] = nil
		}
		if ex.Transcript != nil {
			res["transcript"] = *ex.Transcript
		} else if ex.DegradedReason == "" {
			res["transcript"] = nil // TS 正常结果里 transcript 为 null；降级结果没有该键
		}
		if ex.WarningReason != "" {
			res["warningReason"] = ex.WarningReason
		}
		if ex.DegradedReason != "" {
			res["degradedReason"] = ex.DegradedReason
		}
		out["result"] = res
	}
	fgCompare(t, want, out)
}
