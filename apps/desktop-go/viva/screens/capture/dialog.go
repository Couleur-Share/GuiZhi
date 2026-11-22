// Package capture 是「快速采集」对话框（渲染层 components/capture/CaptureDialog.tsx）：
// 粘贴文本、链接或分享口令自动识别；选择或拖入文本、图片与音视频文件；可指定目标合集与标签；也提供「空白笔记」直达入口。
//
// 草稿的解析规则在 draft.go（渲染层 capture-input.ts 的 Go 对应实现）；这里是界面与提交流程。
// 用户操作的失败都有可见的原因（kit.Guard），失败时对话框留着，草稿不丢，可以直接重试。
package capture

import (
	"context"
	"strings"
	"sync"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Hooks 是对话框需要外壳代办的事。
type Hooks struct {
	// OnEnqueued 在任务入队成功之后、成功提示之前调用（渲染层 import.store 的 enqueue 在返回前重新读取队列）；
	// 在后台 goroutine 里被调用，可为空。
	OnEnqueued func()
	// BlankNote 是「空白笔记」：对话框已经关上，外壳切到知识库并新建一条空白笔记（collectionID 为空表示不归合集）；
	// 在界面线程被调用。为空表示没有这个入口，按钮不显示。
	BlankNote func(collectionID string)
}

// Dialog 是快速采集对话框的界面与状态。Look、Ctx、Guard、Imports 必填；Library 为空时没有合集与标签可选。
// 每一帧都要调用 View（Modal 要在不打开的那一帧复位）。
type Dialog struct {
	Look    *kit.Look
	Ctx     context.Context
	Guard   kit.Guard
	Imports services.Imports
	Library services.Library
	Hooks   Hooks

	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// Wake 请求重绘（后台结果落定之后调用）；可为空。
	Wake func()

	// mu 保护后台也会改的状态：开关与代数、提交在途标记、已选文件、合集与标签的缓存。
	// 切片都是整体替换、从不原地修改，所以快照拿到之后不会被改。
	mu          sync.Mutex
	open        bool
	epoch       int // 每次打开与关闭加一：晚到的后台结果据此判断自己是不是属于当前这一次打开
	busy        bool
	files       []string
	collections []services.LibraryCollection
	tags        []services.LibraryTag

	// 以下只在界面线程上读写。
	collectionID string
	tagNames     []string
	override     ActionKind // 文字夹链接时用户在提示栏上的改判；ActionEmpty 表示沿用自动判定
	dragOver     bool
	draft        ui.Field
	tagDraft     ui.Field
	modal        kit.ModalState
	pick         kit.SelectState
	zone         gfx.Rect // 拖放区上一帧的外框（系统文件拖放据此判断落点）
	tagRow       gfx.Rect // 标签行上一帧的外框（已有标签的浮层挂在它下面）

	// 草稿解析的缓存：文字没变就不重新解析（粘贴上千个链接时每帧都解析太贵）。
	parsedFor string
	parsed    Draft
	parsedOK  bool
}

// state 是一帧开头取的快照。
type state struct {
	open, busy  bool
	files       []string
	collections []services.LibraryCollection
	tags        []services.LibraryTag
}

func (d *Dialog) snapshot() state {
	d.mu.Lock()
	defer d.mu.Unlock()
	return state{open: d.open, busy: d.busy, files: d.files, collections: d.collections, tags: d.tags}
}

// IsOpen 报告对话框此刻是否打开。
func (d *Dialog) IsOpen() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.open
}

func (d *Dialog) spawn(fn func()) {
	if d.Spawn != nil {
		d.Spawn(fn)
		return
	}
	go fn()
}

func (d *Dialog) wake() {
	if d.Wake != nil {
		d.Wake()
	}
}

// Open 打开对话框，草稿、文件、标签、合集与改判都是空白的（渲染层每次打开都重新挂载组件），并在后台重新读取合集与标签。
// 已经打开时什么也不做：再按一次快捷键不能清掉用户正在写的内容。界面线程调用。
func (d *Dialog) Open() {
	d.mu.Lock()
	if d.open {
		d.mu.Unlock()
		return
	}
	d.open, d.busy, d.files = true, false, nil
	d.epoch++
	epoch := d.epoch
	d.mu.Unlock()

	d.draft.SetText("")
	d.tagDraft.SetText("")
	d.collectionID, d.tagNames, d.override, d.dragOver = "", nil, ActionEmpty, false
	d.pick = kit.SelectState{}
	d.parsedOK = false
	d.refresh(epoch)
}

