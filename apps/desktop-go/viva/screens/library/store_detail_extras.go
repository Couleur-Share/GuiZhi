package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 详情头部与面板用的几块附属状态（渲染层 ItemDetailHeader 的「已保存」提示、SourceCaptureRevisions 的来源版本、
// ReviewRequiredNotice 的复核确认、use-media-actions 的 canDiarize）。它们都随条目切换而重置，由 loadDetail 读到条目之后各自读取。

// justSavedWindow 是「已保存」提示的停留时长（渲染层 setTimeout 2500）。
const justSavedWindow = 2500 * time.Millisecond

// RevisionsView 是「更新的来源版本」背后的状态（渲染层 SourceCaptureRevisions 的 rows 与 error）。
type RevisionsView struct {
	// ItemID 是这份数据属于哪一条；详情切走之后旧数据不再显示。
	ItemID string
	// Rows 是该条目的来源版本（新的在前）；Error 是读取失败的原因（界面显示重试）。
	Rows  []services.LibrarySourceVersion
	Error string
}

// 采用来源版本、标记复核时写死的失败原因（渲染层的中文；errors.New 的参数免检）。
var (
	errAdoptUnsaved = errors.New("请先保存当前草稿")
	errAdoptSwitch  = errors.New("当前资料已切换")
	errAdoptGone    = errors.New("条目已删除")
	errAdoptEdited  = errors.New("编辑内容已变化，来源版本仍保留，请重新核对后采用")
	errReviewGone   = errors.New("条目已不存在")
)

// AutoSaveEnabled 报告自动保存是否开着（设置里的开关；没接线按开着处理）。头部据此判断「未保存」要不要显示：
// 自动保存开着时有未落盘的编辑只是还没到防抖点，不提示（渲染层 isDirty）。
func (s *Store) AutoSaveEnabled() bool { return s.AutoSave == nil || s.AutoSave() }

// ---- 已保存提示 ----
//
// 渲染层 ItemDetailHeader 用一个 effect 判断「保存刚结束」：依赖是 (isSaving, saveError, hasUnsavedChanges)，
// 任何一个变化都先执行上一轮 effect 的清理（收起 2.5 秒定时器），再看「上一次观察到在保存中、现在没在保存、没失败、没待保存」，
// 满足就亮「已保存」并重新计时。justSaved 本身只会被定时器到点或换条目熄灭——所以依赖变化只收起定时器、不熄灭它：
// 提示亮着时又输入了，它会一直亮到下一次保存结束再过 2.5 秒（头部另外要求没有未落盘的编辑才画出来）。这里照原样复刻。

// savedTrack 是 effect 观察到的依赖值（wasSavingRef 即其中的 saving）。零值就是头部刚挂载、还没有观察过
// （渲染层 wasSavingRef 的初值是 false）：什么都不会被判成「保存刚结束」。
type savedTrack struct {
	saving    bool
	unsaved   bool
	saveError string
}

// finishedTo 是 effect 里的条件：上一次观察到在保存中，现在没在保存、没失败、没有待保存。
func (t savedTrack) finishedTo(cur savedTrack) bool {
	return t.saving && !cur.saving && cur.saveError == "" && !cur.unsaved
}

// syncSavedLocked 在 Saving、SaveError、Unsaved 任何一个可能变化的临界区末尾调用：值没变什么也不做；变了就按 effect 的规则处理。
// 没有详情（头部没挂载）时 effect 不运行。mu 由调用方持有。
func (s *Store) syncSavedLocked() {
	d := &s.view.Detail
	if d.Item == nil {
		return
	}
	cur := savedTrack{saving: d.Saving, unsaved: d.Unsaved, saveError: d.SaveError}
	prev := s.detail.saved
	if prev == cur {
		return
	}
	s.stopSavedTimerLocked()
	s.detail.saved = cur
	if prev.finishedTo(cur) {
		s.armJustSavedLocked()
	}
}

