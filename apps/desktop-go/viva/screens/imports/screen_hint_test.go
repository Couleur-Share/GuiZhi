package imports

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测悬停提示（渲染层的 title 属性）的宿主与文字：行内的前导槽、标题、时间，以及挂在容器上的来源筛选组。
// 提示层本身（320 ms 延迟、定位、翻转、容器继承）见 kit/tooltip_test.go；这里只验证采集界面把提示挂对了地方。

// hintHit 是画面上声明了提示 text 的第一个命中区。
func (r *rig) hintHit(text string) (gfx.Hit, bool) {
	for _, h := range r.session.Snapshot().Hits {
		if h.Hint == text && !h.Block {
			return h, true
		}
	}
	return gfx.Hit{}, false
}

// hoverHint 把指针移到声明了提示 text 的命中区中心，等过提示的延迟，报告气泡（名称就是提示文字的分组）是否出现。
func (r *rig) hoverHint(text string) bool {
	r.t.Helper()
	hit, ok := r.hintHit(text)
	if !ok {
		r.t.Fatalf("画面上没有声明提示 %q 的命中区", text)
	}
	return r.hoverAt(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2, text)
}

// hoverAt 把指针移到 (x, y)，等过提示的延迟，报告提示 text 的气泡是否出现。
func (r *rig) hoverAt(x, y float32, text string) bool {
	r.t.Helper()
	if err := r.session.Move(x, y); err != nil {
		r.t.Fatalf("移动指针到 (%.0f, %.0f) 失败：%v", x, y, err)
	}
	r.frames(3)
	if err := r.session.Advance(kit.TooltipDelay + 20*time.Millisecond); err != nil {
		r.t.Fatalf("推进时钟失败：%v", err)
	}
	r.frames(3)
	return r.has(gfx.RoleGroup, text)
}

// 前导槽的提示：有条目类型时是类型名，没有时是「选择」（title={itemTypeLabel ?? t('imports.select')}）。
func TestLeadingSlotHintIsItemTypeOrSelect(t *testing.T) {
	typed := mkTask("typed", nil) // 视频
	untyped := mkTask("untyped", func(x *services.ImportTask) { x.ItemType = "" })
	for _, tc := range []struct {
		name string
		task services.ImportTask
		want string
	}{
		{"typed", typed, kit.GetItemTypeMeta("video").Label},
		{"untyped", untyped, locales.ImportsSelect()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := loaded(t, pageOf(tc.task))
			if !r.hoverHint(tc.want) {
				t.Fatalf("悬停前导槽 320 ms 后应出提示 %q；画面上有 %q", tc.want, r.session.Texts())
			}
		})
	}
}

// 标题：显示名顶掉原始地址时，原始地址是提示（title={sourceTooltip}）——不能打开结果的行（普通文字）与能打开的行（文字按钮）都有。
func TestRowTitleHintIsSourceInputWhenNamed(t *testing.T) {
	const source = "https://www.bilibili.com/video/BV1xx"
	for _, tc := range []struct {
		name string
		task services.ImportTask
	}{
		{"plain-text", mkTask("run", nil)},
		{"openable-button", completed(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := loaded(t, pageOf(tc.task))
			if !r.hoverHint(source) {
				t.Fatalf("悬停标题 320 ms 后应出原始地址的提示；画面上有 %q", r.session.Texts())
			}
		})
	}
}

// 没有显示名时标题就是原始地址本身，没有额外的提示。
func TestRowTitleHasNoHintWithoutDisplayName(t *testing.T) {
	r := loaded(t, pageOf(mkTask("bare", func(x *services.ImportTask) { x.DisplayName = "" })))
	if _, ok := r.hintHit("https://www.bilibili.com/video/BV1xx"); ok {
		t.Fatal("没有显示名时标题不该再挂原始地址的提示")
	}
}

// 时间的提示写入队与更新两个时刻。（行悬停时操作栏会盖在时间上，指针够不到它；这里只核对声明的文字。）
func TestRowTimeHintListsQueuedAndUpdated(t *testing.T) {
	task := mkTask("run", nil)
	r := loaded(t, pageOf(task))
	want := locales.ImportsTimeTooltip(
		kit.FormatDateTime(task.CreatedAt, r.screen.loc(), r.screen.Locale),
		kit.FormatDateTime(task.UpdatedAt, r.screen.loc(), r.screen.Locale),
	)
	if _, ok := r.hintHit(want); !ok {
		t.Fatalf("时间应声明提示 %q", want)
	}
}

// 来源筛选组的 title 挂在容器上：指针落在组里的选项按钮上（按钮自己没有提示）也出这条提示。
func TestOriginGroupHintAppliesToItsButtons(t *testing.T) {
	r := loaded(t, mixed())
	hint := locales.ImportsOriginScopeHint()
	btn, ok := r.node(gfx.RoleButton, locales.ImportsOriginDesktop())
	if !ok {
		t.Fatalf("找不到来源筛选的「%s」按钮", locales.ImportsOriginDesktop())
	}
	if !r.hoverAt(btn.Bounds.X+btn.Bounds.Width/2, btn.Bounds.Y+btn.Bounds.Height/2, hint) {
		t.Fatalf("指针在筛选组的按钮上 320 ms 后应出组的提示 %q；画面上有 %q", hint, r.session.Texts())
	}
	// 组本身是一个带名称的分组；title 同时是它的可访问描述，但悬停期间被 TooltipLayer 摘了下来（宿主是它），
	// 所以此刻没有描述，指针移开后装回去。
	group, ok := r.node(gfx.RoleGroup, locales.ImportsOriginFilter())
	if !ok || group.Description != "" {
		t.Fatalf("悬停期间来源筛选组（名为 %q）不该有描述：%+v（找到 %v）", locales.ImportsOriginFilter(), group, ok)
	}
	if err := r.session.Move(2, 2); err != nil {
		t.Fatalf("移开指针失败：%v", err)
	}
	r.frames(3)
	group, ok = r.node(gfx.RoleGroup, locales.ImportsOriginFilter())
	if !ok || group.Description != hint {
		t.Fatalf("移开后来源筛选组应是名为 %q、描述为 %q 的分组：%+v（找到 %v）", locales.ImportsOriginFilter(), hint, group, ok)
	}
}
