package main

// 小红书采集：不经 yt-dlp。对应 import/xiaohongshu.ts。
//
// yt-dlp 的 XiaoHongShu 提取器只出视频 formats，而小红书的主体内容是图文笔记（type: "normal"），
// 那条路对图文一律报「No video formats found」；它也不认 xhslink.com 短链。
// 笔记页本身是服务端渲染的，window.__INITIAL_STATE__ 里带着标题、文案、作者、配图与视频流地址，无需 cookie。三个约束都是实测出来的：
//  1. 必须用桌面 UA：移动 UA 拿到的是空壳页面（与抖音正好相反）。
//  2. 必须保留 xsec_token：去掉当场 302 到 /404。采集用的链接得是分享面板复制出来的那个。
//  3. __INITIAL_STATE__ 不是合法 JSON：里面有几十处裸 undefined 字面量。
//
// 依赖未公开的页面结构，改版就要跟着修；解析失败会降级成可读原因，不会静默产出空条目。

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

const (
	// 桌面 UA 是这条路的前提（移动 UA 只回壳页面）
	xhsDesktopUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	xhsStateMarker    = "window.__INITIAL_STATE__"
	xhsUndefinedLit   = "undefined"
	xhsMediaMax       = 300 << 20
	xiaohongshuLabel  = "小红书"
	xhsTokenInvalidZh = "小红书拒绝了该链接（缺少 xsec_token 访问令牌，或笔记已被删除）。请在小红书里用「分享 → 复制链接」重新复制完整链接。"
)

// /explore/<id> 与 /discovery/item/<id> 两种站内路径
var (
	reXhsNotePath = regexp.MustCompile(`(?i)/(?:explore|discovery/item)/([\da-f]{16,})`)
	reXhsTopic    = regexp.MustCompile(`#([^#\[\]]+)\[话题\]#`)
)

type xiaohongshuNote struct {
	NoteID string
	Kind   string // "video" | "note"
	Title  string
	// 标题取自笔记的标题字段（作者自己写的），而不是文案首行的兜底
	AuthoredTitle bool
	// 完整文案；与标题相同时为空，避免正文里重复一遍
	Description     string
	Author          string
	DurationSeconds *int
	// 视频的主源与备源地址（逐个降级用）；图文为空
	PlayURLs []string
	// 图文逐张图片的镜像地址（视频笔记为空，封面不入库）
	ImageMirrors [][]string
	// 规范化来源链接，参与去重：带 token 的分享链每次都不同，必须收敛
	WebpageURL string
}

// xiaohongshuDeps 是测试注入点；全空时走真实实现。
type xiaohongshuDeps struct {
	fetch     func(ctx context.Context, url string) (*xiaohongshuNote, error)
	fetchPage func(ctx context.Context, url string) (html, finalURL string, err error)
	download  func(ctx context.Context, playURLs []string) (dir, file string, err error)
}

// extractXhsNoteID 从站内链接里取笔记 ID（小写）；短链等取不到的返回 ""。
func extractXhsNoteID(raw string) string {
	u, err := whatwg.Parse(jsTrim(raw))
	if err != nil {
		return ""
	}
	if m := reXhsNotePath.FindStringSubmatch(u.Pathname()); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// xiaohongshuNoteURL 笔记的规范链接：分享链带的 token 每次都不同，去重只能认这一个形态。
func xiaohongshuNoteURL(noteID string) string {
	return "https://www.xiaohongshu.com/explore/" + noteID
}

// sliceInitialStateJSON 切出 window.__INITIAL_STATE__ = {…} 并就地补成合法 JSON：
// 按花括号配平扫描（跳过字符串字面量），顺手把字符串外的 undefined 换成 null。
// 必须区分串内串外：整段无脑替换会把文案里的 undefined 一起改掉。
func sliceInitialStateJSON(html string) (string, bool) {
	marker := strings.Index(html, xhsStateMarker)
	if marker < 0 {
		return "", false
	}
	rel := strings.IndexByte(html[marker+len(xhsStateMarker):], '{')
	if rel < 0 {
		return "", false
	}
	start := marker + len(xhsStateMarker) + rel
	var sb strings.Builder
	chunk := start
	depth, inString, escaped := 0, false, false
	for i := start; i < len(html); i++ {
		c := html[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				sb.WriteString(html[chunk : i+1])
				return sb.String(), true
			}
		case c == 'u' && strings.HasPrefix(html[i:], xhsUndefinedLit):
			sb.WriteString(html[chunk:i])
			sb.WriteString("null")
			i += len(xhsUndefinedLit) - 1
			chunk = i + 1
		}
	}
	return "", false
}