// armJustSavedLocked 亮起「已保存」并排上 justSavedWindow 之后熄灭的定时器。mu 由调用方持有。
func (s *Store) armJustSavedLocked() {
	s.view.Detail.JustSaved = true
	s.detail.savedGen++
	gen := s.detail.savedGen
	s.detail.savedStop = s.After(justSavedWindow, func() {
		s.mu.Lock()
		if gen != s.detail.savedGen { // 已被收起或换了条目：旧定时器作废
			s.mu.Unlock()
			return
		}
		s.view.Detail.JustSaved = false
		s.detail.savedStop = nil
		s.mu.Unlock()
		s.wake()
	})
}

// stopSavedTimerLocked 收起还没到点的熄灭定时器（不改变是否亮着）。mu 由调用方持有。
func (s *Store) stopSavedTimerLocked() {
	s.detail.savedGen++
	if s.detail.savedStop != nil {
		s.detail.savedStop()
		s.detail.savedStop = nil
	}
}

// resetSavedLocked 熄灭「已保存」并丢掉观察记录（头部卸载：换条目、取消选中、筛选挤掉选中项）。mu 由调用方持有。
func (s *Store) resetSavedLocked() {
	s.stopSavedTimerLocked()
	s.view.Detail.JustSaved = false
	s.detail.saved = savedTrack{}
}

// ---- 读到条目之后的附属读取 ----

// afterDetailLoaded 在读到条目之后读它的附属状态：来源版本与转写能力（渲染层这两处各在组件挂载时读一次）。
// 序号变了（又选了别的）就丢弃。
func (s *Store) afterDetailLoaded(ctx context.Context, id string, seq int) {
	s.loadRevisions(ctx, id, seq)
	s.loadMediaCapabilities(ctx, seq)
}

// loadRevisions 读来源版本（渲染层 SourceCaptureRevisions 的 effect）：读到之前是空的，失败记下原因等重试。
func (s *Store) loadRevisions(ctx context.Context, id string, seq int) {
	versions, err := s.svc.SourceVersions(ctx, id)
	s.mu.Lock()
	current := seq == s.detail.seq
	if current {
		view := RevisionsView{ItemID: id}
		if err != nil {
			view.Error = describeRevisionsError(err)
		} else {
			view.Rows = versions
		}
		s.view.Detail.Revisions = view
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("library.sourceVersions", fmt.Errorf("读取来源版本失败：%w", err))
	}
	if current {
		s.wake()
	}
}

// describeRevisionsError 取来源版本读取失败的原因：空的换成「来源版本读取失败」（渲染层 `result.error || translate(...)`）。
func describeRevisionsError(err error) string {
	if msg := err.Error(); msg != "" {
		return msg
	}
	return locales.LibrarySourceRevisionsLoadFailed()
}

// RetryRevisions 重新读当前条目的来源版本（重试按钮）。没有详情时什么也不做。
func (s *Store) RetryRevisions(ctx context.Context) {
	s.mu.Lock()
	cur, seq := s.view.Detail.Item, s.detail.seq
	if cur == nil { // 详情显示着的条目就是选中的那条（commitSelectLocked 同时落定两者），所以只需看有没有详情
		s.mu.Unlock()
		return
	}
	id := cur.ID
	s.view.Detail.Revisions = RevisionsView{ItemID: id}
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.loadRevisions(ctx, id, seq) })
}

// loadMediaCapabilities 读转写能力（渲染层 useTranscriptActions 在面板挂载时读一次）：只有内置本地引擎支持区分说话人，
// 读取失败当作不支持——不摆一个点了必然报错的入口。
func (s *Store) loadMediaCapabilities(ctx context.Context, seq int) {
	caps, err := s.svc.MediaCapabilities(ctx)
	s.mu.Lock()
	current := seq == s.detail.seq
	if current {
		s.view.Detail.CanDiarize = err == nil && caps.Diarization
	}
	s.mu.Unlock()
	if current {
		s.wake()
	}
}

// ---- 采用来源版本 ----

