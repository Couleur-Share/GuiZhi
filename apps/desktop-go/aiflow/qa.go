package aiflow

// 全局问答引擎（对应 services/knowledge-ai/qa.ts，移植自 .NET 版 KnowledgeQaService）：
// Agent 工具循环（search / read / answer 动作协议，检索即推理）优先；
// 协议解析失败或纪律违规超限时，自动回退单发检索管线（弱模型兜底）。
//
// 依赖经 QADeps 注入（全是函数字段），测试用脚本化的假实现驱动。取消走 ctx，与 TS 一样只在固定检查点判断：
//   - 对话与条目检索（TS 里带 signal 的两处）收到 ctx；
//   - 其余是 IPC 调用（Wiki 目录、页面检索、读条目与页面），TS 不传 signal，这里用脱离取消的 ctx，
//     取消只在检查点生效，已发出的调用照常完成。

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ---- Agent 循环预算 ----
const (
	qaMaxToolRounds               = 6
	qaMaxProtocolViolations       = 2
	qaTranscriptBudget            = 8000
	qaItemReadLimit               = 1500
	qaWikiReadLimit               = 2000
	qaSearchItemLimit             = 5
	qaSearchWikiLimit             = 4
	qaLinkedResourceLimit         = 4
	qaMaxConsecutiveEmptySearches = 2
)

// ---- 单发兜底管线预算 ----
const (
	qaRetrievalLimit           = 5
	qaPerSourceContentLimit    = 1500
	qaTotalSourceContextBudget = 6000
	qaMinSourceChars           = 200
	qaHistoryTurnLimit         = 3
	qaHistoryAnswerLimit       = 400
)

const (
	qaTemperature = 0.2
	// qaMaxTokens：回答本身 300~500 token 足够，余量留给思考类模型的推理消耗（推理计入 max_tokens）。
	qaMaxTokens = 4096
)

var reLineBreakToSpace = regexp.MustCompile(`\r?\n`)

// QAEngine 驱动全局问答。
type QAEngine struct {
	Deps QADeps
	// Now 返回毫秒时间戳，给证据快照补采集时间；为 nil 时用系统时钟。
	Now func() int64
}

// agentResource 对应 AgentResource：循环里登记过的一份可读资料。
type agentResource struct {
	evidence *EvidenceSnapshot
	ordinal  int
	kind     QASourceKind
	refID    string
	title    string
	read     bool
	// matchText 是检索命中的片段，read 时据此定位该读文档的哪一段。
	matchText string
}

// ref 对应 {ordinal, kind, refId, title, evidence}：证据快照复制一份，免得调用方改动影响循环里的状态。
func (r *agentResource) ref() QASourceRef {
	out := QASourceRef{Ordinal: r.ordinal, Kind: r.kind, RefID: r.refID, Title: r.title}
	if r.evidence != nil {
		evidence := r.evidence.clone()
		out.Evidence = &evidence
	}
	return out
}

func resourceRefs(resources []*agentResource) []QASourceRef {
	refs := make([]QASourceRef, len(resources))
	for i, resource := range resources {
		refs[i] = resource.ref()
	}
	return refs
}

// getOrAddResource 取已登记的资料，没有就按下一个编号登记；后一轮检索可能命中同一文档的另一段，用最新的定位线索。
func getOrAddResource(resources *[]*agentResource, kind QASourceKind, refID, title, matchText string) *agentResource {
	for _, candidate := range *resources {
		if candidate.kind == kind && candidate.refID == refID {
			if matchText != "" {
				candidate.matchText = matchText
			}
			return candidate
		}
	}
	resource := &agentResource{ordinal: len(*resources) + 1, kind: kind, refID: refID, title: title, matchText: matchText}
	*resources = append(*resources, resource)
	return resource
}

// qaSession 是一次提问的运行状态。
type qaSession struct {
	deps         *QADeps
	now          func() int64
	ctx          context.Context
	storeCtx     context.Context
	question     string
	history      []QaTurn
	step         func(description string)
	onAnswerText func(text string)
}

