package capture

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 对应渲染层 components/capture/CaptureDialog.tsx：打开、识别提示与改判、文件、标签、合集、提交成败、空白笔记、快捷键与拖放。

// 打开：对话框出现，草稿框取得焦点，「采集」禁用，提示栏是默认说明；合集与标签在后台读到。
func TestOpenShowsAnEmptyFocusedDialog(t *testing.T) {
	r := opened(t)
	if !r.dialogOpen() {
		t.Fatalf("应显示标题「快速采集」；画面上有 %q", r.session.Texts())
	}
	box := r.draftBox()
	if !box.Multiline || !box.Focused || box.Value != "" {
		t.Fatalf("草稿框应是取得焦点的多行空框：%+v", box)
	}
	if !r.submitButton().Disabled {
		t.Fatal("草稿与文件都空时「采集」应禁用")
	}
	if !r.hasText(locales.CaptureHint()) {
		t.Fatalf("应显示默认说明；画面上有 %q", r.session.Texts())
	}
	for _, name := range []string{locales.CaptureChooseFiles(), locales.CaptureBlankNote()} {
		if !r.has(gfx.RoleButton, name) {
			t.Fatalf("缺少按钮「%s」", name)
		}
	}
	if !r.has(gfx.RoleCombobox, locales.LibraryCollection()) || !r.hasText(locales.LibraryNoCollection()) {
		t.Fatalf("合集下拉默认是「未归合集」；画面上有 %q", r.session.Texts())
	}
	if r.has(gfx.RoleButton, locales.CaptureSwitchToText()) || r.has(gfx.RoleButton, locales.CaptureSwitchToUrl()) {
		t.Fatal("没有输入时不该有改判按钮")
	}
}

// 识别提示随草稿变化：单个链接、多个链接、纯文本各有各的提示与图标，清空后回到默认说明。
func TestStatusBarFollowsTheDraft(t *testing.T) {
	r := opened(t)
	r.draftWith("https://example.com/a")
	if !r.hasText(locales.CaptureDetectedUrl()) || r.submitButton().Disabled {
		t.Fatalf("单个链接：画面上有 %q", r.session.Texts())
	}
	r.keys("cmd+a")
	r.typeText("https://example.com/a\nhttps://example.com/b\nhttps://example.com/a")
	if !r.hasText(locales.CaptureDetectedUrls(2)) {
		t.Fatalf("两个不同的链接（重复的去掉）：画面上有 %q", r.session.Texts())
	}
	if r.has(gfx.RoleButton, locales.CaptureSwitchToText()) {
		t.Fatal("纯链接不需要改判")
	}
	r.keys("cmd+a")
	r.typeText("会议纪要\n第一条")
	if !r.hasText(locales.CaptureDetectedText()) || r.submitButton().Disabled {
		t.Fatalf("纯文本：画面上有 %q", r.session.Texts())
	}
	r.keys("cmd+a")
	r.keys("backspace")
	if !r.hasText(locales.CaptureHint()) || !r.submitButton().Disabled {
		t.Fatalf("清空后回到默认说明且禁用：画面上有 %q", r.session.Texts())
	}
}

