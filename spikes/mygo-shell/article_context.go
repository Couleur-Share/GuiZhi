package main

// 本文问答的上下文组装。对应 shared/utils/article-context.ts、reading-source.ts（读取部分）、
// image-note.ts（分段）与 services/article-ask-context.ts。
//
// 来源描述只含标识和摘录，不携带网络凭证。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

var (
	reArticleImage  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reArticleLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reArticleMarks  = regexp.MustCompile("[*_`#>]")
	reArticleWS     = regexp.MustCompile(fsp + `+`)
	reArticleBlocks = regexp.MustCompile(`\n` + fsp + `*\n`)
	reArticleTerms  = regexp.MustCompile(`[a-z0-9]{2,}|[\x{3400}-\x{9fff}]{2}`)
	reImageOnly     = regexp.MustCompile(`^(?:!\[[^\]]*\]\([^)]+\)` + fsp + `*)+$`)
	reBlankBlocks   = regexp.MustCompile(`\n{2,}`)
)

// normalizeArticleText 统一折叠空白，DOM 选段与 Markdown 正文可按相同文字匹配。
func normalizeArticleText(v string) string {
	v = reArticleImage.ReplaceAllString(v, "")
	v = reArticleLink.ReplaceAllString(v, "$1")
	v = reArticleMarks.ReplaceAllString(v, "")
	return jsTrim(reArticleWS.ReplaceAllString(v, " "))
}

// utf16Chunks 对应 `s.match(/[\s\S]{1,N}/g) ?? []`：按 UTF-16 码元切块（被切开的代理对在 Go 里落成 U+FFFD）。
func utf16Chunks(s string, n int) []string {
	if s == "" {
		return nil
	}
	u := utf16.Encode([]rune(s))
	var out []string
	for i := 0; i < len(u); i += n {
		end := i + n
		if end > len(u) {
			end = len(u)
		}
		out = append(out, string(utf16.Decode(u[i:end])))
	}
	return out
}

type articleSelected struct {
	Text    string
	Clipped bool
}