func (s *qaSession) throwIfAborted() error {
	if s.ctx.Err() != nil {
		return ErrAborted
	}
	return nil
}

// Ask 对应 askKnowledgeBase：onStep 报告进度（检索词、阅读哪份资料），onAnswerText 在回答逐字到达时回调
// 「到目前为止的全文」，为 nil 则整段返回。取消用 IsAborted 判断；知识库没有相关资料返回 ErrQANoSource。
//
// 取消之后 onAnswerText 仍可能收到这一轮剩余的分块（TS 的回调同样没有取消判断，要等对话返回后的检查点才抛出），
// 界面层收到「已停止」后应忽略后续更新。
func (e *QAEngine) Ask(ctx context.Context, question string, history []QaTurn, onStep func(description string), onAnswerText func(text string)) (QAAnswer, error) {
	trimmedQuestion := jsTrim(question)
	if trimmedQuestion == "" {
		return QAAnswer{}, errors.New("请输入问题")
	}

	// 历史回答截断：完整回答可达千余 token，挤占资料预算
	var trimmedHistory []QaTurn
	for _, turn := range history[max(0, len(history)-qaHistoryTurnLimit):] {
		trimmedHistory = append(trimmedHistory, QaTurn{Question: turn.Question, Answer: TruncateText(turn.Answer, qaHistoryAnswerLimit)})
	}

	step := onStep
	if step == nil {
		step = func(string) {}
	}
	s := &qaSession{
		deps: &e.Deps, now: e.Now, ctx: ctx, storeCtx: context.WithoutCancel(ctx),
		question: trimmedQuestion, history: trimmedHistory, step: step, onAnswerText: onAnswerText,
	}
	answer, err := s.askByAgentLoop()
	if err != nil {
		return QAAnswer{}, err
	}
	if answer != nil {
		return *answer, nil
	}

	step("智能检索未成功，改用单次检索…")
	return s.askSingleShot()
}

func transcriptLength(transcript []string) int {
	total := 0
	for _, entry := range transcript {
		total += u16Len(entry)
	}
	return total
}

func anyRead(resources []*agentResource) bool {
	for _, resource := range resources {
		if resource.read {
			return true
		}
	}
	return false
}

func readResources(resources []*agentResource) []*agentResource {
	var out []*agentResource
	for _, resource := range resources {
		if resource.read {
			out = append(out, resource)
		}
	}
	return out
}

// wikiHit 是 Wiki 页面检索的一条命中（FTS 命中与目录子串匹配共用）。
type wikiHit struct {
	id, title, summary string
	kind               WikiPageKind
}

