package capture

import (
	"errors"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 本文件补 M3-3 精选变异检查里首轮存活的行为：后台结果的 epoch 守卫（晚到的回调不得污染下一次打开）、在途时不重复提交、
// 拖放落点的四条边、标签重名与建议查询词的边界、改判不跨打开泄漏。
// 晚到的回调要靠「把后台动作攥在手里、等界面状态变了再放行」来造：测试台默认同步执行后台动作，这里换成可控队列。

// spawnQueue 把 Dialog.Spawn 交来的后台动作先攒着，由测试决定何时执行。
type spawnQueue struct{ fns []func() }

func (q *spawnQueue) spawn(fn func()) { q.fns = append(q.fns, fn) }

// take 取走目前攒下的全部动作。
func (q *spawnQueue) take() []func() {
	out := q.fns
	q.fns = nil
	return out
}

func runAll(fns []func()) {
	for _, fn := range fns {
		fn()
	}
}

// queuedRig 是后台动作由测试放行的测试台（对话框还没打开）。
func queuedRig(t *testing.T) (*rig, *spawnQueue) {
	t.Helper()
	q := &spawnQueue{}
	r := newRig(t, func(r *rig) { r.dialog.Spawn = q.spawn })
	return r, q
}

// 关闭后重新打开，上一次打开发出的合集与标签读取这时才返回：结果丢弃，新一次的结果照常生效。
func TestStaleCollectionAndTagReadsAreDiscardedAfterReopening(t *testing.T) {
	r, q := queuedRig(t)
	r.dialog.Open()
	r.frames(2)
	stale := q.take()
	if len(stale) != 2 {
		t.Fatalf("打开时应发出合集与标签两个读取：%d", len(stale))
	}
	r.dialog.Close()
	r.dialog.Open()
	r.frames(2)
	fresh := q.take()

	runAll(stale)
	r.frames(2)
	if st := r.dialog.snapshot(); len(st.collections) != 0 || len(st.tags) != 0 {
		t.Fatalf("过期的读取结果不该写进新一次打开的缓存：合集 %d、标签 %d", len(st.collections), len(st.tags))
	}

	runAll(fresh)
	r.frames(2)
	if st := r.dialog.snapshot(); len(st.collections) != len(rigCollections) || len(st.tags) != len(rigTags) {
		t.Fatalf("新一次的读取应生效：合集 %d、标签 %d", len(st.collections), len(st.tags))
	}
}

// 上一次打开发出的入队失败这时才返回：不能把新一次打开里正在进行的提交标记成「不在途」。
func TestStaleEnqueueFailureDoesNotClearTheNewSubmitInFlight(t *testing.T) {
	r, q := queuedRig(t)
	r.dialog.Open()
	r.frames(2)
	runAll(q.take())
	r.imports.FailOn("enqueue", errors.New("队列已满"))
	r.draftWith("第一次")
	r.keys("ctrl+enter")
	oldSubmit := q.take()
	if len(oldSubmit) != 1 {
		t.Fatalf("第一次提交应发出一个后台入队：%d", len(oldSubmit))
	}

	r.dialog.Close()
	r.dialog.Open()
	r.frames(2)
	runAll(q.take())
	r.draftWith("第二次")
	r.keys("ctrl+enter")
	newSubmit := q.take()
	if len(newSubmit) != 1 || !r.dialog.snapshot().busy {
		t.Fatalf("第二次提交应在途：后台动作 %d，busy=%v", len(newSubmit), r.dialog.snapshot().busy)
	}

	runAll(oldSubmit) // 上一次的失败这时才到
	if !r.dialog.snapshot().busy {
		t.Fatal("过期的失败回调不该清掉新一次提交的在途标记")
	}
	if !r.dialog.IsOpen() {
		t.Fatal("过期的失败回调不该动对话框")
	}

	r.imports.FailOn("enqueue", nil) // 故障排除，新一次提交成功
	runAll(newSubmit)
	got := r.imports.Enqueued()
	if len(got) != 1 || got[0].Input != "第二次" || r.dialog.IsOpen() {
		t.Fatalf("新一次提交应成功并关闭：已入队 %+v，打开=%v", got, r.dialog.IsOpen())
	}
}

// 上一次打开发出的入队成功这时才返回：结果照常提示，但不能关掉新一次打开的对话框、清掉里面写着的草稿。
func TestStaleEnqueueSuccessDoesNotCloseTheReopenedDialog(t *testing.T) {
	r, q := queuedRig(t)
	r.dialog.Open()
	r.frames(2)
	runAll(q.take())
	r.draftWith("第一次")
	r.keys("ctrl+enter")
	oldSubmit := q.take()
	if len(oldSubmit) != 1 {
		t.Fatalf("应发出一个后台入队：%d", len(oldSubmit))
	}

	r.dialog.Close()
	r.dialog.Open()
	r.frames(2)
	runAll(q.take())
	r.draftWith("第二次正在写")

	runAll(oldSubmit)
	r.frames(3)
	if !r.dialog.IsOpen() || !r.dialogOpen() {
		t.Fatal("过期的成功回调不该关掉新一次打开的对话框")
	}
	if got := r.draftBox().Value; got != "第二次正在写" {
		t.Fatalf("草稿不该丢：%q", got)
	}
	if got := r.imports.Enqueued(); len(got) != 1 || got[0].Input != "第一次" {
		t.Fatalf("上一次的入队照常完成：%+v", got)
	}
	if r.enqueued != 1 || !r.hasText(locales.CaptureEnqueued(1)) {
		t.Fatalf("成功结果照常通知外壳并提示：%d %q", r.enqueued, r.session.Texts())
	}
}

// 提交在途时再按 Ctrl+Enter 不重复入队（「采集」按钮这时是禁用的，快捷键是另一条路）。
func TestSubmitInFlightIsNotDuplicated(t *testing.T) {
	r, q := queuedRig(t)
	r.dialog.Open()
	r.frames(2)
	runAll(q.take())
	r.draftWith("只存一条")
	r.keys("ctrl+enter")
	r.keys("ctrl+enter")
	r.keys("ctrl+enter")
	pending := q.take()
	if len(pending) != 1 {
		t.Fatalf("在途时不该再发后台入队：%d", len(pending))
	}
	if !r.dialog.snapshot().busy {
		t.Fatal("提交在途应标记 busy")
	}
	runAll(pending)
	if got := r.imports.Enqueued(); len(got) != 1 {
		t.Fatalf("只应入队一次：%+v", got)
	}
}

// 拖放落点是草稿框的外框：上、下、左、右四侧紧邻的点都不接收，落在这些点上的文件不加入。
func TestFileDropIgnoresPointsOutsideEveryEdgeOfTheZone(t *testing.T) {
	r := opened(t)
	zone := r.dialog.zone
	if zone.Width == 0 {
		t.Fatal("拖放区的外框没有记录下来")
	}
	midX, midY := zone.X+zone.Width/2, zone.Y+zone.Height/2
	points := map[string][2]float32{
		"左侧": {zone.X - 3, midY},
		"右侧": {zone.X + zone.Width + 3, midY},
		"上侧": {midX, zone.Y - 3},
		"下侧": {midX, zone.Y + zone.Height + 3},
	}
	for name, p := range points {
		if taken, err := r.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: p[0], Y: p[1]}); err != nil || taken {
			t.Fatalf("悬停在外框%s不应接收：taken=%v err=%v", name, taken, err)
		}
		r.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: p[0], Y: p[1], Paths: []string{`C:\out\` + name + `.txt`}})
		r.frames(2)
		if r.dialog.dragOver || len(r.dialog.snapshot().files) != 0 {
			t.Fatalf("外框%s：不该高亮或加入文件：dragOver=%v files=%v", name, r.dialog.dragOver, r.dialog.snapshot().files)
		}
	}
	// 贴着四个角内侧的点算在里面。
	if taken, _ := r.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: zone.X + 1, Y: zone.Y + 1}); !taken {
		t.Fatal("左上角内侧应接收")
	}
	if taken, _ := r.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: zone.X + zone.Width - 1, Y: zone.Y + zone.Height - 1}); !taken {
		t.Fatal("右下角内侧应接收")
	}
}

// 标签重名不分大小写，与先后无关：先加小写再加首字母大写的同名标签也被忽略。
func TestTagDuplicateCheckFoldsCaseInBothDirections(t *testing.T) {
	r := opened(t)
	r.focusTags()
	r.typeText("rust")
	r.keys("enter")
	r.typeText("Rust")
	r.keys("enter")
	r.typeText("RUST")
	r.keys("enter")
	if r.count(gfx.RoleButton, locales.LibraryRemoveTag("rust")) != 1 ||
		r.has(gfx.RoleButton, locales.LibraryRemoveTag("Rust")) || r.has(gfx.RoleButton, locales.LibraryRemoveTag("RUST")) {
		t.Fatalf("只应留下先加的「rust」；画面上有 %q", r.session.Texts())
	}
	if r.tagInput().Value != "" {
		t.Fatalf("重名的也要清空输入：%q", r.tagInput().Value)
	}
}

// 建议的查询词两端的空白不算：「go 」「 go」与「go」建议相同；只有空白时不浮层。
func TestTagSuggestionQueryIgnoresSurroundingSpaces(t *testing.T) {
	r := opened(t)
	r.focusTags()
	r.typeText("go")
	want := r.optionNames()
	if len(want) == 0 {
		t.Fatal("输入 go 应有建议")
	}
	r.typeText(" ")
	if got := r.optionNames(); !slices.Equal(got, want) {
		t.Fatalf("尾部空格不该改变建议：%q，期望 %q", got, want)
	}
	r.keys("cmd+a")
	r.typeText(" go")
	if got := r.optionNames(); !slices.Equal(got, want) {
		t.Fatalf("首部空格不该改变建议：%q，期望 %q", got, want)
	}
	r.keys("cmd+a")
	r.typeText("   ")
	if got := r.optionNames(); len(got) != 0 {
		t.Fatalf("只有空白不该浮层：%q", got)
	}
}

// 改判只属于当次打开：关闭后重新打开，同样的口令文字回到自动判定（默认采集链接），不沿用上次的「存为文本」。
func TestOverrideDoesNotLeakIntoTheNextOpening(t *testing.T) {
	r := opened(t)
	r.draftWith(douyinShareText)
	r.click(gfx.RoleButton, locales.CaptureSwitchToText())
	if !r.hasText(locales.CaptureDetectedText()) {
		t.Fatalf("改判成文本之后：画面上有 %q", r.session.Texts())
	}
	r.keys("escape")
	r.dialog.Open()
	r.frames(4)
	r.draftWith(douyinShareText)
	url := "https://v.douyin.com/0lZNY93J6Ck/"
	if !r.hasText(locales.CaptureExtractedUrl(url)) || !r.has(gfx.RoleButton, locales.CaptureSwitchToText()) || r.hasText(locales.CaptureDetectedText()) {
		t.Fatalf("重新打开后应回到自动判定（采集链接）；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CaptureSubmit())
	if got := r.imports.Enqueued(); len(got) != 1 || got[0].Kind != "url" || got[0].Input != url {
		t.Fatalf("应按链接入队：%+v", got)
	}
}
