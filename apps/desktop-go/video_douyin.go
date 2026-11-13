package main

// 抖音采集：不经 yt-dlp。对应 import/douyin.ts。
//
// yt-dlp 的 Douyin 提取器打的是 douyin.com/aweme/v1/web/aweme/detail/，该接口对没有签名 cookie 的请求一律返回空 body。
// 优先读取移动分享页的 window._ROUTER_DATA；分享页不再提供作品数据时，TS 版回退到离屏桌面页面读取页面自身加载的详情。
// 播放地址把 playwm 换成 play，继续复用既有下载和转写管线。
//
// 依赖未公开的页面结构，抖音改版就要跟着修——解析失败会降级成可读原因，不会静默产出空条目。
//
// 与 TS 版的差别（已登记在 MYGO-MIGRATION.md）：离屏页面回退依赖渲染宿主，尚未迁移；
// 没有注入 captureDetail 时，分享页缺数据直接把「分享页未返回作品数据」的原因报给用户。

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

const (
	// 分享页只在移动端 UA 下服务端渲染，桌面 UA 会被 302 到 douyin.com
	douyinMobileUA    = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	douyinRouterData  = "window._ROUTER_DATA"
	douyinMediaMax    = 300 << 20 // 与 yt-dlp 链路的 --max-filesize 300m 对齐
	douyinTitleMaxLen = 120
	douyinLabel       = "抖音"
)

var (
	reDouyinAwemePath = regexp.MustCompile(`/(?:video|note|slides)/(\d{6,})`)
	reDouyinAwemeID   = regexp.MustCompile(`^\d{6,}$`)
)

type douyinAweme struct {
	AwemeID string
	// 图文作品同样带 play_addr（抖音会拿图片合成幻灯片视频），所以只能按 images 判定
	Kind string // "video" | "note"
	// 抖音没有独立标题字段，标题取文案首行
	Title string
	// 完整文案；与标题相同（单行且未截断）时为空，避免正文里重复一遍
	Description     string
	Author          string
	DurationSeconds *int
	// 无水印播放地址；图文作品为空（那个幻灯片视频不值得转写）
	PlayURL string
	// 图文作品逐张图片的 CDN 镜像地址（视频作品为空）
	ImageMirrors [][]string
	// 规范化来源链接，参与去重：短链 / 分享链 / 站内链都收敛到同一个
	WebpageURL string
}

// douyinDeps 是测试注入点；全空时走真实实现。
type douyinDeps struct {
	// 整体替换分享页解析
	fetch func(ctx context.Context, url string) (*douyinAweme, error)
	// 替换分享页抓取
	fetchPage func(ctx context.Context, url string) (html, finalURL string, err error)
	// 分享页缺少数据时读取离屏页面自身的详情响应（渲染宿主迁移前默认不可用）
	captureDetail func(ctx context.Context, awemeID string) (string, error)
	// 替换无水印视频下载
	download func(ctx context.Context, playURL string) (dir, file string, err error)
}

// extractAwemeID 从各种抖音链接形态里取作品 ID；短链等取不到的返回 ""。
func extractAwemeID(raw string) string {
	u, err := whatwg.Parse(jsTrim(raw))
	if err != nil {
		return ""
	}
	if m := reDouyinAwemePath.FindStringSubmatch(u.Pathname()); m != nil {
		return m[1]
	}
	// douyin.com/discover?modal_id=xxx、用户主页上点开的作品
	if id, ok := searchParamGet(u, "modal_id"); ok && reDouyinAwemeID.MatchString(id) {
		return id
	}
	return ""
}

// douyinShareURL：/share/video/ 与 /share/note/ 互通，/share/slides/ 两种都不返回 _ROUTER_DATA，所以一律走 video。
func douyinShareURL(awemeID string) string {
	return "https://www.iesdouyin.com/share/video/" + awemeID + "/"
}