// askByAgentLoop 是 Agent 工具循环。返回 nil 答案（且无错误）表示应回退单发管线（协议失败）。
func (s *qaSession) askByAgentLoop() (*QAAnswer, error) {
	deps := s.deps
	// Wiki 目录取一次：编译知识参与检索与出链遍历（Wiki 为空时自然退化为条目检索）
	var catalog []WikiCatalogEntry
	if deps.WikiCatalog != nil {
		entries, err := deps.WikiCatalog(s.storeCtx)
		if err != nil {
			return nil, err
		}
		catalog = entries
	}

	var resources []*agentResource
	var transcript []string
	toolRounds, violations, consecutiveEmptySearches := 0, 0, 0
	forceAnswer := false
	// violate 记一次协议违规并追加纠正说明；超限返回 true，调用方回退单发管线。
	violate := func(correction string) (fallback bool) {
		violations++
		if violations > qaMaxProtocolViolations {
			return true
		}
		transcript = append(transcript, correction)
		return false
	}

	for {
		if err := s.throwIfAborted(); err != nil {
			return nil, err
		}

		if !forceAnswer && (toolRounds >= qaMaxToolRounds || transcriptLength(transcript) > qaTranscriptBudget) {
			forceAnswer = true
			transcript = append(transcript,
				"[系统] 检索预算已用尽，请立即基于已阅读的资料给出最终回答（action=answer），资料不足就说明缺少什么。")
		}

		// 每一轮都流式。哪一轮是回答轮事先不知道（模型多半在预算用尽前就自己给出 answer），
		// 而提取器只在动作确实是 answer 时才吐字，检索轮的动作 JSON 不会漏到界面上。
		// 还没读过任何资料时不流出：此时的 answer 会被判违规，不能在界面上留下半截模型猜测。
		stream := NewAnswerStream()
		var onDelta func(chunk string)
		if s.onAnswerText != nil && anyRead(resources) {
			onDelta = func(chunk string) {
				if text := stream.Push(chunk); text != "" {
					s.onAnswerText(text)
				}
			}
		}

		temperature := qaTemperature
		generation, err := deps.Chat(s.ctx, []ChatMessage{
			{Role: "system", Content: QAAgentSystemPrompt},
			{Role: "user", Content: BuildQaAgentPrompt(s.question, s.history, transcript)},
		}, ScenarioChatOptions{Temperature: &temperature, MaxTokens: qaMaxTokens, OnDelta: onDelta})
		if err != nil {
			return nil, err
		}
		if err := s.throwIfAborted(); err != nil {
			return nil, err
		}

		action, ok := tryParseAction(generation.Content)
		if !ok {
			if violate("[系统] 上一步输出无法解析。请严格只输出一个符合协议的 JSON 对象。") {
				return nil, nil
			}
			continue
		}

		if forceAnswer && action.kind != actionAnswer {
			if violate("[系统] 预算已用尽，只能给出最终回答（action=answer）。") {
				return nil, nil
			}
			continue
		}

		switch action.kind {
		case actionAnswer:
			read := readResources(resources)
			text := jsTrim(action.text)
			if len(read) == 0 || text == "" {
				correction := "[系统] 回答内容为空，请重新给出回答。"
				if len(read) == 0 {
					correction = "[系统] 给出回答前必须至少阅读一份资料（先 search 再 read）。"
				}
				if violate(correction) {
					return nil, nil
				}
				continue
			}

			readOrdinals := map[int]bool{}
			for _, resource := range read {
				readOrdinals[resource.ordinal] = true
			}
			cited := extractCitedOrdinals(text, readOrdinals)
			chosen := read
			if len(cited) > 0 {
				chosen = nil
				for _, resource := range read {
					if cited[resource.ordinal] {
						chosen = append(chosen, resource)
					}
				}
			}
			return &QAAnswer{
				Text:         text,
				Sources:      resourceRefs(chosen),
				Model:        generation.Model,
				UsedFallback: false,
				Truncated:    generation.FinishReason == "length",
			}, nil

		case actionSearch:
			toolRounds++
			s.step("检索：" + action.query)
			var lines []string

			// Wiki 页面检索在前（编译知识优先），条目融合检索在后。
			// 有 FTS 就走 FTS，没有才退回内存子串计分
			var wikiHits []wikiHit
			if deps.SearchWikiPages != nil {
				hits, err := deps.SearchWikiPages(s.storeCtx, action.query, qaSearchWikiLimit)
				if err != nil {
					return nil, err
				}
				for _, hit := range hits {
					wikiHits = append(wikiHits, wikiHit{id: hit.ID, title: hit.Title, summary: hit.Summary, kind: hit.Kind})
				}
			} else {
				matched := matchWikiPages(catalog, action.query)
				for _, entry := range matched[:min(qaSearchWikiLimit, len(matched))] {
					wikiHits = append(wikiHits, wikiHit{id: entry.ID, title: entry.Title, summary: entry.Summary, kind: entry.Kind})
				}
			}
			for _, entry := range wikiHits {
				resource := getOrAddResource(&resources, QAKindWiki, entry.id, entry.title, "")
				lines = append(lines, "["+strconv.Itoa(resource.ordinal)+"]（Wiki·"+WikiKindLabel(entry.kind)+"）《"+entry.title+"》："+entry.summary)
			}

			hits, err := deps.SearchItems(s.ctx, action.query, qaSearchItemLimit)
			if err != nil {
				return nil, err
			}
			for _, hit := range hits {
				resource := getOrAddResource(&resources, QAKindItem, hit.ID, hit.Title, hit.MatchText)
				excerpt := TruncateText(reLineBreakToSpace.ReplaceAllString(hit.Snippet, " "), 80)
				lines = append(lines, "["+strconv.Itoa(resource.ordinal)+"]（条目）《"+hit.Title+"》："+excerpt)
			}

			if len(lines) == 0 {
				consecutiveEmptySearches++
				transcript = append(transcript, "[工具] 搜索「"+action.query+"」无命中，请换一组关键词。")
				if consecutiveEmptySearches >= qaMaxConsecutiveEmptySearches {
					transcript = append(transcript, "[系统] 已连续多次无命中：换一个角度检索，或基于已读资料回答 / 说明资料不足。")
				}
			} else {
				consecutiveEmptySearches = 0
				transcript = append(transcript, "[工具] 搜索「"+action.query+"」结果：\n"+strings.Join(lines, "\n"))
			}

		case actionRead:
			toolRounds++
			var resource *agentResource
			for _, candidate := range resources {
				if float64(candidate.ordinal) == action.target {
					resource = candidate
					break
				}
			}
			if resource == nil {
				transcript = append(transcript, "[工具] 编号 "+jsNumberString(action.target)+" 不存在，请使用搜索结果中出现过的编号。")
				break
			}

			s.step("阅读：" + resource.title)
			var entry string
			var err error
			if resource.kind == QAKindItem {
				entry, err = s.readItemResource(resource)
			} else {
				entry, err = s.readWikiResource(resource, catalog, &resources)
			}
			if err != nil {
				return nil, err
			}
			transcript = append(transcript, entry)
			if deps.OnEvidence != nil {
				deps.OnEvidence(resourceRefs(readResources(resources)))
			}
		}
	}
}

