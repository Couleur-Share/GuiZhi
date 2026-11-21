package library

import (
	"context"
	"errors"
	"strings"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 知识库详情里的三项 AI 功能背后的异步状态：AI 摘要（AiSummaryCard）、图中文字识别（AiOcrCard）、标签建议（AiTagSuggest）。
//
// 摘要与识别的「在途」标记属于工具侧栏这一代挂载（toolsState.gen，换条目、清详情、重新读取详情都会让它加一，对应渲染层按条目重建组件）：
// 旧一代在途的请求回来发现代号变了，就不碰新一代的标记、详情条目与展开序号，也不再弹提示（提示会落在用户已经离开的条目上，显得错乱）；
// 但请求本身已经写进了服务端，所以列表行照样更新，失败照样留日志。这与评论采集（RefreshComments）的处理一致。
// 标签建议没有「在途」状态：忙标记与候选 chips 属于标签浮层（tagpopover.go），这里只提供业务调用。

// showsLocked 报告详情里此刻显示的就是条目 id。mu 由调用方持有。
func (s *Store) showsLocked(id string) bool {
	cur := s.view.Detail.Item
	return cur != nil && cur.ID == id && s.view.List.SelectedID == id
}

func (s *Store) toastWarning(message, detail string) {
	if s.guard.Notify == nil {
		return
	}
	var opts []kit.ToastOption
	if detail != "" {
		opts = append(opts, kit.WithDetail(detail))
	}
	s.guard.Notify.Warning(message, opts...)
}

// openSettingsAI 跳转设置页的 AI 分区（渲染层 requestSettingsSection("ai")）。
func (s *Store) openSettingsAI() {
	if s.OnOpenSettingsAI != nil {
		s.OnOpenSettingsAI()
	}
}

// reportAIFailure 处理 AI 功能的失败（渲染层三个组件的 catch 分支）：
// 未配置模型提示 notConfigured 并跳设置页的 AI 分区；其余记日志（action 为日志里的动作名），并用 failed 包一层原因提示，原文放进可复制的详情。
// show 为假（请求已过期，用户离开了那个条目）时只留日志，不弹提示、不跳转。
func (s *Store) reportAIFailure(show bool, action string, err error, notConfigured string, failed func(message any) string) {
	if errors.Is(err, services.ErrAINotConfigured) {
		if show {
			s.toastError(notConfigured, "")
			s.openSettingsAI()
		}
		return
	}
	s.guard.Background(action, err)
	if show {
		msg := describeToolsError(err, locales.CommonUnknownError())
		s.toastError(failed(msg), msg)
	}
}

// ---- AI 摘要 ----

// GenerateSummary 为详情里的条目生成 AI 摘要并写回（AiSummaryCard 的 generate）。不阻塞：生成与写回放到后台。
//
// 没有详情、详情与工具侧栏不是同一条、生成在途时什么也不做；正文（含未落盘的编辑）为空白时提示「内容为空」、不发请求。
// 写回只带 summary 一个字段：详情里未落盘的标题与正文草稿不受影响（mergeServerItemLocked 重新叠上）。
// 写回成功后列表行跟上，仍在看这一条时详情换成服务端版本并让 SummaryExpandSeq 加一（卡片据此展开）；
// 摘要被截断时给警告（照常写入，但不装作完整），否则给成功提示。
func (s *Store) GenerateSummary(ctx context.Context) {
	s.mu.Lock()
	item := s.view.Detail.Item
	if item == nil || s.view.List.SelectedID != item.ID || s.view.Detail.Tools.ItemID != item.ID || s.detail.tools.summarizing {
		s.mu.Unlock()
		return
	}
	if strings.TrimSpace(item.Content) == "" {
		s.mu.Unlock()
		s.toastError(locales.LibraryAiSummaryEmpty(), "")
		return
	}
	gen, id, title, content := s.detail.tools.gen, item.ID, item.Title, item.Content
	s.detail.tools.summarizing = true
	s.view.Detail.Tools.SummaryGenerating = true
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runSummary(ctx, gen, id, title, content) })
}

