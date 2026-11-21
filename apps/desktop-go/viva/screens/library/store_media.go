package library

import (
	"context"
	"errors"
	"strings"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 音视频转写、文字稿 AI 排版、媒体总结与刷新论坛讨论背后的异步状态
// （渲染层 components/library/use-media-actions.ts 的 useTranscriptActions、useMediaSummaryAction、useForumDiscussionRefreshAction）。
//
// 渲染层里这些状态都是 hook 的组件状态，随条目 ID 重置（effect 依赖 itemId）：这里同样挂在详情上，换条目、清详情时整体清空，
// mediaState.gen 随之加一——旧一代在途的动作回来发现代号变了，就不碰新一代的忙标记、详情条目与提示（提示会落在用户已经离开的条目上，显得错乱）；
// 但动作本身已经写进了服务端，所以列表行照样更新，失败照样留日志。这与 AI 摘要、图中文字识别（store_ai.go）的处理一致。
//
// 防重入直接看 MediaView 里的忙标记（它们与代号在同一处重置，不另存一份副本）：转写只看 RunningAction（两个转写按钮共用），
// 排版看 RunningAction 与 Formatting（转写途中不能排版），总结与刷新讨论各看自己的标记——与渲染层各个 hook 的早退一致。
//
// 进度推送（media:transcribeProgress、media:formatProgress）在 Load 时订阅一次、Close 时取消（subscribeMedia / Close）；
// 只有「推送属于当前选中的条目、且对应动作确实在跑」才收下，动作一结束进度就清空，晚到的推送自然被挡掉。

// 文字稿排版的两个尺寸常量（packages/shared/constants/transcript.ts）：渲染层据此在点击前判断长稿并说明代价，
// 「多长算长」「大概几次请求」必须与主进程实际的切块口径一致，改动要与 TS 同步。
const (
	transcriptFormatChunkChars = 1600
	transcriptFormatLongChars  = 50_000
)

// 转写动作的运行态取值（MediaView.RunningAction）。
const (
	MediaActionTranscribe = "transcribe"
	MediaActionDiarize    = "diarize"
)

// LongFormatPrompt 是长稿排版确认框要说的代价：文字稿的字数与大约要拆成多少块（串行请求）。
type LongFormatPrompt struct {
	Chars, Chunks int
}

// MediaView 是媒体动作的界面状态（use-media-actions.ts 各 hook 的返回值里除能力判断之外的部分）。
type MediaView struct {
	// RunningAction 是正在跑的转写动作：空、MediaActionTranscribe 或 MediaActionDiarize。
	// 两个按钮共用一个忙标记的话会一起转圈，所以要分清是哪一个。
	RunningAction string
	// Formatting 是文字稿排版在途；Summarizing 是总结（音视频或论坛讨论）在途；RefreshingDiscussion 是刷新论坛讨论在途。
	Formatting, Summarizing, RefreshingDiscussion bool
	// TranscribeProgress 是转写进行中的已用时长；FormatProgress 是排版的逐块进度。没有在跑或还没收到首个推送时为 nil。
	// 推送来一次就换一个新对象，快照里拿到的不会再变。
	TranscribeProgress *services.LibraryTranscribeProgress
	FormatProgress     *services.LibraryTranscriptFormatProgress
	// PendingLongFormat 非空表示正等用户确认长稿排版的代价（ConfirmLongFormat / CancelLongFormat）。
	PendingLongFormat *LongFormatPrompt
}

// mediaState 是媒体动作的内部状态（detailState 里的一份）。
type mediaState struct {
	// gen 是挂载代号：换条目、清详情都会让它加一（渲染层 hook 随 itemId 重置）。
	gen int
}

// resetMediaLocked 让上一代作废并清空快照（换条目、清详情）。mu 由调用方持有。
func (s *Store) resetMediaLocked() {
	s.detail.media.gen++
	s.view.Detail.Media = MediaView{}
}

// ---- 进度订阅 ----

// subscribeMedia 订阅转写与排版的进度推送；已经订阅过就什么也不做（Load 每次挂载都会调用）。
func (s *Store) subscribeMedia() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mediaStop != nil {
		return
	}
	stop := s.svc.SubscribeMediaProgress(s.onTranscribeProgress, s.onFormatProgress)
	if stop == nil {
		stop = func() {}
	}
	s.mediaStop = stop
}

