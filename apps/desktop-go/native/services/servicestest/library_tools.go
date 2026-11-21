package servicestest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	"guizhi-mygo-shell/native/services"
)

// 文章工具侧栏的假实现：来源评论（读取与重新采集）与网页来源版本（读取与采用）。规则取自真实处理器：
//   - 重新采集只接受 10、20、50 三档，没有平台来源的条目失败；采到的评论并入已落库的（按外部 ID 去重，新的覆盖旧的），
//     返回并入之后的全部评论，按点赞数降序（点赞相同时新采的在前）；
//   - 采用要求版本存在且完整、比较基线（正文哈希与标题）与此刻一致；成功后正文换成该版本的 markdown，标题保持此刻的标题
//     （真实处理器只在标题仍是采集时的标题才跟着版本走，界面不依赖这一点），基线随之更新。

// 与真实处理器同文的失败原因（经 errors.New 构造：诊断文本，不是界面文案）。
var (
	errCommentLimit    = errors.New("无效的评论数量")
	errCommentNoSource = errors.New("该条目没有可补采评论的平台来源")
	errAdoptMissing    = errors.New("条目或完整版本不存在")
	errAdoptChanged    = errors.New("正文或标题已在比较期间发生变化，请重新比较后采用")
	errWebItemMissing  = errors.New("条目不存在")
)

// RefreshCall 是 RefreshSourceComments 收到的一次请求。
type RefreshCall struct {
	ItemID string
	Limit  int
}

// toolsState 是文章工具侧栏的内部状态；零值可用。
type toolsState struct {
	comments map[string][]services.LibrarySourceComment
	// captures 是下一次重新采集会采到的评论（按条目）；没有登记的条目视为没有平台来源。
	captures  map[string][]services.LibrarySourceComment
	refreshes []RefreshCall
	web       map[string]services.LibraryWebVersions
	adopts    []services.LibraryAdoptWebVersionInput
	// 各读写入口的闸门：让下一次调用卡住（HoldSourceComments 等）。
	commentsGate, refreshGate, webGate, adoptGate chan struct{}
}

// SeedSourceComments 设定一条已落库的来源评论（SourceComments 返回它，不再另行排序）。
func (f *Library) SeedSourceComments(itemID string, comments []services.LibrarySourceComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tools.comments == nil {
		f.tools.comments = map[string][]services.LibrarySourceComment{}
	}
	f.tools.comments[itemID] = slices.Clone(comments)
}

// SeedCapture 设定下一次重新采集会采到的评论；登记了才算这条有平台来源。
func (f *Library) SeedCapture(itemID string, comments []services.LibrarySourceComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tools.captures == nil {
		f.tools.captures = map[string][]services.LibrarySourceComment{}
	}
	f.tools.captures[itemID] = slices.Clone(comments)
}

// SeedWebVersions 设定一条网页条目的原文版本与比较基线。
func (f *Library) SeedWebVersions(itemID string, versions services.LibraryWebVersions) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tools.web == nil {
		f.tools.web = map[string]services.LibraryWebVersions{}
	}
	versions.Versions = slices.Clone(versions.Versions)
	f.tools.web[itemID] = versions
}

// hold 登记一个让下一次调用卡住的闸门，返回放行函数（只卡一次）。
func (f *Library) hold(slot *chan struct{}) (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	*slot = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldSourceComments 让下一次 SourceComments 卡住（在读取之前）。
func (f *Library) HoldSourceComments() (release func()) { return f.hold(&f.tools.commentsGate) }

// HoldRefreshComments 让下一次 RefreshSourceComments 卡住（在采集之前）。
func (f *Library) HoldRefreshComments() (release func()) { return f.hold(&f.tools.refreshGate) }

// HoldWebVersions 让下一次 WebVersions 卡住（在读取之前）。
func (f *Library) HoldWebVersions() (release func()) { return f.hold(&f.tools.webGate) }

// HoldAdopt 让下一次 AdoptWebVersion 卡住（在写入之前）。
func (f *Library) HoldAdopt() (release func()) { return f.hold(&f.tools.adoptGate) }

// takeGate 取走并清掉一个闸门（调用方持锁时才安全）。
func takeGate(slot *chan struct{}) chan struct{} {
	gate := *slot
	*slot = nil
	return gate
}

// RefreshCalls 返回 RefreshSourceComments 收到的请求，按发生顺序（含被 FailOn 拦下的）。
func (f *Library) RefreshCalls() []RefreshCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tools.refreshes)
}

