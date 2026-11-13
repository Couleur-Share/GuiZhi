package main

// 登录态采集的页面级流程：登录提示、抖音详情、评论补采、NodeSeek 页面读取。
// 对应 platform-capture/{browser-capture,douyin-detail-capture,nodeseek-capture,source-comments}.ts
// 与 import/nodeseek.ts。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"guizhi-mygo-shell/extract"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

func parseWhatwgURL(raw string) (*whatwg.Url, error) { return whatwg.Parse(raw) }

// ---- 登录提示 ----

const loginIndicatorScript = `((currentPlatform) => {
  const indicatorId = "guizhi-platform-login-progress";
  document.getElementById(indicatorId)?.remove();
  const indicator = document.createElement("div");
  indicator.id = indicatorId;
  indicator.setAttribute("role", "status");
  indicator.setAttribute("aria-live", "polite");
  const isLinuxDo = currentPlatform === "linuxdo";
  Object.assign(indicator.style, {
    position: "fixed", left: "50%", top: isLinuxDo ? "24px" : "50%",
    transform: isLinuxDo ? "translateX(-50%)" : "translate(-50%, -50%)",
    zIndex: "2147483647", width: "min(460px, calc(100vw - 48px))", boxSizing: "border-box",
    padding: "20px 24px", borderRadius: "16px", background: "rgba(24, 24, 27, 0.94)", color: "#fff",
    boxShadow: "0 18px 60px rgba(0, 0, 0, 0.45)",
    fontFamily: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
    textAlign: "center", pointerEvents: "none", transition: "opacity 0.3s ease",
  });
  const title = document.createElement("div");
  title.textContent = currentPlatform === "douyin" ? "正在加载抖音官方登录框…"
    : isLinuxDo ? "正在加载 LINUX DO 验证页…" : "正在加载小红书官方登录框…";
  Object.assign(title.style, { fontSize: "16px", fontWeight: "650" });
  const detail = document.createElement("div");
  detail.textContent = isLinuxDo
    ? "请完成 Cloudflare 验证；若需访问私密版块可顺便登录，完成后提示会自动消失"
    : "登录组件出现后此提示会自动消失，请勿关闭窗口";
  Object.assign(detail.style, { marginTop: "8px", fontSize: "13px", lineHeight: "1.5", color: "rgba(255, 255, 255, 0.68)" });
  indicator.append(title, detail);
  document.body.appendChild(indicator);
  const readySelectors = currentPlatform === "douyin"
    ? ["#animate_qrcode_container img", "input[placeholder*='手机号']", "article.web-login-mobile-code"]
    : isLinuxDo
      ? ["#challenge-stage", "#cf-wrapper", "iframe[src*='challenges.cloudflare.com']", ".cf-turnstile", "#main-outlet", ".d-header", "#site-logo", "button.login-button", "header"]
      : ["div.login-container img", "input[placeholder*='手机号']", "input[placeholder*='验证码']"];
  const isReady = () => readySelectors.some((selector) => {
    const element = document.querySelector(selector);
    if (!element) return false;
    const rect = element.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return false;
    if (element instanceof HTMLImageElement) return element.complete && element.naturalWidth > 0;
    return true;
  });
  let pollTimer;
  const removeWhenReady = () => {
    if (!isReady()) return;
    observer.disconnect();
    if (pollTimer !== undefined) window.clearInterval(pollTimer);
    indicator.remove();
  };
  const observer = new MutationObserver(removeWhenReady);
  observer.observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ["src", "style", "class"] });
  window.setTimeout(() => {
    if (!indicator.isConnected) return;
    if (currentPlatform === "douyin") detail.textContent = "抖音登录服务响应较慢，仍在等待二维码或手机号表单…";
    else if (isLinuxDo) {
      detail.textContent = "正在等待 Cloudflare 响应，若页面出现验证框请手动完成…";
      window.setTimeout(() => { if (indicator.isConnected) indicator.remove(); }, 3000);
    } else detail.textContent = "小红书登录服务响应较慢，仍在等待登录表单…";
  }, 8000);
  pollTimer = window.setInterval(removeWhenReady, 500);
  removeWhenReady();
})`

func (s *platformSessions) showLoginLoadingIndicator(p *platformPage, platform string) {
	_, _ = p.eval(loginIndicatorScript + "(" + jsQuote(platform) + ")")
}

// ---- 抖音作品详情（公开作品回退，不要求登录）----