func (s *Store) runSummary(ctx context.Context, gen int, id, title, content string) {
	res, err := s.svc.GenerateSummary(ctx, title, content)
	var updated services.LibraryItem
	var found bool
	if err == nil {
		updated, found, err = s.svc.UpdateItemFields(ctx, id, services.LibraryItemFieldsPatch{Summary: &res.Text})
		if err == nil && !found {
			// 条目在请求途中已被删除：绝不能报成功，与 store_detail.go 的 saveDraft 同一句诊断文案。
			err = errors.New("条目不存在或已被删除")
		}
	}

	s.mu.Lock()
	current := gen == s.detail.tools.gen
	if current {
		s.detail.tools.summarizing = false
		s.view.Detail.Tools.SummaryGenerating = false
	}
	if err == nil && found {
		s.applyEntryLocked(entryOf(updated))
		if current && s.showsLocked(id) {
			merged := s.mergeServerItemLocked(updated)
			s.view.Detail.Item = &merged
			s.view.Detail.Tools.SummaryExpandSeq++
		}
	}
	s.mu.Unlock()
	s.wake()

	switch {
	case err != nil:
		s.reportAIFailure(current, "library.aiSummary", err, locales.AskNotConfigured(), locales.LibraryAiSummaryFailed)
	case !current:
	case res.Truncated:
		s.toastWarning(locales.LibraryAiSummaryTruncated(), "")
	default:
		s.toastSuccess(locales.LibraryAiSummaryDone())
	}
}

// ---- 图中文字识别 ----

// RecognizeImages 识别图片条目里全部本地配图的文字，作为「图中文字」小节写回正文（AiOcrCard 的 recognize）。不阻塞：识别与写回放到后台。
//
// 只对图片类型且正文里有本地配图引用的条目生效；识别在途时什么也不做。逐张识别，单张失败不影响其余（记日志、记入失败清单）；
// 模型未配置立刻中止并跳设置页；全部没识别出文字提示失败（逐张原因放进详情）；写回基于发起时详情里的正文（含未落盘的编辑）。
// 写回成功后列表行跟上，仍在看这一条时：没有未落盘的正文改动就把正文基线同步成服务端版本（否则之后的保存会被误判为冲突），
// 有未落盘的正文改动则保留草稿（mergeServerItemLocked 叠回）。全部成功给成功提示，部分失败给警告并附逐张原因。
func (s *Store) RecognizeImages(ctx context.Context) {
	s.mu.Lock()
	item := s.view.Detail.Item
	if item == nil || s.view.List.SelectedID != item.ID || s.view.Detail.Tools.ItemID != item.ID ||
		item.ItemType != services.LibraryTypeImage || s.detail.tools.ocrRunning {
		s.mu.Unlock()
		return
	}
	refs := notes.ExtractLocalAssetRefs(item.Content, notes.LocalImage)
	if len(refs) == 0 {
		s.mu.Unlock()
		return
	}
	gen, id, content := s.detail.tools.gen, item.ID, item.Content
	s.detail.tools.ocrRunning = true
	s.view.Detail.Tools.OcrRunning = true
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runOcr(ctx, gen, id, content, refs) })
}

// releaseOcr 放下识别在途的标记（仅当这一代仍是当前），返回这一代是否仍是当前。
func (s *Store) releaseOcr(gen int) bool {
	s.mu.Lock()
	current := gen == s.detail.tools.gen
	if current {
		s.detail.tools.ocrRunning = false
		s.view.Detail.Tools.OcrRunning = false
	}
	s.mu.Unlock()
	s.wake()
	return current
}