// selectArticleContext 优先保留选段及邻段，再按中英文词/双字匹配补充；无需全库索引。
func selectArticleContext(text, question, selection string, budget int) articleSelected {
	if utf16Len(text) <= budget {
		return articleSelected{Text: text}
	}
	var blocks []string
	for _, p := range reArticleBlocks.Split(strings.ReplaceAll(text, "\r\n", "\n"), -1) {
		for _, c := range utf16Chunks(p, 1200) {
			if normalizeArticleText(c) != "" {
				blocks = append(blocks, c)
			}
		}
	}
	selected := normalizeArticleText(selection)
	query := jsLower(normalizeArticleText(question + " " + selection))
	var terms []string
	seen := map[string]bool{}
	for _, t := range reArticleTerms.FindAllString(query, -1) {
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	type ranked struct{ i, score int }
	rank := make([]ranked, len(blocks))
	for i, b := range blocks {
		n := jsLower(normalizeArticleText(b))
		score := 0
		for _, t := range terms {
			if strings.Contains(n, t) {
				score++
			}
		}
		rank[i] = ranked{i, score}
	}
	sort.SliceStable(rank, func(a, b int) bool {
		if rank[a].score != rank[b].score {
			return rank[a].score > rank[b].score
		}
		return rank[a].i < rank[b].i
	})
	chosen := map[int]bool{}
	size := 0
	// 选段可能跨越分块；单独保留它，匹配到的相邻块也优先进入。
	lead := ""
	if selected != "" {
		lead = "选中段落：\n" + utf16Slice(selection, 4000) + "\n\n"
	}
	size += utf16Len(lead)
	add := func(i int) {
		if i >= 0 && i < len(blocks) && !chosen[i] && size+utf16Len(blocks[i]) <= budget {
			chosen[i] = true
			size += utf16Len(blocks[i]) + 2
		}
	}
	anchor := -1
	if selected != "" {
		head := utf16Slice(selected, 80)
		for i, p := range blocks {
			if strings.Contains(normalizeArticleText(p), head) {
				anchor = i
				break
			}
		}
	}
	if anchor >= 0 {
		for _, i := range []int{anchor, anchor - 1, anchor + 1} {
			add(i)
		}
	}
	for _, r := range rank {
		add(r.i)
	}
	idx := make([]int, 0, len(chosen))
	for i := range chosen {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	parts := make([]string, len(idx))
	for k, i := range idx {
		parts[k] = blocks[i]
	}
	return articleSelected{Text: lead + strings.Join(parts, "\n\n"), Clipped: true}
}

type imageNoteSections struct{ Caption, Recognized string }

// splitImageNoteSections 图片条目正文的分段：文案 / 图片 / 图中文字。
func splitImageNoteSections(content string) imageNoteSections {
	idx := strings.Index(content, ocrSectionHeading)
	head := content
	if idx >= 0 {
		head = content[:idx]
	}
	var kept []string
	for _, block := range reBlankBlocks.Split(head, -1) {
		if b := jsTrim(block); b != "" && !reImageOnly.MatchString(b) {
			kept = append(kept, b)
		}
	}
	out := imageNoteSections{Caption: strings.Join(kept, "\n\n")}
	if idx >= 0 {
		out.Recognized = jsTrim(content[idx:])
	}
	return out
}

// readingSourceText 取条目的阅读来源文本（正文或讨论总结）。
func readingSourceText(content, itemType, kind string) (string, error) {
	if itemType == "forum" {
		s := splitForumNoteSections(content)
		if kind == "summary" {
			return s.Summary, nil
		}
		return s.Body, nil
	}
	if kind == "summary" {
		return "", errors.New("此条目没有讨论总结")
	}
	body := content
	if b, ok := videoMetaBody(content); ok {
		body = b
	}
	if itemType == "image" {
		return splitImageNoteSections(body).Caption, nil
	}
	return body, nil
}

// ---- 目标与上下文 ----

var articleViews = []string{"body", "summary", "transcript", "recognized", "replies", "snapshot", "themed"}

var articleViewLabels = map[string]string{
	"body": "正文", "summary": "AI 讨论总结", "transcript": "文字稿", "recognized": "OCR 文本",
	"replies": "论坛讨论", "snapshot": "网页快照", "themed": "AI 阅读页",
}

type articleTarget struct {
	ItemID     string  `json:"itemId"`
	View       string  `json:"view"`
	SourceKind *string `json:"sourceKind,omitempty"`
	VersionID  *string `json:"versionId,omitempty"`
	Selection  *string `json:"selection,omitempty"`
	Floor      *int64  `json:"floor,omitempty"`
}

type articleSource struct {
	Ordinal     int            `json:"ordinal"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Text        string         `json:"text"`
	URL         string         `json:"url,omitempty"`
	Target      *articleTarget `json:"target,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	CapturedAt  *int64         `json:"capturedAt,omitempty"`
}

type articleContextOut struct {
	Target      articleTarget   `json:"target"`
	Title       string          `json:"title"`
	Fingerprint string          `json:"fingerprint"`
	Sources     []articleSource `json:"sources"`
	Clipped     bool            `json:"clipped"`
}

func parseArticleTarget(raw any) (articleTarget, error) {
	bad := errors.New("本文来源无效")
	itemID, ok := jsGet(raw, "itemId").(string)
	if !vTruthy(raw) || !ok || itemID == "" || utf16Len(itemID) > 200 {
		return articleTarget{}, bad
	}
	view, _ := jsGet(raw, "view").(string)
	valid := false
	for _, v := range articleViews {
		if v == view {
			valid = true
		}
	}
	if !valid {
		return articleTarget{}, bad
	}
	t := articleTarget{ItemID: itemID, View: view}
	if v := jsGet(raw, "versionId"); v != jsUndefined {
		s, ok := v.(string)
		if !ok || utf16Len(s) > 200 {
			return articleTarget{}, errors.New("来源版本无效")
		}
		t.VersionID = &s
	}
	if v := jsGet(raw, "sourceKind"); v != jsUndefined {
		s, ok := v.(string)
		if !ok || (s != "body" && s != "summary") {
			return articleTarget{}, errors.New("阅读来源无效")
		}
		t.SourceKind = &s
	}
	if v := jsGet(raw, "selection"); v != jsUndefined {
		s, ok := v.(string)
		if !ok || utf16Len(s) > 4000 {
			return articleTarget{}, errors.New("选段最多 4000 字，请缩小选择范围")
		}
		t.Selection = &s
	}
	if f, ok := jsGet(raw, "floor").(float64); ok && f == math.Trunc(f) && math.Abs(f) <= 9007199254740991 && f > 0 {
		n := int64(f)
		t.Floor = &n
	}
	return t, nil
}

func articleSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// themedArticleSource 取「AI 阅读页」版本的文本、原文与已有资料；AI 阅读页（themedReading）迁移前如实报告。
type themedArticleSource struct {
	text, original string
	prior          []articleSource
}

func (l *Library) themedArticleSource(ctx context.Context, itemID string, t articleTarget, question string) (themedArticleSource, error) {
	return themedArticleSource{}, errors.New("AI 阅读页尚未迁移到新内核")
}

// articleContext 组装本文问答的上下文来源（正文 / 总结 / 文字稿 / OCR / 论坛讨论 / 网页快照 / AI 阅读页）。
func (l *Library) articleContext(ctx context.Context, input any) (*articleContextOut, error) {
	target, err := parseArticleTarget(jsOptGet(input, "target"))
	if err != nil {
		return nil, err
	}
	question, ok := jsGet(input, "question").(string)
	if !ok || utf16Len(question) > 8000 {
		return nil, errors.New("问题最多 8000 字")
	}
	rows, err := l.db.Query(ctx, `SELECT id,title,content,transcript,item_type,deleted_at FROM knowledge_items WHERE id=?`, target.ItemID)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 || rows.Values[0][5] != nil {
		return nil, errors.New("文章不存在或已删除，请先恢复文章")
	}
	r := rows.Values[0]
	str := func(v any) string { s, _ := v.(string); return s }
	itemID, title, content, transcript, itemType := str(r[0]), str(r[1]), str(r[2]), str(r[3]), str(r[4])

	var text, original string
	var prior []articleSource
	switch target.View {
	case "snapshot":
		if target.VersionID == nil || *target.VersionID == "" {
			return nil, errors.New("网页快照尚未载入，请稍后重试")
		}
		versions, err := l.webSourceVersions(ctx, itemID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, v := range versions {
			if v.ID == *target.VersionID {
				text, found = v.Markdown, true
				break
			}
		}
		if !found {
			return nil, errors.New("正在查看的快照版本已不存在")
		}
	case "themed":
		if target.VersionID == nil || *target.VersionID == "" {
			return nil, errors.New("AI 阅读页尚未载入，请稍后重试")
		}
		src, err := l.themedArticleSource(ctx, itemID, target, question)
		if err != nil {
			return nil, err
		}
		text, original, prior = src.text, src.original, src.prior
	case "transcript":
		text = transcript
	case "recognized":
		text = splitImageNoteSections(content).Recognized
	case "replies":
		text = splitForumNoteSections(content).Replies
	default:
		text, err = readingSourceText(content, itemType, target.View)
		if err != nil {
			return nil, err
		}
	}
	if normalizeArticleText(text) == "" {
		return nil, errors.New("还没有可提问的文本，请先采集、识别或转写")
	}
	selection := ""
	if target.Selection != nil {
		selection = *target.Selection
	}
	if selection != "" && !strings.Contains(normalizeArticleText(text), normalizeArticleText(selection)) {
		return nil, errors.New("选段与当前保存的来源不一致，请重新选择或完成保存")
	}
	fingerprint := articleSHA256(text)
	selected := selectArticleContext(text, question, selection, 12000)
	label := articleViewLabels[target.View]
	if target.Floor != nil {
		label += " · 第 " + strconv.FormatInt(*target.Floor, 10) + " 楼"
	}
	sources := []articleSource{{Ordinal: 1, Kind: "article", Title: label + " · " + title, Text: selected.Text, Target: &target, Fingerprint: fingerprint}}
	if original == "" && target.View != "body" && target.View != "snapshot" {
		if original, err = readingSourceText(content, itemType, "body"); err != nil {
			return nil, err
		}
	}
	if original != "" && original != text {
		extra := selectArticleContext(original, question, "", 2500)
		sources = append(sources, articleSource{Ordinal: 2, Kind: "article", Title: "原文补充 · " + title, Text: extra.Text,
			Target: &articleTarget{ItemID: itemID, View: "body"}, Fingerprint: articleSHA256(original)})
	}
	for _, p := range prior {
		p.Ordinal = len(sources) + 1
		sources = append(sources, p)
	}
	return &articleContextOut{Target: target, Title: title, Fingerprint: fingerprint, Sources: sources, Clipped: selected.Clipped}, nil
}