// 文字夹链接：视频平台的分享口令默认采集链接（显示抠出的链接），可以一键改判为存文本，再改回来。
func TestMixedDraftCanBeSwitchedBetweenUrlsAndText(t *testing.T) {
	r := opened(t)
	r.draftWith(douyinShareText)
	if !r.hasText(locales.CaptureExtractedUrl("https://v.douyin.com/0lZNY93J6Ck/")) {
		t.Fatalf("应提示已提取的链接；画面上有 %q", r.session.Texts())
	}
	if !r.has(gfx.RoleButton, locales.CaptureSwitchToText()) {
		t.Fatal("应有「改为保存为文本」")
	}
	r.click(gfx.RoleButton, locales.CaptureSwitchToText())
	if !r.hasText(locales.CaptureDetectedText()) || !r.has(gfx.RoleButton, locales.CaptureSwitchToUrl()) {
		t.Fatalf("改判成文本之后：画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CaptureSwitchToUrl())
	if !r.hasText(locales.CaptureExtractedUrl("https://v.douyin.com/0lZNY93J6Ck/")) || !r.has(gfx.RoleButton, locales.CaptureSwitchToText()) {
		t.Fatalf("改回链接之后：画面上有 %q", r.session.Texts())
	}
}

// 普通网页夹说明文字：默认存文本；有多个链接时改判按钮写明个数，改判后按提取的链接入队。
func TestMixedDraftWithPlainLinksPrefersText(t *testing.T) {
	r := opened(t)
	r.draftWith("明天看 https://example.com/a 和 https://example.com/b")
	if !r.hasText(locales.CaptureDetectedText()) || !r.has(gfx.RoleButton, locales.CaptureSwitchToUrls(2)) {
		t.Fatalf("默认存文本、按钮写明两个链接：画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CaptureSwitchToUrls(2))
	if !r.hasText(locales.CaptureExtractedUrls(2)) || !r.has(gfx.RoleButton, locales.CaptureSwitchToText()) {
		t.Fatalf("改判成链接之后：画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CaptureSubmit())
	got := r.imports.Enqueued()
	if len(got) != 2 || got[0].Kind != "url" || got[0].Input != "https://example.com/a" || got[1].Input != "https://example.com/b" {
		t.Fatalf("应按提取的两个链接入队：%+v", got)
	}
}

// 选择文件：路径列在草稿框下面，只有文件也能采集；再选同一个文件不重复；可以逐个移除。
func TestPickedFilesAreListedDedupedAndRemovable(t *testing.T) {
	r := opened(t)
	r.imports.SetFiles([]string{`C:\notes\a.md`, `C:\notes\b.png`})
	r.click(gfx.RoleButton, locales.CaptureChooseFiles())
	for _, p := range []string{`C:\notes\a.md`, `C:\notes\b.png`} {
		if !r.hasText(p) {
			t.Fatalf("应列出 %s；画面上有 %q", p, r.session.Texts())
		}
	}
	if r.submitButton().Disabled {
		t.Fatal("有文件时即使草稿为空也能采集")
	}
	r.imports.SetFiles([]string{`C:\notes\b.png`, `C:\notes\c.mp3`})
	r.click(gfx.RoleButton, locales.CaptureChooseFiles())
	if got := r.count(gfx.RoleButton, locales.CommonRemove()); got != 3 {
		t.Fatalf("重复的文件不该再列一次，应有 3 行：%d", got)
	}
	r.clickNth(gfx.RoleButton, locales.CommonRemove(), 1) // 移除 b.png
	if r.hasText(`C:\notes\b.png`) || !r.hasText(`C:\notes\a.md`) || !r.hasText(`C:\notes\c.mp3`) {
		t.Fatalf("只应移除 b.png；画面上有 %q", r.session.Texts())
	}
	r.clickNth(gfx.RoleButton, locales.CommonRemove(), 0)
	r.clickNth(gfx.RoleButton, locales.CommonRemove(), 0)
	if !r.submitButton().Disabled || r.has(gfx.RoleButton, locales.CommonRemove()) {
		t.Fatal("文件全移除、草稿也空之后应禁用")
	}
}

// 选择文件失败：提示原因，对话框与已选的文件不动。
func TestPickFilesFailureIsReported(t *testing.T) {
	r := opened(t)
	r.imports.FailOn("select", errors.New("文件选择框打不开"))
	r.click(gfx.RoleButton, locales.CaptureChooseFiles())
	if !r.dialogOpen() || !r.hasText(locales.ThemedReadingActionFailed(locales.CaptureChooseFiles())) {
		t.Fatalf("应提示失败并保持打开：%q", r.session.Texts())
	}
	if got := r.log.entries(); len(got) != 1 || !strings.Contains(got[0], "文件选择框打不开") {
		t.Fatalf("应留痕：%v", got)
	}
}

// 标签建议：输入时浮出包含输入的已有标签（不分大小写、至多 6 个、排除已加的），点一项加上并保持输入焦点。
func TestTagSuggestionsFilterAndAdd(t *testing.T) {
	r := opened(t)
	r.focusTags()
	r.typeText("go")
	got := r.optionNames()
	want := []string{"golang", "Go语言", "GoLand", "gopher", "gorm", "gofmt"} // 8 个标签里含 go 的有 7 个，截到 6 个
	if !slices.Equal(got, want) {
		t.Fatalf("建议 = %q，期望 %q", got, want)
	}
	if !r.has(gfx.RoleListBox, locales.CaptureExistingTags()) {
		t.Fatal("建议应在 listbox 里")
	}
	r.click(gfx.RoleOption, "golang")
	if r.has(gfx.RoleOption, "golang") || len(r.optionNames()) != 0 {
		t.Fatalf("点选之后输入框清空、建议消失：%q", r.optionNames())
	}
	if !r.has(gfx.RoleButton, locales.LibraryRemoveTag("golang")) {
		t.Fatal("应加上标签小块")
	}
	if !r.tagInput().Focused {
		t.Fatal("点建议不该抢走输入框的焦点")
	}
	r.typeText("go")
	if slices.Contains(r.optionNames(), "golang") {
		t.Fatalf("已加的标签不再建议：%q", r.optionNames())
	}
	r.typeText("zzz")
	if len(r.optionNames()) != 0 {
		t.Fatalf("没有匹配时不浮层：%q", r.optionNames())
	}
}

// 回车加标签：去掉首尾空白，空的与重名（不分大小写）的忽略，加完清空输入；可以点 × 移除。
func TestTagsAddByEnterRemoveAndBackspace(t *testing.T) {
	r := opened(t)
	r.focusTags()
	r.typeText("  新标签  ")
	r.keys("enter")
	if !r.has(gfx.RoleButton, locales.LibraryRemoveTag("新标签")) {
		t.Fatalf("应加上去掉空白的「新标签」；画面上有 %q", r.session.Texts())
	}
	if r.tagInput().Value != "" {
		t.Fatalf("加完应清空输入：%q", r.tagInput().Value)
	}
	r.typeText("Rust")
	r.keys("enter")
	r.typeText("rust")
	r.keys("enter")
	if r.count(gfx.RoleButton, locales.LibraryRemoveTag("Rust"))+r.count(gfx.RoleButton, locales.LibraryRemoveTag("rust")) != 1 {
		t.Fatal("大小写不同的重名标签只留第一个")
	}
	if r.tagInput().Value != "" {
		t.Fatal("重名的也要清空输入")
	}
	r.keys("enter") // 空输入回车：什么也不加
	if r.count(gfx.RoleButton, locales.LibraryRemoveTag("")) != 0 {
		t.Fatal("空标签不能加")
	}
	r.click(gfx.RoleButton, locales.LibraryRemoveTag("新标签"))
	if !r.chipRemoved("新标签") || !r.has(gfx.RoleButton, locales.LibraryRemoveTag("Rust")) {
		t.Fatal("点 × 只移除对应的标签")
	}
	// 渲染层的「输入为空时退格删最后一个标签」在 Viva 里做不到（文本框把 Backspace 当编辑命令消化，见 tagRowView 的说明）。
	r.click(gfx.RoleButton, locales.LibraryRemoveTag("Rust"))
	if !r.chipRemoved("Rust") {
		t.Fatal("最后一个标签也应能移除")
	}
}

// 提交：链接逐条、文件逐个，都带合集与标签；成功提示、通知外壳刷新队列并关闭对话框。
func TestSubmitEnqueuesUrlsFilesCollectionAndTags(t *testing.T) {
	r := opened(t)
	r.draftWith("https://example.com/a\nhttps://example.com/b")
	r.imports.SetFiles([]string{`C:\notes\a.md`})
	r.click(gfx.RoleButton, locales.CaptureChooseFiles())
	r.focusTags()
	r.typeText("资料")
	r.keys("enter")
	r.click(gfx.RoleCombobox, locales.LibraryCollection())
	r.click(gfx.RoleOption, "灵感")
	if !r.hasText("灵感") {
		t.Fatalf("合集下拉应显示所选；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CaptureSubmit())

	got := r.imports.Enqueued()
	if len(got) != 3 {
		t.Fatalf("应入队 3 项：%+v", got)
	}
	kinds := []string{got[0].Kind, got[1].Kind, got[2].Kind}
	inputs := []string{got[0].Input, got[1].Input, got[2].Input}
	if !slices.Equal(kinds, []string{"url", "url", "file"}) ||
		!slices.Equal(inputs, []string{"https://example.com/a", "https://example.com/b", `C:\notes\a.md`}) {
		t.Fatalf("入队内容 = %v %v", kinds, inputs)
	}
	for _, in := range got {
		if in.CollectionID == nil || *in.CollectionID != "c2" || !slices.Equal(in.TagNames, []string{"资料"}) {
			t.Fatalf("每一项都应带合集 c2 与标签：%+v", in)
		}
	}
	if r.enqueued != 1 {
		t.Fatalf("应通知外壳刷新队列一次：%d", r.enqueued)
	}
	if !r.hasText(locales.CaptureEnqueued(3)) {
		t.Fatalf("应提示「已加入采集队列（3 项）」；画面上有 %q", r.session.Texts())
	}
	if r.dialogOpen() || r.dialog.IsOpen() {
		t.Fatal("成功后应关闭对话框")
	}
}

// 提交文字：存成一条文本笔记；没选合集与标签时这两项为空。
func TestSubmitTextHasNoCollectionOrTagsByDefault(t *testing.T) {
	r := opened(t)
	r.draftWith("  会议纪要\n第一条  ")
	r.click(gfx.RoleButton, locales.CaptureSubmit())
	got := r.imports.Enqueued()
	if len(got) != 1 || got[0].Kind != "text" || got[0].Input != "会议纪要\n第一条" || got[0].CollectionID != nil || len(got[0].TagNames) != 0 {
		t.Fatalf("入队内容：%+v", got)
	}
	if !r.hasText(locales.CaptureEnqueued(1)) || r.dialogOpen() {
		t.Fatalf("应提示并关闭：%q", r.session.Texts())
	}
}

// 入队失败：提示带原因的错误（详情可展开复制），对话框、草稿、文件、标签原样留着，修好之后可以直接重试。
func TestEnqueueFailureKeepsTheDialogAndAllowsRetry(t *testing.T) {
	r := opened(t)
	r.draftWith("https://example.com/a")
	r.imports.SetFiles([]string{`C:\notes\a.md`})
	r.click(gfx.RoleButton, locales.CaptureChooseFiles())
	r.imports.FailOn("enqueue", errors.New("队列已满"))
	r.click(gfx.RoleButton, locales.CaptureSubmit())

	if !r.dialogOpen() {
		t.Fatal("失败后对话框应留着")
	}
	if !r.hasText(locales.CaptureEnqueueFailed("队列已满")) {
		t.Fatalf("应提示失败原因；画面上有 %q", r.session.Texts())
	}
	if r.draftBox().Value != "https://example.com/a" || !r.hasText(`C:\notes\a.md`) {
		t.Fatalf("草稿与文件不该丢：%q", r.draftBox().Value)
	}
	if r.enqueued != 0 || len(r.imports.Enqueued()) != 0 {
		t.Fatal("失败不该通知外壳、也不该入队")
	}
	if logs := r.log.entries(); len(logs) != 1 || !strings.Contains(logs[0], "队列已满") {
		t.Fatalf("应留痕：%v", logs)
	}
	if r.submitButton().Disabled {
		t.Fatal("失败后「采集」应可再点")
	}

	r.imports.FailOn("enqueue", nil)
	r.click(gfx.RoleButton, locales.CaptureSubmit())
	if r.dialogOpen() || len(r.imports.Enqueued()) != 2 || !r.hasText(locales.CaptureEnqueued(2)) {
		t.Fatalf("重试应成功并关闭：已入队 %d 项", len(r.imports.Enqueued()))
	}
}

// 空白笔记：关闭对话框并把目标合集交给外壳（外壳切到知识库、新建条目）。
func TestBlankNoteClosesAndHandsOffTheCollection(t *testing.T) {
	r := opened(t)
	r.click(gfx.RoleCombobox, locales.LibraryCollection())
	r.click(gfx.RoleOption, "工作")
	r.click(gfx.RoleButton, locales.CaptureBlankNote())
	if !slices.Equal(r.blankNotes, []string{"c1"}) {
		t.Fatalf("应交出合集 c1：%v", r.blankNotes)
	}
	if r.dialogOpen() || len(r.imports.Enqueued()) != 0 {
		t.Fatal("对话框应关闭，且不入队")
	}
}

// 没有空白笔记入口（外壳没接）时不显示这个按钮。
func TestBlankNoteButtonIsHiddenWithoutAHook(t *testing.T) {
	r := opened(t, func(r *rig) { r.dialog.Hooks.BlankNote = nil })
	if r.has(gfx.RoleButton, locales.CaptureBlankNote()) {
		t.Fatal("没有 BlankNote 回调时不该显示「空白笔记」")
	}
}

// Ctrl+Enter 在草稿框里提交；草稿与文件都空时按它什么也不发生。
func TestCtrlEnterSubmitsOnlyWhenThereIsSomething(t *testing.T) {
	r := opened(t)
	r.keys("ctrl+enter")
	if !r.dialogOpen() || len(r.imports.Enqueued()) != 0 {
		t.Fatal("空草稿按 Ctrl+Enter 不该提交")
	}
	if r.draftBox().Value != "" {
		t.Fatalf("Ctrl+Enter 不该在草稿里插入换行：%q", r.draftBox().Value)
	}
	r.draftWith("随手记一笔")
	r.keys("ctrl+enter")
	got := r.imports.Enqueued()
	if len(got) != 1 || got[0].Kind != "text" || got[0].Input != "随手记一笔" {
		t.Fatalf("Ctrl+Enter 应提交：%+v", got)
	}
	if r.dialogOpen() {
		t.Fatal("提交成功后应关闭")
	}
}

// 回车只是换行，不提交。
func TestPlainEnterInsertsANewLine(t *testing.T) {
	r := opened(t)
	r.draftWith("第一行")
	r.keys("enter")
	r.typeText("第二行")
	if got := r.draftBox().Value; got != "第一行\n第二行" {
		t.Fatalf("草稿 = %q", got)
	}
	if len(r.imports.Enqueued()) != 0 || !r.dialogOpen() {
		t.Fatal("回车不该提交")
	}
}

// Esc 关闭；再次打开是全新的：草稿、文件、标签、合集、改判都清空。
func TestEscapeClosesAndReopeningStartsFresh(t *testing.T) {
	r := opened(t)
	r.draftWith(douyinShareText)
	r.click(gfx.RoleButton, locales.CaptureSwitchToText())
	r.imports.SetFiles([]string{`C:\notes\a.md`})
	r.click(gfx.RoleButton, locales.CaptureChooseFiles())
	r.focusTags()
	r.typeText("临时")
	r.keys("enter")
	r.click(gfx.RoleCombobox, locales.LibraryCollection())
	r.click(gfx.RoleOption, "工作")

	r.keys("escape")
	if r.dialogOpen() || r.dialog.IsOpen() {
		t.Fatal("Esc 应关闭对话框")
	}
	r.dialog.Open()
	r.frames(4)
	if !r.dialogOpen() || r.draftBox().Value != "" || r.hasText(`C:\notes\a.md`) || r.has(gfx.RoleButton, locales.LibraryRemoveTag("临时")) ||
		!r.hasText(locales.LibraryNoCollection()) || !r.hasText(locales.CaptureHint()) || !r.submitButton().Disabled {
		t.Fatalf("重新打开应是空白的；画面上有 %q", r.session.Texts())
	}
}

// 已经打开时再要求打开（快捷键再按一次）不能清掉正在写的内容。
func TestOpeningAnOpenDialogKeepsTheDraft(t *testing.T) {
	r := opened(t)
	r.draftWith("写到一半")
	r.dialog.Open()
	r.frames(2)
	if got := r.draftBox().Value; got != "写到一半" {
		t.Fatalf("草稿被清掉了：%q", got)
	}
}

// 关闭按钮与点遮罩也能关；没有任何后台调用残留。
func TestCloseButtonClosesTheDialog(t *testing.T) {
	r := opened(t)
	r.click(gfx.RoleButton, locales.CommonClose())
	if r.dialogOpen() || r.dialog.IsOpen() {
		t.Fatal("关闭钮应关闭对话框")
	}
}

// 系统文件拖放：悬停在草稿框区域时接收（拖放区高亮），落下时路径追加到文件列表；落在区域外、对话框没开时不接收。
func TestSystemFileDropOnlyLandsInTheDraftZone(t *testing.T) {
	r := opened(t)
	zone := r.dialog.zone
	if zone.Width == 0 {
		t.Fatal("拖放区的外框没有记录下来")
	}
	inside := func(phase gfx.FileDropPhase, paths ...string) gfx.FileDropEvent {
		return gfx.FileDropEvent{Phase: phase, X: zone.X + zone.Width/2, Y: zone.Y + zone.Height/2, Paths: paths}
	}
	if taken, err := r.session.Drop(inside(gfx.FileDropOver)); err != nil || !taken {
		t.Fatalf("悬停在草稿框上应接收：taken=%v err=%v", taken, err)
	}
	r.frames(2)
	if !r.dialog.dragOver {
		t.Fatal("悬停时拖放区应进入高亮状态")
	}
	outside := gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 5, Y: 5}
	if taken, err := r.session.Drop(outside); err != nil || taken {
		t.Fatalf("悬停在区域外不应接收：taken=%v err=%v", taken, err)
	}
	r.frames(2)
	if r.dialog.dragOver {
		t.Fatal("移出之后高亮应取消")
	}

	r.session.Drop(inside(gfx.FileDropOver))
	if taken, err := r.session.Drop(inside(gfx.FileDropDrop, `C:\drop\a.txt`, `C:\drop\b.mp4`)); err != nil || !taken {
		t.Fatalf("落在草稿框上应接收：taken=%v err=%v", taken, err)
	}
	r.frames(3)
	if !r.hasText(`C:\drop\a.txt`) || !r.hasText(`C:\drop\b.mp4`) || r.dialog.dragOver {
		t.Fatalf("落下的文件应列出、高亮取消；画面上有 %q", r.session.Texts())
	}
	r.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: 5, Y: 5, Paths: []string{`C:\drop\c.txt`}})
	r.frames(2)
	if r.hasText(`C:\drop\c.txt`) {
		t.Fatal("落在区域外的文件不该加入")
	}
	r.session.Drop(inside(gfx.FileDropDrop, `C:\drop\a.txt`))
	r.frames(2)
	if got := r.count(gfx.RoleButton, locales.CommonRemove()); got != 2 {
		t.Fatalf("重复落下的文件不重复列出：%d", got)
	}

	r.keys("escape")
	if taken, _ := r.session.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: zone.X + 10, Y: zone.Y + 10}); taken {
		t.Fatal("对话框关闭后不该接收文件")
	}
}

// 顶栏「采集文件」：选完文件直接入队（不带合集与标签）并提示，失败时给出原因；与对话框开不开无关。
func TestImportFilesEnqueuesWithoutTheDialog(t *testing.T) {
	r := newRig(t)
	r.imports.SetFiles([]string{`C:\notes\a.md`, `C:\notes\b.png`})
	r.dialog.ImportFiles()
	r.frames(3)
	got := r.imports.Enqueued()
	if len(got) != 2 || got[0].Kind != "file" || got[1].Input != `C:\notes\b.png` || got[0].CollectionID != nil || len(got[0].TagNames) != 0 {
		t.Fatalf("入队内容：%+v", got)
	}
	if r.enqueued != 1 || !r.hasText(locales.CaptureEnqueued(2)) {
		t.Fatalf("应通知外壳并提示：%d %q", r.enqueued, r.session.Texts())
	}

	r.imports.SetFiles(nil)
	r.dialog.ImportFiles() // 用户取消了选择框：什么也不做
	if len(r.imports.Enqueued()) != 2 {
		t.Fatal("取消选择不该入队")
	}

	r.imports.SetFiles([]string{`C:\notes\c.md`})
	r.imports.FailOn("enqueue", errors.New("队列已满"))
	r.dialog.ImportFiles()
	r.frames(3)
	if !r.hasText(locales.CaptureEnqueueFailed("队列已满")) || len(r.imports.Enqueued()) != 2 {
		t.Fatalf("应提示失败原因：%q", r.session.Texts())
	}
}

// 合集与标签读不到时：只记日志，对话框照常能用（只是没有可选项）。
func TestLoadFailuresOfCollectionsAndTagsAreLoggedNotBlocking(t *testing.T) {
	r := newRig(t, func(r *rig) {
		r.library.FailOn("Collections", errors.New("库读不出来"))
		r.library.FailOn("Tags", errors.New("标签读不出来"))
	})
	r.dialog.Open()
	r.frames(4)
	if !r.dialogOpen() {
		t.Fatal("读取失败不该影响打开")
	}
	logs := strings.Join(r.log.entries(), "\n")
	if !strings.Contains(logs, "库读不出来") || !strings.Contains(logs, "标签读不出来") {
		t.Fatalf("两个失败都应留痕：%q", logs)
	}
	r.focusTags()
	r.typeText("go")
	if len(r.optionNames()) != 0 {
		t.Fatal("没读到标签就没有建议")
	}
}

// 晚到的结果不能污染下一次打开：关闭后又打开，上一次选文件的结果丢弃。
func TestStaleFilePickIsDiscardedAfterReopening(t *testing.T) {
	r := opened(t)
	epoch := r.dialog.epoch
	r.dialog.Close()
	r.dialog.Open()
	r.dialog.addFiles(epoch, []string{`C:\late\x.md`}) // 上一次打开发出的选择，这时才返回
	r.frames(3)
	if r.hasText(`C:\late\x.md`) {
		t.Fatal("过期的选择结果不该出现在新打开的对话框里")
	}
}

func TestBuildInputsCopiesTargetAndTags(t *testing.T) {
	tags := []string{"a", "b"}
	inputs := buildInputs(Action{Kind: ActionText, Text: "x"}, []string{"f1"}, "c9", tags)
	tags[0] = "changed"
	if len(inputs) != 2 || inputs[0].Kind != "text" || inputs[1].Kind != "file" {
		t.Fatalf("%+v", inputs)
	}
	for _, in := range inputs {
		if in.CollectionID == nil || *in.CollectionID != "c9" || !slices.Equal(in.TagNames, []string{"a", "b"}) {
			t.Fatalf("目标与标签应各自带上且不受调用方后续修改影响：%+v", in)
		}
	}
	if got := buildInputs(Action{}, nil, "", nil); len(got) != 0 {
		t.Fatalf("什么都没有就不入队：%+v", got)
	}
}