// Close 关闭对话框；在途的后台结果随之作废（入队已经发出的话，结果照常提示，只是不再动对话框）。
func (d *Dialog) Close() {
	d.mu.Lock()
	d.open = false
	d.epoch++
	d.mu.Unlock()
	d.dragOver = false
	d.wake()
}

// refresh 在后台读取合集与标签。失败只进日志：它们只是可选项，读不到时仍可以直接采集。
func (d *Dialog) refresh(epoch int) {
	if d.Library == nil {
		return
	}
	d.spawn(func() {
		cols, err := d.Library.Collections(d.Ctx)
		if err != nil {
			d.Guard.Background("capture.collections", err)
			return
		}
		d.mu.Lock()
		if d.epoch == epoch {
			d.collections = cols
		}
		d.mu.Unlock()
		d.wake()
	})
	d.spawn(func() {
		tags, err := d.Library.Tags(d.Ctx)
		if err != nil {
			d.Guard.Background("capture.tags", err)
			return
		}
		d.mu.Lock()
		if d.epoch == epoch {
			d.tags = tags
		}
		d.mu.Unlock()
		d.wake()
	})
}

// action 返回此刻草稿解析的结果与提交时真正要做的事（带缓存）。界面线程调用。
func (d *Dialog) action() (Draft, Action) {
	text := d.draft.Text()
	if !d.parsedOK || d.parsedFor != text {
		d.parsed, d.parsedFor, d.parsedOK = ParseDraft(text), text, true
	}
	return d.parsed, ResolveAction(d.parsed, d.override)
}

// buildInputs 把动作与文件组成入队的各项（渲染层 submit）：链接逐条、文字一条、文件逐个；都带目标合集与标签。
func buildInputs(action Action, files []string, collectionID string, tagNames []string) []services.EnqueueImportInput {
	var target *string
	if collectionID != "" {
		id := collectionID
		target = &id
	}
	var tags []string
	if len(tagNames) > 0 {
		tags = append([]string(nil), tagNames...)
	}
	var inputs []services.EnqueueImportInput
	add := func(kind, input string) {
		inputs = append(inputs, services.EnqueueImportInput{Kind: kind, Input: input, CollectionID: target, TagNames: tags})
	}
	switch action.Kind {
	case ActionURLs:
		for _, url := range action.URLs {
			add("url", url)
		}
	case ActionText:
		add("text", action.Text)
	}
	for _, path := range files {
		add("file", path)
	}
	return inputs
}

// submit 把草稿与文件加入采集队列：成功提示并关闭对话框；失败提示原因（带可复制的详情）、对话框与草稿原样留着。
// 入队在后台进行；在途时再点「采集」不重复提交。界面线程调用。
func (d *Dialog) submit(action Action, st state) {
	inputs := buildInputs(action, st.files, d.collectionID, d.tagNames)
	if len(inputs) == 0 {
		return
	}
	d.mu.Lock()
	if d.busy {
		d.mu.Unlock()
		return
	}
	d.busy = true
	epoch := d.epoch
	d.mu.Unlock()

	d.spawn(func() {
		_, err := d.Imports.Enqueue(d.Ctx, inputs)
		if err != nil {
			d.mu.Lock()
			if d.epoch == epoch {
				d.busy = false
			}
			d.mu.Unlock()
			d.Guard.Fail(locales.CaptureEnqueueFailed(err.Error()), "capture.enqueue", err)
			d.wake()
			return
		}
		if d.Hooks.OnEnqueued != nil {
			d.Hooks.OnEnqueued()
		}
		d.Guard.Succeed(locales.CaptureEnqueued(len(inputs)))
		d.mu.Lock()
		if d.epoch == epoch {
			d.open, d.busy = false, false
			d.epoch++
		}
		d.mu.Unlock()
		d.wake()
	})
}

// PickFiles 弹出系统的文件选择框，选中的路径追加到文件列表（已有的不重复，渲染层 pickFiles）。
// 选择框阻塞，所以放在后台；选择期间对话框若已关闭或重新打开，结果丢弃。
func (d *Dialog) PickFiles() {
	d.mu.Lock()
	epoch := d.epoch
	d.mu.Unlock()
	d.spawn(func() {
		paths, err := d.Imports.SelectFiles(d.Ctx)
		if err != nil {
			d.Guard.Report(locales.CaptureChooseFiles(), err)
			d.wake()
			return
		}
		d.addFiles(epoch, paths)
	})
}

