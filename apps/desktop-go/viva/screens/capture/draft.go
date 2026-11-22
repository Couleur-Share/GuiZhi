package capture

// 采集框草稿的解析：渲染层 packages/shared/utils/capture-input.ts 的 parseCaptureDraft 与 resolveCaptureAction 的 Go 对应实现。
// 判定规则（含「分享口令默认采集链接、普通网页夹说明文字默认存文本」）的原因见 TS 原文注释；改动必须与 TS 同步，
// 并由 draft_test.go 的表守住。

import "guizhi-mygo-shell/native/services"

// DraftKind 是草稿的判定类别（CaptureDraft.kind）。
type DraftKind uint8

const (
	// DraftEmpty 是空输入（只有空白也算）。
	DraftEmpty DraftKind = iota
	// DraftURLs 是全部由 http(s) 链接组成（按空白切分）：批量采集，批内重复去重。
	DraftURLs
	// DraftText 是纯文本。
	DraftText
	// DraftMixed 是文字里夹着链接：两种解释都成立，Prefer 定默认，界面上可以改判。
	DraftMixed
)

// ActionKind 是提交时真正要做的事（CaptureAction.kind）。
type ActionKind uint8

const (
	ActionEmpty ActionKind = iota
	ActionURLs
	ActionText
)

// Draft 是 ParseDraft 的结果。
type Draft struct {
	Kind DraftKind
	// URLs 在 DraftURLs 与 DraftMixed 下有值：前者是整段输入的链接，后者是从文字里抠出来的链接。
	URLs []string
	// Text 在 DraftText 与 DraftMixed 下有值：去掉首尾空白的原文。
	Text string
	// Prefer 只在 DraftMixed 下有意义：ActionURLs 或 ActionText。
	Prefer ActionKind
}

// Action 是 ResolveAction 的结果。
type Action struct {
	Kind ActionKind
	URLs []string // ActionURLs
	Text string   // ActionText
}

// hasPlatformConnector 报告该链接有专用采集连接器（视频平台或论坛），而不是走通用网页抓取。
func hasPlatformConnector(url string) bool {
	return services.DetectVideoPlatform(url) != "" || services.DetectForumPlatform(url) != ""
}

// ParseDraft 解析采集框内容（parseCaptureDraft）。
//
// 按空白切分：全部是 http(s) 链接才作为链接批处理；混进文字的走 DraftMixed——有专用连接器（视频平台、论坛）的链接默认采集链接，
// 其余默认存文本，两种都能在提示栏上一键改判。
func ParseDraft(value string) Draft {
	trimmed := jsTrim(value)
	if trimmed == "" {
		return Draft{Kind: DraftEmpty}
	}

	tokens := jsFields(trimmed)
	if len(tokens) > 0 && allHTTPURLLike(tokens) {
		// 同一批里重复粘贴的链接去重，避免生成重复任务。
		return Draft{Kind: DraftURLs, URLs: dedupe(tokens)}
	}

	if embedded := ExtractURLsFromText(trimmed); len(embedded) > 0 {
		prefer := ActionURLs
		for _, url := range embedded {
			if !hasPlatformConnector(url) {
				prefer = ActionText
				break
			}
		}
		return Draft{Kind: DraftMixed, URLs: embedded, Text: trimmed, Prefer: prefer}
	}

	return Draft{Kind: DraftText, Text: trimmed}
}

// ResolveAction 结合用户在提示栏上的改判算出提交时真正要做的事（resolveCaptureAction）。
// override 为 ActionEmpty 表示没有改判（TS 的 null）；非 DraftMixed 的判定原样透传。
func ResolveAction(d Draft, override ActionKind) Action {
	switch d.Kind {
	case DraftEmpty:
		return Action{Kind: ActionEmpty}
	case DraftURLs:
		return Action{Kind: ActionURLs, URLs: d.URLs}
	case DraftText:
		return Action{Kind: ActionText, Text: d.Text}
	}
	choice := d.Prefer
	if override != ActionEmpty {
		choice = override
	}
	if choice == ActionURLs {
		return Action{Kind: ActionURLs, URLs: d.URLs}
	}
	return Action{Kind: ActionText, Text: d.Text}
}

func allHTTPURLLike(tokens []string) bool {
	for _, token := range tokens {
		if !IsHTTPURLLike(token) {
			return false
		}
	}
	return true
}

// dedupe 按出现顺序去重（[...new Set(tokens)]）。
func dedupe(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
