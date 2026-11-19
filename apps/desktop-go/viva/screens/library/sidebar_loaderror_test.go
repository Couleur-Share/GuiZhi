package library

import (
	"errors"
	"sort"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 侧栏读取失败的错误态（台账 B-036；渲染层把「读不出来」画成空态，这里按 AGENTS.md 画成错误态）：
// 某份数据从没成功读到过、且最近一次读取失败时，对应分区的位置换成 LoadErrorState（原因加「重试」，重试重新读取三份数据）；
// 读到过之后的刷新失败只留日志，不清已有数据。

// countText 是画面上恰好等于 text 的文字有几处。
func (r *rig) countText(text string) int {
	n := 0
	for _, got := range r.session.Texts() {
		if got == text {
			n++
		}
	}
	return n
}

// tall 把窗口放高：四块错误态叠在一起很高，默认的 800 px 里回收站在折叠线下面，访问树里没有被滚出去的控件，
// 「没有这个按钮」的断言会空转。
func (r *rig) tall() {
	r.t.Helper()
	if err := r.session.Resize(1280, 1600); err != nil {
		r.t.Fatalf("调整窗口失败：%v", err)
	}
	r.frames(3)
}

func failAll(r *rig) {
	r.svc.FailOn("Counts", errors.New("counts boom"))
	r.svc.FailOn("Collections", errors.New("collections boom"))
	r.svc.FailOn("Tags", errors.New("tags boom"))
}

func healAll(r *rig) {
	r.svc.FailOn("Counts", nil)
	r.svc.FailOn("Collections", nil)
	r.svc.FailOn("Tags", nil)
}

// 三份都读不出来：合集、平台、标签、回收站四处都是错误态，没有一处渲染成「还没有」的空态。
func TestSidebarShowsErrorStatesInsteadOfEmptyStates(t *testing.T) {
	r := newRig(t, failAll)
	r.tall()
	if got := r.countText(locales.CommonLoadFailed()); got != 4 {
		t.Fatalf("「读取失败」应出现 4 处（合集、平台、标签、回收站），实际 %d 处", got)
	}
	if got := r.countText(locales.CommonRetry()); got != 4 {
		t.Errorf("「重试」按钮应有 4 个，实际 %d 个", got)
	}
	// 原因：合集是合集自己的，平台与回收站是计数的，标签优先标签自己的。
	for text, want := range map[string]int{"collections boom": 1, "counts boom": 2, "tags boom": 1} {
		if got := r.countText(text); got != want {
			t.Errorf("原因「%s」应出现 %d 处，实际 %d 处", text, want, got)
		}
	}
	for _, empty := range []string{locales.LibraryNoCollections(), locales.LibraryNoPlatforms(), locales.LibraryNoTags()} {
		if r.hasText(empty) {
			t.Errorf("读取失败不能画成空态：画面上有「%s」", empty)
		}
	}
	// 分区标题照旧：错误态替换的是分区里的内容，不是整个分区。
	for _, heading := range []string{locales.LibraryCollections(), locales.LibraryPlatforms(), locales.LibraryTags()} {
		if !r.hasText(heading) {
			t.Errorf("读取失败时分区标题「%s」应还在", heading)
		}
	}
	if r.has(gfx.RoleButton, locales.LibraryScopeTrash()) {
		t.Error("回收站的数字读不出来：该处是错误态，不是一行没有计数的回收站")
	}
}

// 只有计数读不出来：哪些标签有条目、平台有哪些、回收站有几条都靠计数，这三处是错误态；合集自己读到了，照常。
func TestCountsFailureBlocksPlatformsTagsAndTrashButNotCollections(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.FailOn("Counts", errors.New("counts boom")) })
	r.tall()
	if got := r.countText(locales.CommonLoadFailed()); got != 3 {
		t.Fatalf("「读取失败」应出现 3 处（平台、标签、回收站），实际 %d 处", got)
	}
	if got := r.countText("counts boom"); got != 3 {
		t.Errorf("原因「counts boom」应出现 3 处，实际 %d 处", got)
	}
	for _, name := range []string{"工作", "阅读"} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("合集读到了，应照常显示「%s」", name)
		}
	}
	for _, name := range []string{"go", "rust", locales.LibraryScopeTrash()} {
		if r.has(gfx.RoleButton, name) {
			t.Errorf("计数读不出来时不该画「%s」（有没有条目、有几条都不知道）", name)
		}
	}
	if r.hasText(locales.LibraryNoTags()) {
		t.Error("标签没读错，但计数读不出来：不能画成「在条目详情中添加标签」")
	}
}