// addFiles 把路径追加到文件列表，按路径去重；epoch 不是当前这次打开就丢弃。
func (d *Dialog) addFiles(epoch int, paths []string) {
	d.mu.Lock()
	if d.epoch == epoch && len(paths) > 0 {
		merged := append([]string(nil), d.files...)
		for _, path := range paths {
			if path != "" && !containsString(merged, path) {
				merged = append(merged, path)
			}
		}
		d.files = merged
	}
	d.mu.Unlock()
	d.wake()
}

// removeFile 把路径从文件列表里去掉。
func (d *Dialog) removeFile(path string) {
	d.mu.Lock()
	kept := make([]string, 0, len(d.files))
	for _, p := range d.files {
		if p != path {
			kept = append(kept, p)
		}
	}
	d.files = kept
	d.mu.Unlock()
}

// ImportFiles 是顶栏「采集文件」：选文件后直接加入采集队列（不指定合集与标签），提示「已加入采集队列」（渲染层 NewItemButton 的 importFiles）。
// 与对话框的状态无关，对话框开不开都能用。失败提示原因。
func (d *Dialog) ImportFiles() {
	d.spawn(func() {
		paths, err := d.Imports.SelectFiles(d.Ctx)
		if err != nil {
			d.Guard.Report(locales.HeaderNewImportFiles(), err)
			return
		}
		if len(paths) == 0 {
			return
		}
		inputs := buildInputs(Action{}, paths, "", nil)
		if _, err := d.Imports.Enqueue(d.Ctx, inputs); err != nil {
			d.Guard.Fail(locales.CaptureEnqueueFailed(err.Error()), "capture.enqueue", err)
			return
		}
		if d.Hooks.OnEnqueued != nil {
			d.Hooks.OnEnqueued()
		}
		d.Guard.Succeed(locales.CaptureEnqueued(len(paths)))
	})
}

// FileDrop 接系统拖来的文件（Config.OnFileDrop）：只在对话框打开、落点在草稿拖放区里时接收；悬停时拖放区高亮，
// 落下时路径追加到文件列表（目录不筛，与渲染层一致）。窗口线程调用。
func (d *Dialog) FileDrop(e gfx.FileDropEvent) gfx.EventResult {
	if !d.IsOpen() {
		return gfx.EventResult{}
	}
	inside := e.X >= d.zone.X && e.X < d.zone.X+d.zone.Width && e.Y >= d.zone.Y && e.Y < d.zone.Y+d.zone.Height
	switch e.Phase {
	case gfx.FileDropOver:
		changed := d.dragOver != inside
		d.dragOver = inside
		return gfx.EventResult{Handled: inside, Repaint: changed}
	case gfx.FileDropLeave:
		changed := d.dragOver
		d.dragOver = false
		return gfx.EventResult{Repaint: changed}
	case gfx.FileDropDrop:
		d.dragOver = false
		if !inside {
			return gfx.EventResult{Repaint: true}
		}
		d.mu.Lock()
		epoch := d.epoch
		d.mu.Unlock()
		d.addFiles(epoch, e.Paths)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

// addTag 追加一个标签（渲染层 addTag）：去掉首尾空白；空的、与已有的（不分大小写）重名的忽略；无论是否追加都清空输入。
func (d *Dialog) addTag(name string) {
	name = jsTrim(name)
	d.tagDraft.SetText("")
	if name == "" || d.hasTag(name) {
		return
	}
	d.tagNames = append(append([]string(nil), d.tagNames...), name)
}

func (d *Dialog) hasTag(name string) bool {
	lower := strings.ToLower(name)
	for _, existing := range d.tagNames {
		if strings.ToLower(existing) == lower {
			return true
		}
	}
	return false
}

func (d *Dialog) removeTag(name string) {
	kept := make([]string, 0, len(d.tagNames))
	for _, n := range d.tagNames {
		if n != name {
			kept = append(kept, n)
		}
	}
	d.tagNames = kept
}

// matchingTags 是输入框下面的已有标签建议（渲染层 matchingTags）：输入非空时，名称包含输入（不分大小写）且还没加上的标签，至多 6 个。
func (d *Dialog) matchingTags(all []services.LibraryTag) []services.LibraryTag {
	query := strings.ToLower(jsTrim(d.tagDraft.Text()))
	if query == "" {
		return nil
	}
	var out []services.LibraryTag
	for _, tag := range all {
		if strings.Contains(strings.ToLower(tag.Name), query) && !d.hasTag(tag.Name) {
			out = append(out, tag)
			if len(out) == maxSuggestions {
				break
			}
		}
	}
	return out
}

// maxSuggestions 是已有标签建议的上限（渲染层 slice(0, 6)）。
const maxSuggestions = 6

func containsString(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}