func buildItemText(item *QAItemContent) string {
	if item.Transcript != nil && *item.Transcript != "" {
		return item.Content + "\n【口播转写稿】\n" + *item.Transcript
	}
	return item.Content
}

// itemIsEmpty 对应 !(item.content.trim() || item.transcript?.trim())。
func itemIsEmpty(item *QAItemContent) bool {
	return item == nil || (jsTrim(item.Content) == "" && (item.Transcript == nil || jsTrim(*item.Transcript) == ""))
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func formatInt64Ptr(n *int64) *string {
	if n == nil {
		return nil
	}
	return ptr(strconv.FormatInt(*n, 10))
}

// itemEvidence 为读到的条目留证据快照（text 是实际提供给模型的片段）。
func (s *qaSession) itemEvidence(refID string, item *QAItemContent, text string) EvidenceSnapshot {
	return CaptureEvidence(EvidenceInput{
		Kind: "item", SourceID: &refID, Title: item.Title, Text: text, SourceVersion: formatInt64Ptr(item.UpdatedAt),
		ReviewStatus: optionalString(item.ReviewStatus), ReviewReasons: item.ReviewReasons,
	}, s.now)
}

// readItemResource 读条目全文（视频/音频附转写稿）。
func (s *qaSession) readItemResource(resource *agentResource) (string, error) {
	item, err := s.deps.ReadItem(s.storeCtx, resource.refID)
	if err != nil {
		return "", err
	}
	if itemIsEmpty(item) {
		return "[工具] [" + strconv.Itoa(resource.ordinal) + "]《" + resource.title + "》不存在或内容为空。", nil
	}
	resource.read = true
	body := extractReadWindow(buildItemText(item), qaItemReadLimit, resource.matchText)
	evidence := s.itemEvidence(resource.refID, item, body)
	resource.evidence = &evidence
	return "[工具] [" + strconv.Itoa(resource.ordinal) + "]《" + item.Title + "》" +
		QualityNotice(item.ReviewStatus, item.ReviewReasons) + "内容：\n" + evidence.Text, nil
}

// readWikiResource 读 Wiki 页面，并把它的关联页面与来源条目注册为新的可读资料
// （编译期物化的链接在查询期成为遍历线索——检索即推理的核心收益）。
func (s *qaSession) readWikiResource(resource *agentResource, catalog []WikiCatalogEntry, resources *[]*agentResource) (string, error) {
	var detail *WikiPageDetail
	if s.deps.ReadWikiPage != nil {
		var err error
		if detail, err = s.deps.ReadWikiPage(s.storeCtx, resource.refID); err != nil {
			return "", err
		}
	}
	if detail == nil {
		return "[工具] [" + strconv.Itoa(resource.ordinal) + "]《" + resource.title + "》页面已不存在。", nil
	}

	resource.read = true
	sourceItemIDs := make([]string, 0, len(detail.Sources))
	reviewReasons := []string{}
	for _, source := range detail.Sources {
		sourceItemIDs = append(sourceItemIDs, source.ItemID)
		if source.ReviewStatus == "needs_review" {
			reviewReasons = append(reviewReasons, "待复核来源："+source.Title)
		}
	}
	reviewStatus := "clear"
	if len(reviewReasons) > 0 {
		reviewStatus = "needs_review"
	}
	evidence := CaptureEvidence(EvidenceInput{
		Kind: "wiki", SourceID: &resource.refID, SourceItemIDs: sourceItemIDs, Title: detail.Page.Title,
		Text:          TruncateText(detail.Page.Body, qaWikiReadLimit),
		SourceVersion: formatInt64Ptr(&detail.Page.UpdatedAt),
		ReviewStatus:  &reviewStatus, ReviewReasons: reviewReasons,
	}, s.now)
	resource.evidence = &evidence
	parts := []string{
		"[工具] [" + strconv.Itoa(resource.ordinal) + "]《" + detail.Page.Title + "》页面内容：",
		QualityNotice(evidence.ReviewStatus, evidence.ReviewReasons), evidence.Text,
	}

	// 出链：从正文 [[目标|显示]] 解析并按目录定位
	var linked []WikiCatalogEntry
	for _, entry := range resolveBodyLinks(detail.Page.Body, catalog) {
		if entry.ID != detail.Page.ID {
			linked = append(linked, entry)
		}
	}
	if len(linked) > qaLinkedResourceLimit {
		linked = linked[:qaLinkedResourceLimit]
	}
	if len(linked) > 0 {
		labels := make([]string, len(linked))
		for i, entry := range linked {
			resourceRef := getOrAddResource(resources, QAKindWiki, entry.ID, entry.Title, "")
			labels[i] = "[" + strconv.Itoa(resourceRef.ordinal) + "]《" + entry.Title + "》"
		}
		parts = append(parts, "关联页面（可继续 read）："+strings.Join(labels, "、"))
	}

	if len(detail.Sources) > 0 {
		sources := detail.Sources[:min(qaLinkedResourceLimit, len(detail.Sources))]
		labels := make([]string, len(sources))
		for i, source := range sources {
			resourceRef := getOrAddResource(resources, QAKindItem, source.ItemID, source.Title, "")
			labels[i] = "[" + strconv.Itoa(resourceRef.ordinal) + "]《" + source.Title + "》"
		}
		parts = append(parts, "来源条目（原文，可继续 read）："+strings.Join(labels, "、"))
	}

	return jsTrimEnd(strings.Join(parts, "\n")), nil
}

// buildRetrievalText 组装检索文本：新问题 + 最近一轮的问题（追问上下文），提高弱问题的召回。
func buildRetrievalText(question string, history []QaTurn) string {
	if len(history) > 0 {
		if last := history[len(history)-1].Question; last != "" {
			return question + " " + last
		}
	}
	return question
}

// askSingleShot 是单发检索管线：宽召回 → 组装资料上下文 → 生成回答并按引用对齐来源。
func (s *qaSession) askSingleShot() (QAAnswer, error) {
	deps := s.deps
	hits, err := deps.SearchItems(s.ctx, buildRetrievalText(s.question, s.history), qaRetrievalLimit)
	if err != nil {
		return QAAnswer{}, err
	}
	if err := s.throwIfAborted(); err != nil {
		return QAAnswer{}, err
	}
	if len(hits) == 0 {
		return QAAnswer{}, ErrQANoSource
	}

	// 取全文并组装资料上下文（打包到总预算：耗尽即停）
	var sources []QASourceRef
	var contextBlocks []string
	budgetRemaining := qaTotalSourceContextBudget
	ordinal := 1
	for _, hit := range hits {
		if budgetRemaining < qaMinSourceChars {
			break
		}
		item, err := deps.ReadItem(s.storeCtx, hit.ID)
		if err != nil {
			return QAAnswer{}, err
		}
		if itemIsEmpty(item) {
			continue
		}
		content := EvidenceText(extractReadWindow(buildItemText(item), min(qaPerSourceContentLimit, budgetRemaining), hit.MatchText))
		budgetRemaining -= u16Len(content)
		contextBlocks = append(contextBlocks, "["+strconv.Itoa(ordinal)+"] 《"+item.Title+"》"+QualityNotice(item.ReviewStatus, item.ReviewReasons)+"\n"+content)
		evidence := s.itemEvidence(hit.ID, item, content)
		sources = append(sources, QASourceRef{Ordinal: ordinal, Kind: QAKindItem, RefID: hit.ID, Title: item.Title, Evidence: &evidence})
		ordinal++
	}
	if err := s.throwIfAborted(); err != nil {
		return QAAnswer{}, err
	}
	if len(sources) == 0 {
		return QAAnswer{}, ErrQANoSource
	}

	if deps.OnEvidence != nil {
		deps.OnEvidence(cloneSourceRefs(sources))
	}
	// 兜底管线输出的是纯 Markdown，不用过 JSON 提取器
	var onDelta func(chunk string)
	if s.onAnswerText != nil {
		var streamed strings.Builder
		onDelta = func(chunk string) {
			streamed.WriteString(chunk)
			s.onAnswerText(streamed.String())
		}
	}
	temperature := qaTemperature
	generation, err := deps.Chat(s.ctx, []ChatMessage{
		{Role: "system", Content: QASystemPrompt},
		{Role: "user", Content: BuildQaUserPrompt(s.question, contextBlocks, s.history)},
	}, ScenarioChatOptions{Temperature: &temperature, MaxTokens: qaMaxTokens, OnDelta: onDelta})
	if err != nil {
		return QAAnswer{}, err
	}

	// 引用对齐：来源列表只保留回答实际标注引用的条目；未标注时退回完整列表保底可回溯
	validOrdinals := map[int]bool{}
	for _, source := range sources {
		validOrdinals[source.Ordinal] = true
	}
	cited := extractCitedOrdinals(generation.Content, validOrdinals)
	citedSources := sources
	if len(cited) > 0 {
		citedSources = nil
		for _, source := range sources {
			if cited[source.Ordinal] {
				citedSources = append(citedSources, source)
			}
		}
	}
	return QAAnswer{
		Text:         generation.Content,
		Sources:      citedSources,
		Model:        generation.Model,
		UsedFallback: true,
		Truncated:    generation.FinishReason == "length",
	}, nil
}

func cloneSourceRefs(sources []QASourceRef) []QASourceRef {
	out := make([]QASourceRef, len(sources))
	for i, source := range sources {
		out[i] = source
		if source.Evidence != nil {
			evidence := source.Evidence.clone()
			out[i].Evidence = &evidence
		}
	}
	return out
}