// douyinDetailResponseHTML 对应 douyinDetailResponseHtml：只消费目标作品，限制遍历规模，最新响应优先。
func douyinDetailResponseHTML(payloads []any, awemeID string) string {
	remaining := 4000
	var find func(v any, depth int) *jsObj
	find = func(v any, depth int) *jsObj {
		remaining--
		o, ok := v.(*jsObj)
		if remaining < 0 || depth > 10 || !ok {
			if a, isArr := v.(jsArr); isArr && remaining >= 0 && depth <= 10 {
				for _, e := range a {
					if item := find(e, depth+1); item != nil {
						return item
					}
				}
			}
			return nil
		}
		if jsToString(o.get("aweme_id")) == awemeID && o.get("aweme_id") != jsUndefined && (vTruthy(o.get("video")) || vTruthy(o.get("images"))) {
			return o
		}
		for _, k := range o.keys() {
			if item := find(o.get(k), depth+1); item != nil {
				return item
			}
		}
		return nil
	}
	for i := len(payloads) - 1; i >= 0; i-- {
		remaining = 4000
		if item := find(payloads[i], 0); item != nil {
			info := newObj()
			info.set("item_list", jsArr{item})
			capture := newObj()
			capture.set("videoInfoRes", info)
			loader := newObj()
			loader.set("capture", capture)
			data := newObj()
			data.set("loaderData", loader)
			s, _ := jsStringify(data)
			return "<script>window._ROUTER_DATA = " + s + "</script>"
		}
	}
	return ""
}

var reDouyinVerify = `/请完成下方验证|拖动滑块|安全验证|验证码/.test(document.body?.innerText ?? "")`

func (s *platformSessions) captureDouyinDetail(ctx context.Context, awemeID string) (string, error) {
	if !regexp.MustCompile(`^\d{6,}$`).MatchString(awemeID) {
		return "", errors.New("无效的抖音作品 ID")
	}
	var html string
	err := s.runSerialized(ctx, "douyin", "capture", platformRun{}, func(ctx context.Context, p *platformPage) error {
		cap := p.startJSONCapture()
		defer p.stopJSONCapture()
		if err := p.goto_("https://www.douyin.com/video/"+awemeID, 30*time.Second); err != nil {
			return err
		}
		// 详情常晚于 DOMContentLoaded 到达，等目标响应而不是固定睡眠后取首条 JSON
		for attempt := 0; attempt < s.detailPolls(); attempt++ {
			if ctx.Err() != nil {
				return pcErr("canceled", "已取消")
			}
			if !s.policy.allowedPlatformURL("douyin", p.url()) {
				return pcErr("platform_changed", "抖音作品页跳转到了不支持的地址")
			}
			if h := douyinDetailResponseHTML(cap.snapshot(), awemeID); h != "" {
				html = h
				return nil
			}
			if err := p.sleep(500 * time.Millisecond); err != nil {
				return pcErr("canceled", "已取消")
			}
		}
		verification := p.evalBool(reDouyinVerify)
		if verification {
			return pcErr("verification_required", "抖音要求安全验证，请在平台登录窗口完成验证后重试")
		}
		return pcErr("platform_changed", "抖音页面未返回目标作品详情，请检查网络或在平台登录窗口检查访问状态后重试")
	})
	return html, err
}

func (s *platformSessions) detailPolls() int {
	if s.detailPollCount > 0 {
		return s.detailPollCount
	}
	return 40
}

// ---- 评论补采 ----

type capturedComment struct {
	ExternalID  string
	AuthorName  string
	Content     string
	LikeCount   int64
	PublishedAt *int64
}

var reReplyKey = regexp.MustCompile(`(?i)(?:sub_?comments?|repl(?:y|ies))$`)