// Close 取消进度订阅并停掉网页快照的补采轮询（应用退出时由根调用）；可重复调用，之后再 Load 会重新订阅。
func (s *Store) Close() {
	s.mu.Lock()
	stop := s.mediaStop
	s.mediaStop = nil
	s.stopSnapshotLocked()
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// onTranscribeProgress 收下转写进度（useTranscriptActions 的 handleProgress）：属于当前选中的条目、且转写确实在跑才收。
func (s *Store) onTranscribeProgress(p services.LibraryTranscribeProgress) {
	s.mu.Lock()
	accept := s.view.Detail.Media.RunningAction != "" && s.showsLocked(p.ItemID)
	if accept {
		s.view.Detail.Media.TranscribeProgress = &p
	}
	s.mu.Unlock()
	if accept {
		s.wake()
	}
}

// onFormatProgress 收下排版的逐块进度：属于当前选中的条目、且排版确实在跑才收。
func (s *Store) onFormatProgress(p services.LibraryTranscriptFormatProgress) {
	s.mu.Lock()
	accept := s.view.Detail.Media.Formatting && s.showsLocked(p.ItemID)
	if accept {
		s.view.Detail.Media.FormatProgress = &p
	}
	s.mu.Unlock()
	if accept {
		s.wake()
	}
}

// ---- 共用 ----

// mediaItemLocked 返回详情里此刻显示的条目（列表选中项与之一致）；没有则为 nil。mu 由调用方持有。
func (s *Store) mediaItemLocked() *services.LibraryItem {
	item := s.view.Detail.Item
	if item == nil || s.view.List.SelectedID != item.ID {
		return nil
	}
	return item
}

// mediaOutcome 判断一次媒体动作是否成功落库；成功时 written 是写回后的条目。
func mediaOutcome(res services.LibraryMediaActionResult, err error) (ok bool, written *services.LibraryItem) {
	if err == nil && res.OK() {
		return true, res.Item
	}
	return false, nil
}

// settleMedia 落定一次媒体动作：这一代仍是当前就放下忙标记（release）；写回了条目（written 非空）时列表行总是跟上，
// 仍在看这一条时再把正文基线同步成服务端版本（没有未落盘的正文改动才同步，否则之后的保存会被误判为冲突）并换上服务端版本，
// 未落盘的草稿由 mergeServerItemLocked 叠回。返回这一代是否仍是当前（为假时不弹提示）。
func (s *Store) settleMedia(gen int, written *services.LibraryItem, release func(m *MediaView)) bool {
	s.mu.Lock()
	current := gen == s.detail.media.gen
	if current {
		release(&s.view.Detail.Media)
	}
	if written != nil {
		s.applyEntryLocked(entryOf(*written))
		if current && s.showsLocked(written.ID) {
			s.syncContentBaseLocked(*written)
			merged := s.mergeServerItemLocked(*written)
			s.view.Detail.Item = &merged
		}
	}
	s.mu.Unlock()
	s.wake()
	return current
}

// reportMediaProblem 处理一次没成功的媒体动作（渲染层各 hook 的 notConfigured / 失败分支）：
// 模型没配置（notConfigured 非空且回复标了 NotConfigured）提示 notConfigured 并跳设置页的 AI 分区；
// 其余留日志（action 为日志里的动作名）并用 failed 包一层原因提示，原文放进可复制的详情——failed 返回的标题里要不要带原因由各动作自己定。
// show 为假（这一代已过期，用户离开了那个条目）时只留日志，不弹提示、不跳转。
func (s *Store) reportMediaProblem(show bool, action string, res services.LibraryMediaActionResult, err error, notConfigured string, failed func(msg string) string) {
	if err == nil && notConfigured != "" && res.NotConfigured {
		if show {
			s.toastError(notConfigured, "")
			s.openSettingsAI()
		}
		return
	}
	if err == nil {
		reason := strings.TrimSpace(res.Error)
		if reason == "" {
			reason = locales.CommonUnknownError()
		}
		err = errors.New(reason)
	}
	s.guard.Background(action, err)
	if show {
		msg := describeToolsError(err, locales.CommonUnknownError())
		s.toastError(failed(msg), msg)
	}
}

// ---- 转写 ----

// Transcribe 转写详情里的音视频条目（useTranscriptActions 的 transcribe）；diarize 为真时区分说话人。不阻塞：落盘与转写放到后台。
//
// 没有详情、转写在途时什么也不做。先把未落盘的编辑落盘（主进程基于库中正文写回，会清注记、总结与标题），落盘失败就收手
// （失败原因在详情头部）；成功后列表行跟上，仍在看这一条时详情换成服务端版本。要了分离却只识别出一个说话人时给警告而不是成功。
func (s *Store) Transcribe(ctx context.Context, diarize bool) {
	s.mu.Lock()
	item := s.mediaItemLocked()
	m := &s.view.Detail.Media
	if item == nil || m.RunningAction != "" {
		s.mu.Unlock()
		return
	}
	gen, id := s.detail.media.gen, item.ID
	m.RunningAction, m.TranscribeProgress = MediaActionTranscribe, nil
	if diarize {
		m.RunningAction = MediaActionDiarize
	}
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runTranscribe(ctx, gen, id, diarize) })
}