// Adopts 返回 AdoptWebVersion 收到的请求，按发生顺序（含被拒绝的）。
func (f *Library) Adopts() []services.LibraryAdoptWebVersionInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tools.adopts)
}

// SourceComments 实现 services.Library。
func (f *Library) SourceComments(ctx context.Context, itemID string) ([]services.LibrarySourceComment, error) {
	if err := f.begin("SourceComments:"+itemID, "SourceComments"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	gate := takeGate(&f.tools.commentsGate)
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tools.comments[itemID]), nil
}

// RefreshSourceComments 实现 services.Library。
func (f *Library) RefreshSourceComments(ctx context.Context, itemID string, limit int) ([]services.LibrarySourceComment, error) {
	err := f.begin("RefreshSourceComments:"+itemID, "RefreshSourceComments")
	f.mu.Lock()
	f.tools.refreshes = append(f.tools.refreshes, RefreshCall{ItemID: itemID, Limit: limit})
	var gate chan struct{}
	if err == nil {
		gate = takeGate(&f.tools.refreshGate)
	}
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if gerr := waitGate(ctx, gate); gerr != nil {
		return nil, gerr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(services.LibraryCommentLimits, limit) {
		return nil, errCommentLimit
	}
	captured, ok := f.tools.captures[itemID]
	if !ok {
		return nil, errCommentNoSource
	}
	merged := slices.Clone(captured)
	for _, old := range f.tools.comments[itemID] {
		if !slices.ContainsFunc(captured, func(c services.LibrarySourceComment) bool { return c.ExternalID == old.ExternalID }) {
			merged = append(merged, old)
		}
	}
	slices.SortStableFunc(merged, func(a, b services.LibrarySourceComment) int { return int(b.LikeCount - a.LikeCount) })
	if f.tools.comments == nil {
		f.tools.comments = map[string][]services.LibrarySourceComment{}
	}
	f.tools.comments[itemID] = merged
	return slices.Clone(merged), nil
}

// WebVersions 实现 services.Library。
func (f *Library) WebVersions(ctx context.Context, itemID string) (services.LibraryWebVersions, error) {
	if err := f.begin("WebVersions:"+itemID, "WebVersions"); err != nil {
		return services.LibraryWebVersions{}, err
	}
	f.mu.Lock()
	gate := takeGate(&f.tools.webGate)
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return services.LibraryWebVersions{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out, ok := f.tools.web[itemID]
	if !ok {
		return services.LibraryWebVersions{}, errWebItemMissing
	}
	out.Versions = slices.Clone(out.Versions)
	return out, nil
}

// contentHash 是假实现里正文的哈希（真实处理器用另一套归一化，界面只比较相等）。
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// AdoptWebVersion 实现 services.Library。
func (f *Library) AdoptWebVersion(ctx context.Context, in services.LibraryAdoptWebVersionInput) error {
	err := f.begin("AdoptWebVersion:"+in.ItemID, "AdoptWebVersion")
	f.mu.Lock()
	f.tools.adopts = append(f.tools.adopts, in)
	var gate chan struct{}
	if err == nil {
		gate = takeGate(&f.tools.adoptGate)
	}
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if gerr := waitGate(ctx, gate); gerr != nil {
		return gerr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.tools.web[in.ItemID]
	if !ok {
		return errAdoptMissing
	}
	i := slices.IndexFunc(state.Versions, func(v services.LibraryWebVersion) bool { return v.ID == in.VersionID })
	if i < 0 || !state.Versions[i].Complete {
		return errAdoptMissing
	}
	if state.ContentHash != in.ExpectedContentHash || state.Title != in.ExpectedTitle {
		return errAdoptChanged
	}
	state.Content = state.Versions[i].Markdown
	state.ContentHash = contentHash(state.Content)
	state.PendingVersion = false
	f.tools.web[in.ItemID] = state
	if f.detail.bodies == nil {
		f.detail.bodies = map[string]DetailBody{}
	}
	body := f.detail.bodies[in.ItemID]
	body.Content = state.Content
	f.detail.bodies[in.ItemID] = body
	return nil
}