func numericOf(v any) float64 {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0
		}
		return x
	case string:
		t := jsTrim(x)
		if t == "" {
			return 0
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func firstNonNullish(o *jsObj, keys ...string) any {
	for _, k := range keys {
		if v := o.get(k); !isNullish(v) {
			return v
		}
	}
	return nil
}

// scanPlatformComments 对应同名函数：从页面自己的接口响应里递归找出顶层评论，按点赞数取前 limit 条。
func scanPlatformComments(payloads []any, limit int) []capturedComment {
	comments := map[string]capturedComment{}
	order := []string{}
	visited := 0
	var visit func(v any, depth int, insideReplies bool)
	visit = func(v any, depth int, insideReplies bool) {
		if len(comments) >= limit || visited > 20000 || depth > 12 || isNullish(v) {
			return
		}
		visited++
		if arr, ok := v.(jsArr); ok {
			for _, e := range arr {
				visit(e, depth+1, insideReplies)
			}
			return
		}
		rec, ok := v.(*jsObj)
		if !ok {
			return
		}
		content := cleanPlatformText(firstNonNullish(rec, "content", "text", "comment_text"), 5000)
		rawID := firstNonNullish(rec, "comment_id", "cid", "commentId")
		parentID := firstNonNullish(rec, "parent_comment_id", "parent_id", "reply_to_comment_id")
		isTopLevel := !insideReplies
		if isTopLevel {
			switch p := parentID.(type) {
			case nil:
			case string:
				isTopLevel = p == "" || p == "0"
			case float64:
				isTopLevel = p == 0
			default:
				isTopLevel = false
			}
		}
		_, idStr := rawID.(string)
		_, idNum := rawID.(float64)
		if isTopLevel && content != "" && (idStr || idNum) {
			var user *jsObj
			if u, ok := firstNonNullish(rec, "user", "author").(*jsObj); ok {
				user = u
			}
			name := any(nil)
			if user != nil {
				name = firstNonNullish(user, "nickname", "name")
			}
			if name == nil {
				name = rec.get("nickname")
			}
			externalID := jsToString(rawID)
			rawPub := numericOf(firstNonNullish(rec, "create_time", "time"))
			var published *int64
			if rawPub != 0 {
				ms := int64(rawPub)
				if rawPub < 10_000_000_000 {
					ms = int64(rawPub * 1000)
				}
				published = &ms
			}
			like := int64(math.Max(0, math.Floor(numericOf(firstNonNullish(rec, "digg_count", "like_count", "likes")))))
			if _, exists := comments[externalID]; !exists {
				order = append(order, externalID)
			}
			comments[externalID] = capturedComment{
				ExternalID: externalID, AuthorName: cleanPlatformText(name, 200), Content: content, LikeCount: like, PublishedAt: published,
			}
		}
		for _, k := range rec.keys() {
			visit(rec.get(k), depth+1, insideReplies || reReplyKey.MatchString(k))
		}
	}
	for _, p := range payloads {
		visit(p, 0, false)
	}
	out := make([]capturedComment, 0, len(order))
	for _, id := range order {
		out = append(out, comments[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LikeCount > out[j].LikeCount })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *platformSessions) captureComments(ctx context.Context, platform, url string, limit int) ([]capturedComment, error) {
	if err := s.requireKnownLogin(platform); err != nil {
		return nil, err
	}
	capped := 20
	if limit == 10 || limit == 20 || limit == 50 {
		capped = limit
	}
	var out []capturedComment
	err := s.runSerialized(ctx, platform, "comments", platformRun{}, func(ctx context.Context, p *platformPage) error {
		if err := s.requireLogin(p, platform); err != nil {
			return err
		}
		cap := p.startJSONCapture()
		if err := p.goto_(url, platformNavigationTimeout); err != nil {
			return err
		}
		if err := s.assertFinalURL(platform, p.url()); err != nil {
			return err
		}
		for i := 0; i < 4; i++ {
			if err := p.scrollBy(900); err != nil {
				return err
			}
			if err := p.sleep(900 * time.Millisecond); err != nil {
				return pcErr("canceled", "操作已取消")
			}
		}
		payloads := cap.snapshot()
		if isLoginPage(p.url()) || anyLoginRequired(payloads) {
			return s.loginExpired(platform)
		}
		out = scanPlatformComments(payloads, capped)
		return nil
	})
	return out, err
}

// captureSourceComments 对应 source-comments.ts：评论补采只服务小红书 / 抖音。
func (s *platformSessions) captureSourceComments(ctx context.Context, platform, url string, limit int) ([]capturedComment, error) {
	if platform != "xiaohongshu" && platform != "douyin" {
		return nil, errors.New("LINUX DO 楼层请在条目的讨论页刷新")
	}
	return s.captureComments(ctx, platform, url, limit)
}

// ---- NodeSeek ----

const nodeseekVerificationMessage = "[nodeseek_verification_required] NodeSeek 需要网页验证，请点击「验证并自动采集」后完成验证"

func nodeseekCanonicalURL(topicID string, page int) string {
	return fmt.Sprintf("https://www.nodeseek.com/post-%s-%d", topicID, page)
}

var reNodeseekPageSuffix = regexp.MustCompile(`-(\d+)$`)
var reNodeseekPageSuffixSlash = regexp.MustCompile(`-(\d+)/?$`)

func (s *platformSessions) nodeseekTarget(raw string) (string, error) {
	if s.nodeseekTopic != nil {
		if id, ok := s.nodeseekTopic(raw); ok {
			return id, nil
		}
		return "", errors.New("无效的 NodeSeek 帖子地址")
	}
	t := detectForumPlatform(raw)
	if !s.policy.allowedPlatformURL("nodeseek", raw) || t == nil || t.Platform != "nodeseek" {
		return "", errors.New("无效的 NodeSeek 帖子地址")
	}
	return t.TopicID, nil
}

func nodeseekVerificationURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("请从 NodeSeek 失败任务点击「验证并自动采集」")
	}
	u, err := parseWhatwgURL(raw)
	if err != nil {
		return "", errors.New("Invalid URL")
	}
	page := 1
	if m := reNodeseekPageSuffixSlash.FindStringSubmatch(u.Pathname()); m != nil {
		page, _ = strconv.Atoi(m[1])
	}
	t := detectForumPlatform(raw)
	if !isAllowedPlatformURL("nodeseek", raw) || t == nil || t.Platform != "nodeseek" {
		return "", errors.New("无效的 NodeSeek 帖子地址")
	}
	return nodeseekCanonicalURL(t.TopicID, page), nil
}