func releaseTranscribe(m *MediaView) { m.RunningAction, m.TranscribeProgress = "", nil }

func (s *Store) runTranscribe(ctx context.Context, gen int, id string, diarize bool) {
	if !s.FlushPendingSave(ctx) {
		s.settleMedia(gen, nil, releaseTranscribe)
		return
	}
	res, err := s.svc.TranscribeMedia(ctx, id, diarize)
	ok, written := mediaOutcome(res, err)
	current := s.settleMedia(gen, written, releaseTranscribe)

	switch {
	case !ok:
		s.reportMediaProblem(current, "library.transcribe", res, err, locales.LibraryTranscribeNotConfigured(),
			func(msg string) string { return locales.LibraryTranscribeFailed(msg) })
	case !current:
	case res.Warning != "":
		s.toastWarning(locales.LibraryTranscribeOneSpeaker(), res.Warning)
	default:
		s.toastSuccess(locales.LibraryTranscribeDone())
	}
}

// ---- 文字稿排版 ----

// FormatTranscript 对详情里已有的文字稿做 AI 排版（useTranscriptActions 的 format）：补标点、分段，不重新转写。不阻塞。
//
// 没有详情、转写或排版在途时什么也不做。文字稿（去首尾空白后，按渲染层字符串长度的 UTF-16 码元数）超过
// transcriptFormatLongChars 时先不发请求，只设 PendingLongFormat 等用户确认代价——几万字要拆成几十次串行请求，耗时数分钟且真实计费，
// 不该在用户点一下图标按钮后就默默花掉；确认走 ConfirmLongFormat，放弃走 CancelLongFormat。
func (s *Store) FormatTranscript(ctx context.Context) { s.formatFlow(ctx, false) }

// ConfirmLongFormat 确认长稿排版的代价并开始排版（useTranscriptActions 的 confirmLongFormat）。没有待确认的提示时什么也不做（防重复点击）。
func (s *Store) ConfirmLongFormat(ctx context.Context) { s.formatFlow(ctx, true) }

// CancelLongFormat 放弃长稿排版（useTranscriptActions 的 cancelLongFormat）。
func (s *Store) CancelLongFormat() {
	s.mu.Lock()
	had := s.view.Detail.Media.PendingLongFormat != nil
	s.view.Detail.Media.PendingLongFormat = nil
	s.mu.Unlock()
	if had {
		s.wake()
	}
}

// formatFlow 是 FormatTranscript 与 ConfirmLongFormat 共用的入口；confirmed 为真时是用户已确认长稿代价（请求带 allowLong）。
func (s *Store) formatFlow(ctx context.Context, confirmed bool) {
	s.mu.Lock()
	item := s.mediaItemLocked()
	m := &s.view.Detail.Media
	if item == nil || m.RunningAction != "" || m.Formatting || (confirmed && m.PendingLongFormat == nil) {
		s.mu.Unlock()
		return
	}
	if !confirmed {
		if chars, long := utf16Exceeds(jsTrim(item.Transcript), transcriptFormatLongChars); long {
			m.PendingLongFormat = &LongFormatPrompt{Chars: chars, Chunks: (chars + transcriptFormatChunkChars - 1) / transcriptFormatChunkChars}
			s.mu.Unlock()
			s.wake()
			return
		}
	}
	gen, id := s.detail.media.gen, item.ID
	m.PendingLongFormat = nil
	m.Formatting, m.FormatProgress = true, nil
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runFormat(ctx, gen, id, confirmed) })
}

func releaseFormat(m *MediaView) { m.Formatting, m.FormatProgress = false, nil }