// douyinURLList 同一份资源抖音会给多个 CDN 地址，全部保留用于下载时逐个降级。
func douyinURLList(source any) []string {
	list, ok := jsOptGet(source, "url_list").(jsArr)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range list {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func douyinReadString(v any) string {
	if s, ok := v.(string); ok {
		return jsTrim(s)
	}
	return ""
}

// douyinBuildTitle 标题取文案首行：图文的文案常是整篇长文，直接截前 N 字会切出带换行的标题。
func douyinBuildTitle(desc, awemeID string) string {
	if t := noteFirstLineTitle(desc, douyinTitleMaxLen); t != "" {
		return t
	}
	return "抖音作品 " + awemeID
}

// sliceRouterDataJSON 从页面脚本里切出 window._ROUTER_DATA = {…} 的那个对象：按花括号配平扫描（跳过字符串字面量）。
func sliceRouterDataJSON(html string) (string, bool) {
	marker := strings.Index(html, douyinRouterData)
	if marker < 0 {
		return "", false
	}
	rel := strings.IndexByte(html[marker+len(douyinRouterData):], '{')
	if rel < 0 {
		return "", false
	}
	start := marker + len(douyinRouterData) + rel
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
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return html[start : i+1], true
			}
		}
	}
	return "", false
}

func (l *Library) douyinStructureMissing(html, message string) error {
	l.logPlatformStructureMissing(douyinLabel, douyinRouterData, html, "解析抖音分享页")
	return newPlatformParseError("structure_missing", message)
}

// jsObjectValues 对应 Object.values(v)：对象按属性顺序，数组按元素，其余没有可枚举值。
func jsObjectValues(v any) []any {
	switch x := v.(type) {
	case *jsObj:
		var out []any
		for _, k := range x.keys() {
			out = append(out, x.m[k])
		}
		return out
	case jsArr:
		return []any(x)
	}
	return nil
}

// jsRoundHalfUp 对应 Math.round：平局向 +∞ 取整。
func jsRoundHalfUp(d float64) float64 {
	r := math.Floor(d)
	if d-r >= 0.5 {
		return r + 1
	}
	return r
}