func (s *Store) runOcr(ctx context.Context, gen int, id, content string, refs []string) {
	texts := make([]string, len(refs))
	var failures []string
	for i, name := range refs {
		text, err := s.svc.RecognizeImageText(ctx, name)
		if err == nil {
			texts[i] = text
			continue
		}
		if errors.Is(err, services.ErrAINotConfigured) {
			s.reportAIFailure(s.releaseOcr(gen), "library.ocr", err, locales.LibraryOcrNotConfigured(), locales.LibraryOcrFailed)
			return
		}
		s.guard.Background("library.ocr", err)
		failures = append(failures, locales.LibraryImageIndex(i+1)+": "+describeToolsError(err, locales.CommonUnknownError()))
	}
	// 逐张的原因要让用户看得到：九张里八张失败，不能只剩一条绿色的「完成」。
	failureDetail := strings.Join(failures, "\n")

	body := notes.BuildOcrSectionBody(texts)
	if body == "" {
		if s.releaseOcr(gen) {
			s.toastError(locales.LibraryOcrFailed(locales.LibraryOcrNoTextRecognized()), failureDetail)
		}
		return
	}

	nextContent := notes.ApplyOcrTextToContent(content, body)
	updated, found, err := s.svc.UpdateItemFields(ctx, id, services.LibraryItemFieldsPatch{Content: &nextContent})
	if err == nil && !found {
		err = errors.New("条目不存在或已被删除")
	}

	s.mu.Lock()
	current := gen == s.detail.tools.gen
	if current {
		s.detail.tools.ocrRunning = false
		s.view.Detail.Tools.OcrRunning = false
	}
	if err == nil && found {
		s.applyEntryLocked(entryOf(updated))
		if current && s.showsLocked(id) {
			s.syncContentBaseLocked(updated)
			merged := s.mergeServerItemLocked(updated)
			s.view.Detail.Item = &merged
		}
	}
	s.mu.Unlock()
	s.wake()

	switch {
	case err != nil:
		s.reportAIFailure(current, "library.ocr", err, locales.LibraryOcrNotConfigured(), locales.LibraryOcrFailed)
	case !current:
	case len(failures) > 0:
		s.toastWarning(locales.LibraryOcrPartial(len(failures)), failureDetail)
	default:
		s.toastSuccess(locales.LibraryOcrDone())
	}
}

// syncContentBaseLocked 在服务端正文被写回（识别结果）后，把该条目的草稿基线正文同步成服务端版本。
// 基线是编辑起点的字段：正文没有未落盘的改动时，它不跟上，之后保存别的字段（如标题）会带着过期的正文基线被判为冲突。
// 正文有待保存或在途的改动时不动基线，草稿保存仍按原基线比较。条目没有基线（没编辑过）时什么也不做。mu 由调用方持有。
func (s *Store) syncContentBaseLocked(updated services.LibraryItem) {
	id := updated.ID
	if pending, _ := s.detail.pending.get(id); pending.Content != nil || s.detail.saving[id].Content != nil {
		return
	}
	if base, ok := s.detail.bases[id]; ok {
		base.Content = services.StringPtr(updated.Content)
		s.detail.bases[id] = base
	}
}

// ---- 标签建议 ----

// SuggestItemTags 为条目生成 AI 建议标签（AiTagSuggest 的 generate），供标签浮层调用。
//
// 同步方法，会阻塞到模型返回，调用方（tagpopover.go）必须放到后台（Store.Spawn）里调用，完成后在界面线程上取走结果；
// 「生成中」忙标记、候选 chips 与「建议的标签都已存在」的判断（要用浮层里当前的标签）都归浮层，这里不保存状态。
// 正文从服务端现读（不含详情里未落盘的编辑，与渲染层一致）。
//
// 返回 ok 表示「产出了建议」：为真时 tags 是模型给的候选（可能为空）；为假表示没有产出建议、调用方保持原状，
// 原因有两种——条目不存在（静默）、调用失败（已提示：未配置模型会跳设置页的 AI 分区，其余记日志并弹出带可复制详情的错误提示）。
func (s *Store) SuggestItemTags(ctx context.Context, itemID string) (tags []string, ok bool) {
	item, found, err := s.svc.GetItem(ctx, itemID)
	if err == nil && found {
		tags, err = s.svc.SuggestTags(ctx, item.Title, item.Content)
	}
	if err != nil {
		s.reportAIFailure(true, "library.suggestTags", err, locales.AskNotConfigured(), locales.LibraryAiTagsFailed)
		return nil, false
	}
	if !found {
		return nil, false
	}
	return tags, true
}