func (s *Store) runFormat(ctx context.Context, gen int, id string, allowLong bool) {
	res, err := s.svc.FormatTranscript(ctx, id, allowLong)
	ok, written := mediaOutcome(res, err)
	current := s.settleMedia(gen, written, releaseFormat)

	switch {
	case !ok:
		s.reportMediaProblem(current, "library.formatTranscript", res, err, locales.LibraryTranscriptFormatNotConfigured(),
			func(string) string { return locales.LibraryTranscriptFormatFailed() })
	case !current:
	case res.Warning != "": // 只排了一部分也算成功（结果已落库），但不能弹绿色的「完成」
		s.toastWarning(locales.LibraryTranscriptFormatPartial(), res.Warning)
	default:
		s.toastSuccess(locales.LibraryTranscriptFormatDone())
	}
}

// ---- 总结 ----

// SummarizeMedia 基于文字稿（音视频）或逐楼回复（论坛）生成结构化总结，写入正文的总结小节（useMediaSummaryAction 的 summarize）。不阻塞。
//
// 没有详情、总结在途时什么也不做；先把未落盘的编辑落盘（主进程基于库中正文生成并写回），落盘失败就收手。
func (s *Store) SummarizeMedia(ctx context.Context) {
	s.mu.Lock()
	item := s.mediaItemLocked()
	m := &s.view.Detail.Media
	if item == nil || m.Summarizing {
		s.mu.Unlock()
		return
	}
	gen, id := s.detail.media.gen, item.ID
	m.Summarizing = true
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runSummarize(ctx, gen, id) })
}

func releaseSummarize(m *MediaView) { m.Summarizing = false }

func (s *Store) runSummarize(ctx context.Context, gen int, id string) {
	if !s.FlushPendingSave(ctx) {
		s.settleMedia(gen, nil, releaseSummarize)
		return
	}
	res, err := s.svc.SummarizeMedia(ctx, id)
	ok, written := mediaOutcome(res, err)
	current := s.settleMedia(gen, written, releaseSummarize)

	switch {
	case !ok:
		s.reportMediaProblem(current, "library.summarizeMedia", res, err, locales.LibraryMediaSummaryNotConfigured(),
			func(msg string) string { return locales.LibraryMediaSummaryFailed(msg) })
	case current:
		s.toastSuccess(locales.LibraryMediaSummaryDone())
	}
}

// ---- 刷新论坛讨论 ----

// RefreshForumDiscussion 重新抓取支持站点（Linux.do、小众软件论坛、2libra）的论坛讨论，替换正文里的讨论小节
// （useForumDiscussionRefreshAction 的 refresh）。不阻塞。
//
// 只对论坛条目且来源是支持的站点生效；没有详情、刷新在途时什么也不做；先把未落盘的编辑落盘，落盘失败就收手。
// 发起时记下条目原来有没有讨论总结：刷新会让它过期，成功提示据此说明「原讨论总结已标记为过期」。
func (s *Store) RefreshForumDiscussion(ctx context.Context) {
	s.mu.Lock()
	item := s.mediaItemLocked()
	m := &s.view.Detail.Media
	if item == nil || m.RefreshingDiscussion || !canRefreshForum(*item) {
		s.mu.Unlock()
		return
	}
	hadSummary := jsTrim(notes.SplitForumSections(item.Content).Summary) != ""
	gen, id := s.detail.media.gen, item.ID
	m.RefreshingDiscussion = true
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.runRefreshDiscussion(ctx, gen, id, hadSummary) })
}

func releaseRefreshDiscussion(m *MediaView) { m.RefreshingDiscussion = false }

func (s *Store) runRefreshDiscussion(ctx context.Context, gen int, id string, hadSummary bool) {
	if !s.FlushPendingSave(ctx) {
		s.settleMedia(gen, nil, releaseRefreshDiscussion)
		return
	}
	res, err := s.svc.RefreshForumDiscussion(ctx, id)
	ok, written := mediaOutcome(res, err)
	current := s.settleMedia(gen, written, releaseRefreshDiscussion)

	switch {
	case !ok: // 刷新讨论没有「未配置模型」这一支：一律按失败处理
		s.reportMediaProblem(current, "library.refreshForumDiscussion", res, err, "",
			func(msg string) string { return locales.LibraryForumDiscussionRefreshFailed(msg) })
	case !current:
	case hadSummary:
		s.toastSuccess(locales.LibraryForumDiscussionRefreshedSummaryStale())
	default:
		s.toastSuccess(locales.LibraryForumDiscussionRefreshed())
	}
}