// parseDouyinRouterData 解析分享页里的 window._ROUTER_DATA；作品不可用时返回平台给的原因。
func (l *Library) parseDouyinRouterData(html, awemeID string) (*douyinAweme, error) {
	raw, ok := sliceRouterDataJSON(html)
	if !ok {
		return nil, l.douyinStructureMissing(html, "分享页未返回作品数据（抖音可能已改版，或该链接需要在 App 内打开）")
	}
	payload, err := jsParse(raw)
	if err != nil {
		return nil, l.douyinStructureMissing(html, "分享页数据解析失败（页面结构可能已变化）")
	}

	// 承载数据的 key 形如 video_(id)/page，按内容找比按名字找更耐改版
	var info any = jsUndefined
	for _, page := range jsObjectValues(jsGet(payload, "loaderData")) {
		if c := jsOptGet(page, "videoInfoRes"); vTruthy(c) {
			info = c
			break
		}
	}

	items, _ := jsOptGet(info, "item_list").(jsArr)
	var item any = jsUndefined
	for _, c := range items {
		if douyinReadString(jsOptGet(c, "aweme_id")) == awemeID {
			item = c
			break
		}
	}
	if item == jsUndefined && len(items) == 1 && douyinReadString(jsOptGet(items[0], "aweme_id")) == "" {
		item = items[0]
	}
	if !vTruthy(item) {
		var filtered any = jsUndefined
		if fl, ok := jsOptGet(info, "filter_list").(jsArr); ok && len(fl) > 0 {
			filtered = fl[0]
		}
		reason := douyinReadString(jsOptGet(filtered, "detail_msg"))
		if reason == "" {
			reason = douyinReadString(jsOptGet(filtered, "notice"))
		}
		if reason != "" && len(items) == 0 {
			return nil, newPlatformParseError("note_unavailable", reason)
		}
		return nil, l.douyinStructureMissing(html, "分享页未返回目标作品详情，无法判断作品是否可用")
	}

	desc := douyinReadString(jsGet(item, "desc"))
	resolvedID := douyinReadString(jsGet(item, "aweme_id"))
	if resolvedID == "" {
		resolvedID = awemeID
	}
	title := douyinBuildTitle(desc, resolvedID)
	video := jsGet(item, "video")
	var durationSeconds *int
	if ms, ok := jsOptGet(video, "duration").(float64); ok && !math.IsInf(ms, 0) && !math.IsNaN(ms) {
		n := int(jsRoundHalfUp(ms / 1000))
		durationSeconds = &n
	}
	playURL := ""
	if urls := douyinURLList(jsOptGet(video, "play_addr")); len(urls) > 0 {
		playURL = urls[0]
	}
	var mirrors [][]string
	switch imgs := jsGet(item, "images").(type) {
	case jsUndefinedT, jsNullT:
	case jsArr:
		for _, img := range imgs {
			if urls := douyinURLList(img); len(urls) > 0 {
				mirrors = append(mirrors, urls)
			}
		}
	default:
		return nil, errors.New("(item.images ?? []).map is not a function")
	}
	kind := "video"
	if len(mirrors) > 0 {
		kind = "note"
	}
	a := &douyinAweme{
		AwemeID: resolvedID, Kind: kind, Title: title, Description: desc,
		Author: douyinReadString(jsOptGet(jsGet(item, "author"), "nickname")), DurationSeconds: durationSeconds,
		ImageMirrors: mirrors,
		// 规范链接按解析到的实际内容定，比按原链接的路由猜更准
		WebpageURL: "https://www.douyin.com/" + kind + "/" + resolvedID,
	}
	if desc == title {
		a.Description = ""
	}
	if kind == "video" && playURL != "" {
		a.PlayURL = strings.Replace(playURL, "/playwm/", "/play/", 1) // 播放地址去水印
	}
	return a, nil
}

func (l *Library) douyinFetchPage(deps douyinDeps) func(context.Context, string) (string, string, error) {
	if deps.fetchPage != nil {
		return deps.fetchPage
	}
	return func(ctx context.Context, target string) (string, string, error) {
		f := l.webFetcher
		if f == nil {
			f = defaultSafeFetcher
		}
		res, err := f.fetchHTML(ctx, target, safeRequestOptions{UserAgent: douyinMobileUA})
		return res.HTML, res.FinalURL, err
	}
}

// fetchDouyinAweme 抓取并解析抖音作品。链接里带作品 ID 时一次请求即可；短链先跟随重定向拿到 ID，再取分享页。
func (l *Library) fetchDouyinAweme(ctx context.Context, url string, deps douyinDeps) (*douyinAweme, error) {
	if deps.fetch != nil {
		return deps.fetch(ctx, url)
	}
	fetchPage := l.douyinFetchPage(deps)
	awemeID := extractAwemeID(url)
	if awemeID == "" {
		_, finalURL, err := fetchPage(ctx, url)
		if err != nil {
			return nil, err
		}
		if awemeID = extractAwemeID(finalURL); awemeID == "" {
			return nil, newPlatformParseError("note_unavailable", "无法从该链接解析出抖音作品 ID")
		}
	}

	html, _, err := fetchPage(ctx, douyinShareURL(awemeID))
	if err != nil {
		return nil, err
	}
	aweme, perr := l.parseDouyinRouterData(html, awemeID)
	if perr == nil {
		return aweme, nil
	}
	var ppe *platformParseError
	if !errors.As(perr, &ppe) || ppe.Code != "structure_missing" {
		return nil, perr
	}
	if ctx.Err() != nil {
		return nil, errCanceledMsg
	}
	if deps.captureDetail == nil {
		return nil, perr // 渲染宿主迁移前：如实报告分享页缺数据
	}
	detail, err := deps.captureDetail(ctx, awemeID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errCanceledMsg
	}
	return l.parseDouyinRouterData(detail, awemeID)
}