// xhsToHTTPS CDN 同时给 http 与 https，统一走 https。
func xhsToHTTPS(v any) string {
	s := douyinReadString(v)
	if strings.HasPrefix(s, "http://") {
		return "https://" + s[len("http://"):]
	}
	return s
}

func xhsDedupeURLs(candidates []any) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range candidates {
		if u := xhsToHTTPS(c); u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// cleanNoteText：话题在文案里的原始形态是 #AI漫剧[话题]#，界面上显示 #AI漫剧；原样留着正文没法看。
func cleanNoteText(text string) string { return reXhsTopic.ReplaceAllString(text, "#${1}") }

// noteFirstLineTitle 标题取文案首行（空则 ""），超长截断加省略号；抖音与小红书共用。
func noteFirstLineTitle(desc string, maxLen int) string {
	for _, line := range strings.Split(strings.ReplaceAll(desc, "\r\n", "\n"), "\n") {
		if t := jsTrim(line); t != "" {
			if utf16Len(t) > maxLen {
				return utf16Slice(t, maxLen) + "…"
			}
			return t
		}
	}
	return ""
}

func (l *Library) xhsStructureMissing(html, message string) error {
	l.logPlatformStructureMissing(xiaohongshuLabel, xhsStateMarker, html, "解析小红书笔记页")
	return newPlatformParseError("structure_missing", message)
}

// jsNullishReadError 复刻 TS 里对 null 取属性抛出的 TypeError 文案（调用方不依赖具体文字，仅用于可读性）。
func jsNullishReadError(prop string) error {
	return errors.New("Cannot read properties of null (reading '" + prop + "')")
}

// parseXiaohongshuNote 解析笔记页里的 __INITIAL_STATE__；取不到笔记时返回可读原因。
func (l *Library) parseXiaohongshuNote(html, urlNoteID string) (*xiaohongshuNote, error) {
	raw, ok := sliceInitialStateJSON(html)
	if !ok {
		return nil, l.xhsStructureMissing(html, "笔记页未返回数据（小红书可能已改版）")
	}
	state, err := jsParse(raw)
	if err != nil {
		return nil, l.xhsStructureMissing(html, "笔记页数据解析失败（页面结构可能已变化）")
	}

	noteState := jsOptGet(state, "note")
	detailMap, _ := jsOptGet(noteState, "noteDetailMap").(*jsObj)
	var keys []string
	if detailMap != nil {
		keys = detailMap.keys()
	}
	detailNote := func(id string) any {
		if detailMap == nil {
			return jsUndefined
		}
		return jsOptGet(detailMap.get(id), "note")
	}
	noteID := ""
	candidates := []string{urlNoteID, douyinReadString(jsOptGet(noteState, "firstNoteId"))}
	if len(keys) > 0 {
		candidates = append(candidates, keys[0])
	}
	for _, c := range candidates {
		if c != "" && vTruthy(detailNote(c)) {
			noteID = c
			break
		}
	}
	var item any = jsUndefined
	if noteID != "" {
		item = detailNote(noteID)
	}
	if noteID == "" || !vTruthy(item) {
		return nil, newPlatformParseError("note_unavailable",
			"页面里没有笔记内容（链接可能缺少 xsec_token 访问令牌，或笔记已被删除）。"+
				"请在小红书里用「分享 → 复制链接」重新复制完整链接。")
	}

	desc := cleanNoteText(douyinReadString(jsGet(item, "desc")))
	authoredTitle := cleanNoteText(douyinReadString(jsGet(item, "title")))
	title := authoredTitle
	if title == "" {
		if title = noteFirstLineTitle(desc, douyinTitleMaxLen); title == "" {
			title = xiaohongshuLabel + "笔记 " + noteID
		}
	}

	video := jsGet(item, "video")
	var streams []any
	for _, entries := range jsObjectValues(jsOptGet(jsOptGet(video, "media"), "stream")) {
		if arr, ok := entries.(jsArr); ok {
			streams = append(streams, []any(arr)...)
		}
	}
	var urlCandidates []any
	for _, s := range streams {
		if s == jsNull {
			return nil, jsNullishReadError("masterUrl")
		}
		urlCandidates = append(urlCandidates, jsGet(s, "masterUrl"))
		if backups, ok := jsGet(s, "backupUrls").(jsArr); ok {
			urlCandidates = append(urlCandidates, []any(backups)...)
		}
	}
	playURLs := xhsDedupeURLs(urlCandidates)
	// 类型按平台自己的标注分，不按「有没有配图」——视频笔记的 imageList 装的是封面
	kind := "note"
	if douyinReadString(jsGet(item, "type")) == "video" {
		kind = "video"
	}

	var mirrors [][]string
	if kind != "video" {
		switch list := jsGet(item, "imageList").(type) {
		case jsUndefinedT, jsNullT:
		case jsArr:
			for _, image := range list {
				if image == jsNull {
					return nil, jsNullishReadError("urlDefault")
				}
				cands := []any{jsGet(image, "urlDefault")}
				switch infos := jsGet(image, "infoList").(type) {
				case jsUndefinedT, jsNullT:
				case jsArr:
					for _, info := range infos {
						cands = append(cands, jsOptGet(info, "url"))
					}
				default:
					return nil, errors.New("(image.infoList ?? []).map is not a function")
				}
				cands = append(cands, jsGet(image, "urlPre"))
				if urls := xhsDedupeURLs(cands); len(urls) > 0 {
					mirrors = append(mirrors, urls)
				}
			}
		default:
			return nil, errors.New("(item.imageList ?? []).map is not a function")
		}
	}

	author := douyinReadString(jsOptGet(jsGet(item, "user"), "nickname"))
	if author == "" {
		author = douyinReadString(jsOptGet(jsGet(item, "user"), "nickName"))
	}
	n := &xiaohongshuNote{
		NoteID: noteID, Kind: kind, Title: title, AuthoredTitle: authoredTitle != "", Description: desc, Author: author,
		ImageMirrors: mirrors, WebpageURL: xiaohongshuNoteURL(noteID),
	}
	if desc == title {
		n.Description = ""
	}
	if kind == "video" {
		n.PlayURLs = playURLs
		n.DurationSeconds = xhsReadDuration(video, streams)
	}
	return n, nil
}

// xhsReadDuration 视频时长：流条目给的是毫秒，capa 给的是秒，两处都可能缺。
func xhsReadDuration(video any, streams []any) *int {
	finite := func(v any) (float64, bool) {
		f, ok := v.(float64)
		return f, ok && !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	for _, s := range streams {
		if ms, ok := finite(jsGet(s, "duration")); ok {
			n := int(jsRoundHalfUp(ms / 1000))
			return &n
		}
	}
	if sec, ok := finite(jsOptGet(jsOptGet(video, "capa"), "duration")); ok {
		n := int(jsRoundHalfUp(sec))
		return &n
	}
	return nil
}

func (l *Library) xhsFetchPage(deps xiaohongshuDeps) func(context.Context, string) (string, string, error) {
	if deps.fetchPage != nil {
		return deps.fetchPage
	}
	return func(ctx context.Context, target string) (string, string, error) {
		f := l.webFetcher
		if f == nil {
			f = defaultSafeFetcher
		}
		res, err := f.fetchHTML(ctx, target, safeRequestOptions{UserAgent: xhsDesktopUA})
		return res.HTML, res.FinalURL, err
	}
}

// fetchXiaohongshuNote 抓取并解析笔记。一次请求即可——短链（xhslink.com）由重定向自然落到带 token 的站内地址上。
func (l *Library) fetchXiaohongshuNote(ctx context.Context, url string, deps xiaohongshuDeps) (*xiaohongshuNote, error) {
	if deps.fetch != nil {
		return deps.fetch(ctx, url)
	}
	html, finalURL, err := l.xhsFetchPage(deps)(ctx, url)
	if err != nil {
		return nil, err
	}
	// 缺 token / 笔记已删时小红书不报错，而是 302 到站内 404 页——那个页面同样带 __INITIAL_STATE__，
	// 不先认出来的话，报错会变成含糊的「没有笔记内容」
	u, perr := whatwg.Parse(finalURL)
	if perr != nil {
		return nil, errors.New("Invalid URL")
	}
	if strings.HasPrefix(u.Pathname(), "/404") {
		return nil, newPlatformParseError("token_invalid", xhsTokenInvalidZh)
	}
	id := extractXhsNoteID(finalURL)
	if id == "" {
		id = extractXhsNoteID(url)
	}
	return l.parseXiaohongshuNote(html, id)
}

// downloadFromMirrors 逐个镜像降级下载；全挂了才返回错误，错误里带上每个镜像的原因。
func (l *Library) xhsDownloadFromMirrors(ctx context.Context, mirrors []string, fileName string, maxBytes int64) (string, string, error) {
	f := l.webFetcher
	if f == nil {
		f = defaultSafeFetcher
	}
	var failures []string
	for _, u := range mirrors {
		dir, file, err := f.downloadToTempFile(ctx, u, fileName, maxBytes,
			safeRequestOptions{UserAgent: xhsDesktopUA, Referer: "https://www.xiaohongshu.com/"})
		if err == nil {
			return dir, file, nil
		}
		if isCanceledError(err) {
			return "", "", err
		}
		failures = append(failures, err.Error())
	}
	if len(failures) == 0 {
		return "", "", errors.New("没有可用的下载地址")
	}
	return "", "", errors.New(strings.Join(failures, "；"))
}

func (l *Library) downloadXiaohongshuMedia(ctx context.Context, playURLs []string) (string, string, error) {
	return l.xhsDownloadFromMirrors(ctx, playURLs, "xiaohongshu.mp4", xhsMediaMax)
}

// downloadXiaohongshuImage 下载一张配图并按真实格式（文件头，不看 URL）定扩展名：
// 小红书图片地址结尾是 !nd_dft_wlteh_jpg_3 这样的处理指令，没有扩展名；OCR 的 mime 按扩展名推，猜错会让 Anthropic 判非法。
func (l *Library) downloadXiaohongshuImage(ctx context.Context, mirrors []string) (string, string, error) {
	dir, file, err := l.xhsDownloadFromMirrors(ctx, mirrors, "image.bin", mediaSizeLimits["image"])
	if err != nil {
		return "", "", err
	}
	renamed := filepath.Join(dir, "image"+sniffImageExt(file))
	if err := os.Rename(file, renamed); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return dir, renamed, nil
}

func (l *Library) xiaohongshuImageNoteSource(n *xiaohongshuNote) imageNoteSource {
	return imageNoteSource{
		PlatformLabel: xiaohongshuLabel, Title: n.Title,
		// 小红书有独立的标题字段，作者写了就照用：AI 重拟只会丢掉可辨识度
		AuthoredTitle: n.AuthoredTitle, Description: n.Description, Author: n.Author,
		ImageMirrors: n.ImageMirrors, WebpageURL: n.WebpageURL, DownloadImage: l.downloadXiaohongshuImage,
	}
}

func (l *Library) resolveXiaohongshuSource(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error) {
	note, err := l.fetchXiaohongshuNote(ctx, url, deps.xiaohongshu)
	if err != nil {
		return nil, err
	}
	meta := ytDlpMetadata{Title: note.Title, Uploader: note.Author, DurationSeconds: note.DurationSeconds,
		Description: note.Description, WebpageURL: note.WebpageURL}

	if note.Kind == "note" {
		imageDeps := deps.imageNote
		imageDeps.onStage = deps.onStage
		return &mediaSource{
			metadata: meta,
			downloadAudio: func(context.Context) (string, string, error) {
				return "", "", errors.New("图文笔记没有音轨")
			},
			note: func() (*extractedContent, error) {
				return l.buildImageNoteEntry(ctx, l.xiaohongshuImageNoteSource(note), imageDeps)
			},
		}, nil
	}
	if len(note.PlayURLs) == 0 {
		return nil, errors.New("未能取到视频播放地址（页面结构可能已变化）")
	}
	download := deps.xiaohongshu.download
	if download == nil {
		download = l.downloadXiaohongshuMedia
	}
	urls := note.PlayURLs
	return &mediaSource{
		metadata: meta,
		downloadAudio: func(c context.Context) (string, string, error) {
			return download(c, urls)
		},
	}, nil
}