// AdoptSourceVersion 把一个来源版本的正文采用进草稿（渲染层 SourceCaptureRevisions 的 adopt）。
//
// 先把当前草稿落盘（落不了就不采用）、确认还是这一条；残缺的新版本（带缺口原因）先把条目标成待复核再进草稿——
// 完整版本不代替用户确认旧复核；标记之后正文又被改过就停下，来源版本仍保留。采用的正文是版本的正文，没有则取文字稿。
// 失败走变更失败通道；返回是否成功。
func (s *Store) AdoptSourceVersion(ctx context.Context, itemID string, v services.LibrarySourceVersion) bool {
	return s.guard.Run(locales.LibraryActionAdoptSource(), func() error {
		if !s.isSelectedItem(itemID) || !s.FlushPendingSave(ctx) {
			return errAdoptUnsaved
		}
		before := s.View().Detail.Item
		if before == nil || !s.isSelectedItem(itemID) {
			return errAdoptSwitch
		}
		var reasons []string
		if err := json.Unmarshal([]byte(v.ReviewJSON), &reasons); err != nil {
			return err
		}
		if len(reasons) > 0 {
			updated, found, err := s.svc.UpdateEntry(ctx, itemID, services.LibraryEntryPatch{
				ReviewStatus:  services.StringPtr(services.LibraryReviewNeedsReview),
				ReviewReasons: reasons,
			})
			if err != nil {
				return err
			}
			if !found {
				return errAdoptGone
			}
			s.ApplyServerEntry(updated)
			latest := s.View().Detail.Item
			if !s.isSelectedItem(itemID) || latest == nil || latest.Content != before.Content {
				return errAdoptEdited
			}
		}
		content := v.Content
		if content == "" {
			content = v.Transcript
		}
		s.UpdateSelected(ctx, services.LibraryDraftFields{Content: &content})
		return nil
	})
}

// isSelectedItem 报告详情正选中并显示着这一条（渲染层 selectedId 与 selectedItem.id 都等于它）。
// 渲染层两头都核对；这里换选中项和清详情在同一个临界区（commitSelectLocked，渲染层 selectItem 的 set 也是同一次），
// 详情在就必然是选中的那条，所以只需核对选中项与有没有详情。
func (s *Store) isSelectedItem(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view.List.SelectedID == id && s.view.Detail.Item != nil
}

// ---- 标记已复核 ----

// MarkReviewed 确认采集内容已核对（渲染层 ReviewRequiredNotice 的 confirmReviewed）：只清掉复核标记，不改正文。
// 成功给「已标记为复核完成」；失败的提示标题是「更新复核状态失败」、原因折叠在详情里。返回是否成功。
// 进行中（MarkingReviewedID 非空）再点直接返回 false。
func (s *Store) MarkReviewed(ctx context.Context, itemID string) bool {
	s.mu.Lock()
	if s.view.Detail.MarkingReviewedID != "" {
		s.mu.Unlock()
		return false
	}
	s.view.Detail.MarkingReviewedID = itemID
	s.mu.Unlock()
	s.wake()
	defer func() {
		s.mu.Lock()
		s.view.Detail.MarkingReviewedID = ""
		s.mu.Unlock()
		s.wake()
	}()

	updated, found, err := s.svc.UpdateEntry(ctx, itemID, services.LibraryEntryPatch{
		ReviewStatus:  services.StringPtr(services.LibraryReviewClear),
		ReviewReasons: []string{},
	})
	if err == nil && !found {
		err = errReviewGone
	}
	if err != nil {
		s.guard.Background("library.markReviewed", fmt.Errorf("更新复核状态失败：%w", err))
		if s.guard.Notify != nil {
			s.guard.Notify.Error(locales.LibraryReviewClearFailed(), strings.TrimSpace(err.Error()))
		}
		return false
	}
	s.ApplyServerEntry(updated)
	if s.guard.Notify != nil {
		s.guard.Notify.Success(locales.LibraryReviewCleared())
	}
	return true
}