// 只有标签读不出来：只有标签分区是错误态，其余照常。
func TestOnlyTheFailedSectionShowsAnError(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.FailOn("Tags", errors.New("tags boom")) })
	if got := r.countText(locales.CommonLoadFailed()); got != 1 {
		t.Fatalf("「读取失败」应只出现 1 处，实际 %d 处", got)
	}
	if !r.hasText("tags boom") {
		t.Error("应显示标签读取失败的原因")
	}
	for _, name := range []string{"工作", "阅读", locales.LibraryScopeTrash()} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("合集与回收站照常显示，缺「%s」", name)
		}
	}
	if r.hasText(locales.LibraryNoTags()) {
		t.Error("标签读取失败不能画成「在条目详情中添加标签」")
	}
}

// 点「重试」重新读取三份数据：故障排除之后一次点击全部恢复；故障还在时错误态保持。
func TestRetryReloadsAllThreeAndRecovers(t *testing.T) {
	r := newRig(t, failAll)
	before := len(r.svc.Calls())
	r.click(gfx.RoleButton, locales.CommonRetry())
	calls := map[string]int{}
	for _, call := range r.svc.Calls()[before:] {
		calls[call]++
	}
	for _, method := range []string{"Counts", "Collections", "Tags"} {
		if calls[method] == 0 {
			t.Errorf("重试应重新读取 %s，实际调用 %v", method, calls)
		}
	}
	if got := r.countText(locales.CommonLoadFailed()); got != 4 {
		t.Fatalf("故障还在：重试之后错误态应保持，实际 %d 处", got)
	}

	healAll(r)
	r.click(gfx.RoleButton, locales.CommonRetry())
	if got := r.countText(locales.CommonLoadFailed()); got != 0 {
		t.Fatalf("故障排除之后一次重试应全部恢复，还剩 %d 处错误态", got)
	}
	for _, name := range []string{"工作", "阅读", "go", locales.LibraryScopeTrash()} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("恢复之后缺「%s」", name)
		}
	}
}

// 四处错误态的「重试」各自都重新读取三份数据（不是各读各的，也不是只读自己那一份）。
func TestEveryRetryButtonReloadsAllThree(t *testing.T) {
	for n, section := range []string{"合集", "平台", "标签", "回收站"} {
		t.Run(section, func(t *testing.T) {
			r := newRig(t, failAll)
			r.tall() // 让四个按钮都在可见范围里
			var retries []app.AccessNode
			for _, node := range r.session.Access() {
				if node.Role == gfx.RoleButton && node.Label == locales.CommonRetry() {
					retries = append(retries, node)
				}
			}
			sort.Slice(retries, func(i, j int) bool { return retries[i].Bounds.Y < retries[j].Bounds.Y })
			if len(retries) != 4 {
				t.Fatalf("应有 4 个「重试」按钮，实际 %d 个", len(retries))
			}
			before := len(r.svc.Calls())
			x, y := centerOf(retries[n])
			r.clickAt(x, y)
			calls := map[string]int{}
			for _, call := range r.svc.Calls()[before:] {
				calls[call]++
			}
			for _, method := range []string{"Counts", "Collections", "Tags"} {
				if calls[method] == 0 {
					t.Errorf("第 %d 个「重试」（%s）应重新读取 %s，实际调用 %v", n+1, section, method, calls)
				}
			}
		})
	}
}

// 读到过之后的刷新失败只留日志：已有的合集、标签与计数留着，不出错误态。
func TestLaterRefreshFailuresKeepTheDataAndShowNoError(t *testing.T) {
	r := newRig(t)
	failAll(r)
	r.store.Load(r.screen.Ctx)
	r.click(gfx.RoleButton, "收藏 2") // 切范围：又触发一次读取计数
	if got := r.countText(locales.CommonLoadFailed()); got != 0 {
		t.Fatalf("读到过之后的失败不该出错误态，实际 %d 处", got)
	}
	for _, name := range []string{"工作", "阅读", "go", locales.LibraryScopeTrash()} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("刷新失败之后已有的「%s」不该消失", name)
		}
	}
	if len(r.log.entries()) == 0 {
		t.Error("刷新失败应留日志")
	}
}
