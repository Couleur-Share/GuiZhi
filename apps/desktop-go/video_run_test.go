package main

// 视频连接器的手写测试：真实子进程执行（超时 / 取消 / 退出码 / 找不到可执行文件）、ensureLocal 失败、取消传播、
// 导入分发。不访问真实网络、不运行真实 yt-dlp：用测试二进制自身充当假命令（helper process 模式）。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVideoHelperProcess(t *testing.T) {
	mode := os.Getenv("GUIZHI_VIDEO_HELPER")
	if mode == "" {
		t.Skip("仅作为子进程运行")
	}
	switch mode {
	case "ok":
		fmt.Println(`{"title":"来自假命令"}`)
	case "fail":
		for i := 1; i <= 5; i++ {
			fmt.Fprintf(os.Stderr, "错误行%d\n", i)
		}
		os.Exit(3)
	case "longfail":
		fmt.Fprint(os.Stderr, strings.Repeat("长", 500))
		os.Exit(4)
	case "sleep":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func runHelper(t *testing.T, ctx context.Context, mode string, timeout time.Duration) (string, error) {
	t.Helper()
	t.Setenv("GUIZHI_VIDEO_HELPER", mode)
	return defaultRunCommand(ctx, os.Args[0], []string{"-test.run=TestVideoHelperProcess"}, timeout)
}

func TestDefaultRunCommand(t *testing.T) {
	out, err := runHelper(t, context.Background(), "ok", 20*time.Second)
	if err != nil || !strings.Contains(out, "来自假命令") {
		t.Fatalf("正常执行：%q %v", out, err)
	}

	_, err = runHelper(t, context.Background(), "fail", 20*time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "yt-dlp 退出码 3: ") || !strings.Contains(err.Error(), "错误行3 错误行4 错误行5") || strings.Contains(err.Error(), "错误行2") {
		t.Fatalf("退出码错误应带 stderr 末 3 行：%v", err)
	}

	_, err = runHelper(t, context.Background(), "longfail", 20*time.Second)
	if err == nil || utf16Len(strings.TrimPrefix(err.Error(), "yt-dlp 退出码 4: ")) != 300 {
		t.Fatalf("stderr 摘要应截到 300 码元：%v", err)
	}

	start := time.Now()
	_, err = runHelper(t, context.Background(), "sleep", 500*time.Millisecond)
	if err == nil || err.Error() != "yt-dlp 执行超时" || time.Since(start) > 15*time.Second {
		t.Fatalf("超时应终止子进程：%v（%v）", err, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	_, err = runHelper(t, ctx, "sleep", 20*time.Second)
	if err == nil || err.Error() != "已取消" {
		t.Fatalf("取消应返回「已取消」：%v", err)
	}

	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if _, err = defaultRunCommand(cctx, os.Args[0], nil, time.Second); err == nil || err.Error() != "已取消" {
		t.Fatalf("已取消的上下文应直接返回：%v", err)
	}

	if _, err = defaultRunCommand(context.Background(), "Z:/不存在的/yt-dlp.exe", nil, time.Second); !errors.Is(err, errYtDlpNotFound) {
		t.Fatalf("找不到可执行文件应返回 errYtDlpNotFound：%v", err)
	}
	if _, err = defaultRunCommand(context.Background(), "guizhi-no-such-binary-xyz", nil, time.Second); !errors.Is(err, errYtDlpNotFound) {
		t.Fatalf("PATH 里找不到应返回 errYtDlpNotFound：%v", err)
	}
}

// 基础替身：元数据与字幕都由注入函数给出，音轨下载用 audio 函数。
func videoTestDeps(meta string) videoDeps {
	return videoDeps{
		ytDlpPath: "/fake/yt-dlp",
		run: func(_ context.Context, _ string, args []string, _ time.Duration) (string, error) {
			for _, a := range args {
				if a == "--dump-json" {
					return meta, nil
				}
			}
			return "", nil
		},
		getPlatformCaptions: func(context.Context) (*platformCaption, error) { return nil, nil },
		getTranscriptionConfig: func() *transcriptionModelConfig {
			return &transcriptionModelConfig{Model: "m", APIURL: "http://example.test"}
		},
		getFormatterConfig: func() *aiClientConfig { return nil },
		getSummaryConfig:   func() *aiClientConfig { return nil },
	}
}

func TestVideoEnsureLocalServiceFailureBecomesNote(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	deps := videoTestDeps(`{"title":"T","webpage_url":"https://w/x"}`)
	deps.ensureLocalService = func(string) error { return errors.New("本地转写引擎启动失败：端口被占用") }
	ex, err := l.extractVideoURL(context.Background(), "https://www.bilibili.com/video/BV1", "bilibili", deps)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Transcript != nil || ex.WarningReason != "文字稿生成失败：本地转写引擎启动失败：端口被占用" {
		t.Fatalf("引擎启动失败应写成转写注记：%+v", ex)
	}
	if !strings.Contains(ex.Content, "> 文字稿生成失败：本地转写引擎启动失败") {
		t.Fatalf("正文缺少注记：%s", ex.Content)
	}
}

func TestVideoCancelPropagation(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	for name, tweak := range map[string]func(*videoDeps, context.CancelFunc){
		"字幕阶段": func(d *videoDeps, cancel context.CancelFunc) {
			d.getPlatformCaptions = func(ctx context.Context) (*platformCaption, error) { cancel(); return nil, errCanceledMsg }
		},
		"元数据阶段": func(d *videoDeps, cancel context.CancelFunc) {
			d.run = func(ctx context.Context, _ string, _ []string, _ time.Duration) (string, error) {
				cancel()
				return "", errCanceledMsg
			}
		},
		"转写阶段": func(d *videoDeps, cancel context.CancelFunc) {
			d.ensureLocalService = func(string) error { return nil }
			d.run = func(_ context.Context, _ string, args []string, _ time.Duration) (string, error) {
				for i, a := range args {
					if a == "--dump-json" {
						return `{"title":"T"}`, nil
					}
					if a == "-o" {
						_ = os.WriteFile(args[i+1][:strings.LastIndex(args[i+1], "audio.")]+"audio.m4a", []byte("x"), 0o600)
					}
				}
				return "", nil
			}
			d.prepareAudio = func(_ context.Context, f, _ string) (preparedAudio, error) {
				return preparedAudio{FilePath: f, Cleanup: func() {}}, nil
			}
			d.transcribe = func(context.Context, string, transcriptionModelConfig, bool) (string, error) {
				cancel()
				return "", errCanceledMsg
			}
		},
		"总结阶段": func(d *videoDeps, cancel context.CancelFunc) {
			d.getPlatformCaptions = func(context.Context) (*platformCaption, error) {
				return &platformCaption{Text: "字幕", Source: captionSourceSubtitles}, nil
			}
			d.getSummaryConfig = func() *aiClientConfig { return &aiClientConfig{Model: "sm"} }
			d.summarize = func(context.Context, mediaSummaryInput, aiClientConfig, aiChatFunc) (mediaSummaryResult, error) {
				cancel()
				return mediaSummaryResult{}, errors.New("请求失败")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deps := videoTestDeps(`{"title":"T"}`)
			tweak(&deps, cancel)
			_, err := l.extractVideoURL(ctx, "https://www.bilibili.com/video/BV1", "bilibili", deps)
			if err == nil || err.Error() != "已取消" {
				t.Fatalf("取消应作为错误上抛而不是写进注记：%v", err)
			}
		})
	}
}

func TestVideoCaptionFailureFallsBackToASR(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	deps := videoTestDeps(`{"title":"T"}`)
	deps.getPlatformCaptions = func(context.Context) (*platformCaption, error) { return nil, errors.New("字幕接口 500") }
	deps.ensureLocalService = func(string) error { return nil }
	var asrCalled bool
	deps.run = func(_ context.Context, _ string, args []string, _ time.Duration) (string, error) {
		for i, a := range args {
			if a == "--dump-json" {
				return `{"title":"T"}`, nil
			}
			if a == "-o" {
				p := args[i+1]
				_ = os.WriteFile(strings.Replace(p, "audio.%(ext)s", "audio.mp3", 1), []byte("x"), 0o600)
			}
		}
		return "", nil
	}
	deps.prepareAudio = func(_ context.Context, f, _ string) (preparedAudio, error) {
		return preparedAudio{FilePath: f, Cleanup: func() {}}, nil
	}
	deps.transcribe = func(context.Context, string, transcriptionModelConfig, bool) (string, error) {
		asrCalled = true
		return "语音转写文字", nil
	}
	ex, err := l.extractVideoURL(context.Background(), "https://www.bilibili.com/video/BV1", "bilibili", deps)
	if err != nil || !asrCalled || ex.Transcript == nil || *ex.Transcript != "语音转写文字" {
		t.Fatalf("字幕失败应回落 ASR：%v %+v", err, ex)
	}
}

func TestVideoImportDispatchAndRetranscribeGuards(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	// 导入分发：视频链接走视频连接器（yt-dlp 未安装 → 降级而不是报错）
	deps := videoDeps{run: func(context.Context, string, []string, time.Duration) (string, error) { return "", errYtDlpNotFound }}
	l.videoTestDeps = &deps
	var stages []string
	ex, err := l.importExtractURL(context.Background(), map[string]any{"sourceInput": " https://www.bilibili.com/video/BV1xx "}, func(s string) { stages = append(stages, s) })
	if err != nil || ex.ItemType != "video" || !strings.Contains(ex.DegradedReason, "尚未安装 yt-dlp") || ex.Title != "https://www.bilibili.com/video/BV1xx" {
		t.Fatalf("未安装 yt-dlp 应降级：%v %+v", err, ex)
	}
	if len(stages) != 1 || stages[0] != "video-metadata" {
		t.Fatalf("阶段回调应经导入分发透传：%v", stages)
	}

	// 重转写：来源链接不是视频平台
	_, _, err = l.downloadItemVideoAudio(context.Background(), map[string]any{"id": "i1", "sourceUri": "https://example.com/a"})
	if err == nil || err.Error() != "该条目没有本地媒体文件，来源链接也不是可解析的视频平台" {
		t.Fatalf("非视频来源应拒绝：%v", err)
	}
	_, _, err = l.downloadItemVideoAudio(context.Background(), map[string]any{"id": "i1"})
	if err == nil {
		t.Fatal("无来源链接应拒绝")
	}
}

func TestPlatformParseCode(t *testing.T) {
	for in, want := range map[string]string{
		"[token_invalid] 缺令牌": "token_invalid", "[network] x": "network", "[unknown] x": "", "普通错误": "", "  [network] 前有空格": "",
	} {
		if got := getPlatformParseCode(errors.New(in)); got != want {
			t.Errorf("%q → %q，期望 %q", in, got, want)
		}
	}
	if got := getPlatformParseCode(newPlatformParseError("guest_denied", "x")); got != "guest_denied" {
		t.Errorf("结构化错误码：%q", got)
	}
}
