package main

// 已有视频条目的重新转写入口：对应 services/media/video-retranscription.ts。

import (
	"context"
	"errors"
)

// errYtDlpNotFound 对应 YtDlpNotFoundError。
var errYtDlpNotFound = errors.New("尚未安装 yt-dlp")

var knownParseCodes = map[string]bool{
	"structure_missing": true, "note_unavailable": true, "token_invalid": true, "guest_denied": true, "network": true,
}

// getPlatformParseCode 从错误对象 / 带 `[code]` 前缀的文案里抽出稳定错误码；没有返回空串。
func getPlatformParseCode(err error) string {
	var pe *platformParseError
	if errors.As(err, &pe) {
		return pe.Code
	}
	if m := reParseCodePrefix.FindStringSubmatch(err.Error()); m != nil && knownParseCodes[m[1]] {
		return m[1]
	}
	return ""
}

// newVideoDeps 读取设置组装视频依赖；测试通过 l.videoTestDeps 注入替身。
func (l *Library) newVideoDeps(ctx context.Context, onStage func(string)) videoDeps {
	var d videoDeps
	if l.videoTestDeps != nil {
		d = *l.videoTestDeps
		d.onStage = onStage
	} else {
		d = l.videoDepsFromSettings(ctx, onStage)
	}
	l.applyPlatformVideoDeps(&d, false, "") // 抖音公开作品的离屏页面回退
	return d
}

// downloadItemVideoAudio 按条目来源链接下载音轨到临时目录；重转写优先使用已保存的分享入口，
// 没有历史入口的旧条目仍尝试规范链接。目录由调用方清理（失败时也可能返回已建的目录）。
func (l *Library) downloadItemVideoAudio(ctx context.Context, item map[string]any) (dir, file string, err error) {
	canonical := jsTrim(strField(item, "sourceUri"))
	platform := detectVideoPlatform(canonical)
	if platform == "" {
		return "", "", errors.New("该条目没有本地媒体文件，来源链接也不是可解析的视频平台")
	}
	url := canonical
	rows, qerr := l.db.Query(ctx, `SELECT access_uri FROM source_records
       WHERE item_id = ? AND source_uri = ? AND source_type = 'url'
         AND access_uri IS NOT NULL AND trim(access_uri) <> ''
       ORDER BY captured_at DESC, id DESC LIMIT 1`, strField(item, "id"), canonical)
	if qerr == nil && len(rows.Values) > 0 {
		if access, _ := rows.Values[0][0].(string); access != "" && isHTTPURL(access) && detectVideoPlatform(access) == platform {
			url = access
		}
	}
	deps := l.newVideoDeps(ctx, nil)
	dir, file, err = l.downloadVideoAudio(ctx, url, platform, &deps)
	if err != nil && platform == "xiaohongshu" && getPlatformParseCode(err) == "token_invalid" {
		return dir, "", newPlatformParseError("token_invalid", "小红书访问链接已失效或缺少访问令牌。请在小红书中「分享 → 复制链接」，"+
			"将新链接重新采集以更新访问入口，再回到此条目重试；也请确认原笔记仍可打开。")
	}
	return dir, file, err
}
