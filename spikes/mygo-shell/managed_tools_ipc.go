package main

// 对应 ipc/media.ipc.ts 里 yt-dlp / ffmpeg 工具管理的 10 个频道：
// status / checkUpdate / install / remove / pickBinary / downloadProgress（推送）。
// FunASR 与转写、摘要等其余媒体频道见各自文件。

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	chYtDlpProgress  = "ytdlp:downloadProgress"
	chFfmpegProgress = "ffmpeg:downloadProgress"
)

// readToolPathSetting 对应 readToolPathSetting：设置里的自定义可执行文件路径（空/未配置返回空串）。
func (l *Library) readToolPathSetting(ctx context.Context, key string) string {
	raw, ok := l.readRawSetting(ctx, key)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal([]byte(raw), &s) != nil {
		return ""
	}
	return strings.TrimFunc(s, jsSpace)
}

func installResult(version string, err error) map[string]any {
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	return map[string]any{"success": true, "version": version}
}

func (l *Library) registerManagedTools(b *Bridge) {
	ytCache, ffCache := newStatusCache[toolStatus](), newStatusCache[toolStatus]()
	force := func(a []json.RawMessage) bool { return jsArg(a, 0) == true }
	pick := func(title string) (any, error) {
		if l.pickExecutable == nil {
			return nil, nil
		}
		p, err := l.pickExecutable(title)
		if err != nil || p == "" {
			return nil, err
		}
		rememberPickedBinaryPath(p) // 登记后 settings:set 才允许把它写进配置
		return p, nil
	}

	b.handlers["ytdlp:status"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		configured, f := l.readToolPathSetting(ctx, "ytDlpPath"), force(a)
		return ytCache.read(configured, func() (toolStatus, error) {
			return l.ytDlpStatus(ctx, configured, l.withCachedVersion(probeYtDlpVersion, f), nodePlatform()), nil
		}, f)
	}
	b.handlers["ytdlp:checkUpdate"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		// 只有当前就跑在内置版上才谈得上「更新」：来源是 PATH / 自定义路径时没有版本比较
		cur := l.ytDlpStatus(ctx, l.readToolPathSetting(ctx, "ytDlpPath"), l.withCachedVersion(probeYtDlpVersion, false), nodePlatform())
		var current *string
		if cur.Source != nil && *cur.Source == "managed" && cur.Version != "" {
			current = &cur.Version
		}
		return checkYtDlpUpdate(current, l.fetchLatestYtDlpVersion(ctx, ytDlpLatestReleaseURLs())), nil
	}
	b.handlers["ytdlp:install"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		defer ytCache.invalidate()
		v, err := l.installYtDlp(ctx, nodePlatform(), ytDlpDownloadURLs(nodePlatform()), ytDlpChecksumURLs(), func(p toolDownloadProgress) { l.pushMain(chYtDlpProgress, p) })
		return installResult(v, err), nil
	}
	b.handlers["ytdlp:remove"] = func(context.Context, []json.RawMessage) (any, error) {
		ytCache.invalidate()
		return l.removeManagedYtDlp()
	}
	b.handlers["ytdlp:pickBinary"] = func(context.Context, []json.RawMessage) (any, error) {
		return pick("选择 yt-dlp 可执行文件")
	}

	b.handlers["ffmpeg:status"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		configured, f := l.readToolPathSetting(ctx, "ffmpegPath"), force(a)
		return ffCache.read(configured, func() (toolStatus, error) {
			return l.ffmpegStatus(ctx, configured, l.withCachedVersion(probeFfmpegVersion, f), nodePlatform()), nil
		}, f)
	}
	b.handlers["ffmpeg:checkUpdate"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		cur := l.ffmpegStatus(ctx, l.readToolPathSetting(ctx, "ffmpegPath"), l.withCachedVersion(probeFfmpegVersion, false), nodePlatform())
		var current *string
		if cur.Source != nil && *cur.Source == "managed" && cur.Version != "" {
			current = &cur.Version
		}
		return checkFfmpegUpdate(current, l.fetchLatestFfmpegBuildDate(ctx, ffmpegDownloadURLs(nodePlatform()))), nil
	}
	b.handlers["ffmpeg:install"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		defer ffCache.invalidate()
		v, err := l.installFfmpeg(ctx, nodePlatform(), ffmpegDownloadURLs(nodePlatform()), func(p toolDownloadProgress) { l.pushMain(chFfmpegProgress, p) })
		return installResult(v, err), nil
	}
	b.handlers["ffmpeg:remove"] = func(context.Context, []json.RawMessage) (any, error) {
		ffCache.invalidate()
		return l.removeManagedFfmpeg()
	}
	b.handlers["ffmpeg:pickBinary"] = func(context.Context, []json.RawMessage) (any, error) {
		return pick("选择 ffmpeg 可执行文件")
	}
}