// downloadDouyinMedia 下载无水印视频到临时目录，供转码转写使用。
func (l *Library) downloadDouyinMedia(ctx context.Context, playURL string) (string, string, error) {
	f := l.webFetcher
	if f == nil {
		f = defaultSafeFetcher
	}
	return f.downloadToTempFile(ctx, playURL, "douyin.mp4", douyinMediaMax,
		safeRequestOptions{UserAgent: douyinMobileUA, Referer: "https://www.douyin.com/"})
}

// imageExtensionFromURL 从图片地址取扩展名；资产入库要靠它决定文件后缀。
func imageExtensionFromURL(raw string) string {
	ext := ""
	if u, err := whatwg.Parse(raw); err == nil {
		ext = strings.ToLower(nodeExtname(u.Pathname()))
	}
	for _, e := range imageExts {
		if e == ext {
			return ext
		}
	}
	return ".webp"
}

// downloadDouyinImage 下载一张配图；同一张图抖音给了多个 CDN 镜像，逐个降级。
func (l *Library) downloadDouyinImage(ctx context.Context, mirrors []string) (string, string, error) {
	f := l.webFetcher
	if f == nil {
		f = defaultSafeFetcher
	}
	var failures []string
	for _, u := range mirrors {
		dir, file, err := f.downloadToTempFile(ctx, u, "image"+imageExtensionFromURL(u), mediaSizeLimits["image"],
			safeRequestOptions{UserAgent: douyinMobileUA, Referer: "https://www.douyin.com/"})
		if err == nil {
			return dir, file, nil
		}
		if isCanceledError(err) {
			return "", "", err
		}
		failures = append(failures, err.Error())
	}
	if len(failures) == 0 {
		return "", "", errors.New("没有可用的图片地址")
	}
	return "", "", errors.New(strings.Join(failures, "；"))
}

// douyinImageNoteSource 图文作品 → 通用图文条目素材。
func (l *Library) douyinImageNoteSource(a *douyinAweme) imageNoteSource {
	return imageNoteSource{
		PlatformLabel: douyinLabel, Title: a.Title,
		// 抖音没有标题字段，这个「标题」只是文案首行，配了文本模型就重拟
		AuthoredTitle: false, Description: a.Description, Author: a.Author,
		ImageMirrors: a.ImageMirrors, WebpageURL: a.WebpageURL, DownloadImage: l.downloadDouyinImage,
	}
}

func (l *Library) resolveDouyinSource(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error) {
	aweme, err := l.fetchDouyinAweme(ctx, url, deps.douyin)
	if err != nil {
		return nil, err
	}
	meta := ytDlpMetadata{Title: aweme.Title, Uploader: aweme.Author, DurationSeconds: aweme.DurationSeconds,
		Description: aweme.Description, WebpageURL: aweme.WebpageURL}

	if aweme.Kind == "note" {
		note := deps.imageNote
		note.onStage = deps.onStage
		return &mediaSource{
			metadata: meta,
			downloadAudio: func(context.Context) (string, string, error) {
				return "", "", errors.New("图文作品没有音轨")
			},
			note: func() (*extractedContent, error) {
				return l.buildImageNoteEntry(ctx, l.douyinImageNoteSource(aweme), note)
			},
		}, nil
	}

	if aweme.PlayURL == "" {
		return nil, errors.New("未能取到视频播放地址（页面结构可能已变化）")
	}
	download := deps.douyin.download
	if download == nil {
		download = l.downloadDouyinMedia
	}
	playURL := aweme.PlayURL
	return &mediaSource{
		metadata: meta,
		downloadAudio: func(c context.Context) (string, string, error) {
			return download(c, playURL)
		},
	}, nil
}