// readNodeseekPage 对应 readNodeseekPage：打开帖子页并轮询到正文可解析；verify 模式给用户 5 分钟完成人机验证。
func (s *platformSessions) readNodeseekPage(ctx context.Context, p *platformPage, raw string, verify bool) (string, error) {
	topicID, err := s.nodeseekTarget(raw)
	if err != nil {
		return "", err
	}
	u, err := parseWhatwgURL(raw)
	if err != nil {
		return "", errors.New("Invalid URL")
	}
	pageNumber := 1
	if m := reNodeseekPageSuffix.FindStringSubmatch(u.Pathname()); m != nil {
		pageNumber, _ = strconv.Atoi(m[1])
	}
	verificationMessage := nodeseekVerificationMessage + "（" + raw + "）"
	if ctx.Err() != nil {
		return "", pcErr("canceled", "操作已取消")
	}
	if pageNumber > 1 {
		if err := p.sleep(800 * time.Millisecond); err != nil {
			return "", err
		}
	}
	if s.nodeseekTopic == nil { // 真实站点才做 SSRF 校验；测试用本地模拟站点时跳过
		f := defaultSafeFetcher
		if s.l != nil && s.l.webFetcher != nil {
			f = s.l.webFetcher
		}
		if _, err := f.assertSafeTarget(ctx, u, true); err != nil {
			return "", err
		}
	}
	if err := p.goto_(raw, 45*time.Second); err != nil {
		return "", err
	}
	budget := 12 * time.Second
	if verify {
		budget = 5 * time.Minute
	}
	deadline := time.Now().Add(budget)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", pcErr("canceled", "操作已取消")
		}
		if p.closed() {
			return "", pcErr("browser_closed", "NodeSeek 验证窗口已关闭，任务尚未重试")
		}
		finalURL := p.url()
		if !s.policy.allowedPlatformURL("nodeseek", finalURL) {
			return "", pcErr("verification_required", verificationMessage)
		}
		html, cerr := p.content()
		if cerr != nil {
			// 验证完成的导航可能短暂销毁执行上下文，保持窗口等待下一次读取
			if !reCtxDestroyed.MatchString(cerr.Error()) {
				return "", cerr
			}
			if err := p.sleep(500 * time.Millisecond); err != nil {
				return "", err
			}
			continue
		}
		fu, ferr := parseWhatwgURL(finalURL)
		if ferr == nil && fu.Pathname() != u.Pathname() {
			lastErr = errors.New(verificationMessage)
		} else if _, perr := parseNodeseekPage(html, topicID, pageNumber); perr != nil {
			lastErr = perr
		} else {
			return html, nil
		}
		if err := p.sleep(500 * time.Millisecond); err != nil {
			return "", err
		}
	}
	if !verify && lastErr != nil && !strings.Contains(lastErr.Error(), "nodeseek_verification_required") {
		return "", pcErr("platform_changed", lastErr.Error())
	}
	if verify {
		return "", pcErr("login_timeout", "NodeSeek 验证等待已超过 5 分钟，请重试")
	}
	return "", pcErr("verification_required", verificationMessage)
}

var reCtxDestroyed = regexp.MustCompile(`(?i)context.*destroyed|frame.*disposed|navigat`)

// captureNodeseek 对应 captureNodeseek：整帖逐页串行采集，成功与失败都会同步登录标记。
func (s *platformSessions) captureNodeseek(ctx context.Context, topicID string) (*forumThread, error) {
	var thread *forumThread
	err := s.runSerialized(ctx, "nodeseek", "capture", platformRun{}, func(ctx context.Context, p *platformPage) error {
		t, err := fetchNodeseekThread(ctx, topicID, func(u string) (string, error) { return s.readNodeseekPage(ctx, p, u, false) })
		if err != nil {
			s.writeLoginState("nodeseek", false)
			if !isPlatformErrAny(err) && ctx.Err() == nil {
				return pcErr("platform_changed", err.Error())
			}
			return err
		}
		s.writeLoginState("nodeseek", true)
		thread = t
		return nil
	})
	return thread, err
}

func isPlatformErrAny(err error) bool {
	var pe *platformCaptureError
	return errors.As(err, &pe)
}

var _ = extract.NodeseekHTMLToMarkdown
var _ = url.PathEscape
