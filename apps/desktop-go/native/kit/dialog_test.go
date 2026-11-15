package kit

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/locales"
)

// newDialogTester 起一个带「页面按钮」的窗口，再画 build 里的对话框与提示视口。
func newDialogTester(t *testing.T, l *Look, n *Notifier, build func(c *ui.Context)) *ui.Tester {
	t.Helper()
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		ui.Column(c).Fill().Background(l.T.Background).Padding(24).Children(func() {
			ui.ButtonBase(c).Label("页面按钮").Padding(8, 16).Children(func() {
				ui.Text(c, "页面按钮")
			})
		})
		build(c)
		if n != nil {
			ToastViewport(c, l, n)
		}
	}, 900, 500)
	tr.Frame()
	return tr
}

// waitUntil 轮询到条件成立；后台 goroutine 的收尾要靠它等。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

func TestConfirmRendersAndNamesItself(t *testing.T) {
	l := lightLook()
	d := NewConfirm(nil)
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	if tr.HasText("删除笔记") {
		t.Fatal("没打开时不该画对话框")
	}
	d.Open(ConfirmOptions{Title: "删除笔记", Message: "删除后可在回收站找回。", Destructive: true})
	tr.Frame()

	info, ok := tr.AccessByRole(ui.RoleAlertDialog, "删除笔记")
	if !ok {
		t.Fatalf("应有名为「删除笔记」的 alertdialog，文字：%v", tr.Texts())
	}
	if info.Description != "删除后可在回收站找回。" {
		t.Errorf("说明 = %q", info.Description)
	}
	for _, a := range tr.AccessAll() {
		if a.Role == ui.RoleButton && a.Name == "" {
			t.Errorf("%v 处有个没有名字的按钮", a.Bounds)
		}
	}
	if !tr.HasText(locales.CommonCancel()) || !tr.HasText(locales.CommonConfirm()) {
		t.Errorf("缺省按钮文字应是「取消」「确认」：%v", tr.Texts())
	}
}

func TestConfirmInitialFocusIsCancel(t *testing.T) {
	l := lightLook()
	d := NewConfirm(nil)
	d.Open(ConfirmOptions{Message: "确定吗？", OnConfirm: func() {}})
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	if !tr.Focused(locales.CommonCancel()) {
		t.Error("初始焦点应在「取消」：防止一路回车误删")
	}
}

func TestConfirmEnterConfirmsFromAnywhere(t *testing.T) {
	l := lightLook()
	d := NewConfirm(nil)
	var confirmed atomic.Int32
	d.Open(ConfirmOptions{Message: "确定吗？", OnConfirm: func() { confirmed.Add(1) }})
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })

	// 焦点在「取消」上按 Enter：渲染层是确认（文档级监听先于按钮），不是点取消
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	if confirmed.Load() != 1 {
		t.Fatalf("Enter 应确认，确认回调次数 = %d", confirmed.Load())
	}
	if d.Visible() {
		t.Error("确认后对话框应关闭")
	}
}

func TestConfirmEscapeAndBackdropCancel(t *testing.T) {
	l := lightLook()
	for name, dismiss := range map[string]func(tr *ui.Tester){
		"Esc": func(tr *ui.Tester) { tr.Key(0, ui.KeyEscape) },
		"点遮罩": func(tr *ui.Tester) { tr.ClickAt(8, 8) },
		"取消钮": func(tr *ui.Tester) { _ = tr.Click(locales.CommonCancel()) },
	} {
		d := NewConfirm(nil)
		var confirmed atomic.Int32
		d.Open(ConfirmOptions{Message: "确定吗？", OnConfirm: func() { confirmed.Add(1) }})
		tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
		dismiss(tr)
		tr.Frame()
		if d.Visible() {
			t.Errorf("%s：应关闭对话框", name)
		}
		if confirmed.Load() != 0 {
			t.Errorf("%s：取消不能触发确认", name)
		}
	}
}

func TestConfirmButtonConfirms(t *testing.T) {
	l := lightLook()
	d := NewConfirm(nil)
	var confirmed atomic.Int32
	d.Open(ConfirmOptions{Message: "确定吗？", ConfirmText: "清空", CancelText: "再想想", OnConfirm: func() { confirmed.Add(1) }})
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	if !tr.HasText("清空") || !tr.HasText("再想想") {
		t.Fatalf("应使用自定义按钮文字：%v", tr.Texts())
	}
	if err := tr.Click("清空"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if confirmed.Load() != 1 || d.Visible() {
		t.Errorf("点确认钮：确认 %d 次，打开 %v", confirmed.Load(), d.Visible())
	}
}

func TestConfirmRestoresFocusToWhereItCame(t *testing.T) {
	l := lightLook()
	d := NewConfirm(nil)
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	if err := tr.Click("页面按钮"); err != nil {
		t.Fatal(err)
	}
	if !tr.Focused("页面按钮") {
		t.Skip("点击没有让页面按钮取得焦点，无从比较")
	}
	d.Open(ConfirmOptions{Message: "确定吗？", OnConfirm: func() {}})
	tr.Frame()
	if tr.Focused("页面按钮") {
		t.Fatal("对话框打开后焦点应进到对话框里")
	}
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	tr.Frame() // 对话框的元素在它消失后的那一帧才清掉，焦点随后交还
	if !tr.Focused("页面按钮") {
		t.Error("关闭后焦点应回到打开前的元素：键盘用户不能被甩到窗口开头")
	}
}

func TestConfirmActionLoadsFailsAndRetries(t *testing.T) {
	l := lightLook()
	n := NewNotifier(nil)
	log := &recordLogger{}
	d := NewConfirm(nil)
	var calls atomic.Int32
	var finished atomic.Bool
	release := make(chan struct{})
	d.Open(ConfirmOptions{
		Title: "删除笔记", Message: "确定删除吗？", Destructive: true,
		ActionName: "删除笔记", Guard: Guard{Notify: n, Log: log},
		Action: func() error {
			k := calls.Add(1)
			<-release
			if k == 1 {
				return errors.New("磁盘已满")
			}
			return nil
		},
		OnDone: func() { finished.Store(true) },
	})
	tr := newDialogTester(t, l, n, func(c *ui.Context) { d.View(c, l) })

	// 第一次：确认 → 加载态
	if err := tr.Click(locales.CommonConfirm()); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	waitUntil(t, "动作开始", func() bool { return calls.Load() == 1 })
	tr.Frame()
	if a, ok := tr.Access(locales.CommonConfirm()); !ok || !a.Disabled {
		t.Errorf("加载态下确认钮应禁用：%+v ok=%v", a, ok)
	}
	if a, _ := tr.Access(locales.CommonCancel()); !a.Disabled {
		t.Error("加载态下取消钮也应禁用")
	}
	// 加载态忽略 Esc、点遮罩、Enter
	tr.Key(0, ui.KeyEscape)
	tr.ClickAt(8, 8)
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	if !d.Visible() || calls.Load() != 1 {
		t.Fatalf("加载态下对话框应保持打开且不重复执行：打开 %v，执行 %d 次", d.Visible(), calls.Load())
	}

	// 动作失败：对话框保持打开，原因进提示与日志
	release <- struct{}{}
	waitUntil(t, "第一次动作结束", func() bool { return !d.b.isRunning() })
	tr.Frame()
	if !d.Visible() {
		t.Fatal("失败后对话框应保持打开，让用户重试或取消")
	}
	if !tr.HasText(locales.CommonOperationFailed("删除笔记")) {
		t.Errorf("应弹出失败提示：%v", tr.Texts())
	}
	if got := log.all(); len(got) != 1 || got[0][2] != "磁盘已满" {
		t.Errorf("失败原因应进日志：%v", got)
	}
	if a, _ := tr.Access(locales.CommonConfirm()); a.Disabled {
		t.Error("失败后确认钮应恢复可点")
	}

	// 重试成功：关闭并回调
	if err := tr.Click(locales.CommonConfirm()); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	waitUntil(t, "第二次动作开始", func() bool { return calls.Load() == 2 })
	release <- struct{}{}
	waitUntil(t, "第二次动作结束", func() bool { return !d.b.isRunning() })
	tr.Frame()
	if d.Visible() || !finished.Load() {
		t.Errorf("成功后应关闭并回调 OnDone：打开 %v，回调 %v", d.Visible(), finished.Load())
	}
}

func TestConfirmActionRunsOncePerConfirmation(t *testing.T) {
	l := lightLook()
	d := NewConfirm(nil)
	var calls atomic.Int32
	release := make(chan struct{})
	d.Open(ConfirmOptions{
		Message: "确定吗？", ActionName: "处理",
		Action: func() error { calls.Add(1); <-release; return nil },
	})
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	// 同一帧里连按两次 Enter：只能执行一次
	tr.Key(0, ui.KeyEnter)
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	waitUntil(t, "动作开始", func() bool { return calls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Errorf("连按确认执行了 %d 次", calls.Load())
	}
	close(release)
	waitUntil(t, "动作结束", func() bool { return !d.b.isRunning() })
}

func TestConfirmActionWakesUIWhenDone(t *testing.T) {
	var wakes atomic.Int32
	l := lightLook()
	d := NewConfirm(func() { wakes.Add(1) })
	d.Open(ConfirmOptions{Message: "确定吗？", Action: func() error { return nil }})
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	waitUntil(t, "后台结束后请求重绘", func() bool { return wakes.Load() > 0 })
}

func TestUnsavedDialogChoices(t *testing.T) {
	l := lightLook()
	type counters struct{ saved, discarded, cancelled atomic.Int32 }
	open := func(cs *counters, save func() error) (*Unsaved, *ui.Tester) {
		d := NewUnsaved(nil)
		d.Open(UnsavedOptions{
			Save: save, ActionName: "保存笔记",
			OnSaved:   func() { cs.saved.Add(1) },
			OnDiscard: func() { cs.discarded.Add(1) },
			OnCancel:  func() { cs.cancelled.Add(1) },
		})
		return d, newDialogTester(t, l, nil, func(c *ui.Context) { d.View(c, l) })
	}

	t.Run("初始焦点在保存", func(t *testing.T) {
		var cs counters
		_, tr := open(&cs, func() error { return nil })
		if !tr.Focused(locales.CommonSave()) {
			t.Error("初始焦点应在「保存」")
		}
		if _, ok := tr.AccessByRole(ui.RoleAlertDialog, locales.CommonUnsavedChanges()); !ok {
			t.Error("应有名为「未保存的更改」的 alertdialog")
		}
	})
	t.Run("Esc 与取消钮都是取消", func(t *testing.T) {
		for name, act := range map[string]func(tr *ui.Tester){
			"Esc": func(tr *ui.Tester) { tr.Key(0, ui.KeyEscape) },
			"取消钮": func(tr *ui.Tester) { _ = tr.Click(locales.CommonCancel()) },
			"点遮罩": func(tr *ui.Tester) { tr.ClickAt(8, 8) },
		} {
			var cs counters
			d, tr := open(&cs, func() error { return nil })
			act(tr)
			tr.Frame()
			if d.Visible() || cs.cancelled.Load() != 1 || cs.saved.Load()+cs.discarded.Load() != 0 {
				t.Errorf("%s：打开 %v，取消 %d，保存 %d，不保存 %d", name, d.Visible(), cs.cancelled.Load(), cs.saved.Load(), cs.discarded.Load())
			}
		}
	})
	t.Run("不保存", func(t *testing.T) {
		var cs counters
		d, tr := open(&cs, func() error { return errors.New("不该被调用") })
		if err := tr.Click(locales.CommonDiscardChanges()); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
		if d.Visible() || cs.discarded.Load() != 1 || cs.saved.Load() != 0 {
			t.Errorf("打开 %v，不保存 %d，保存 %d", d.Visible(), cs.discarded.Load(), cs.saved.Load())
		}
	})
	t.Run("保存成功后关闭", func(t *testing.T) {
		var cs counters
		d, tr := open(&cs, func() error { return nil })
		if err := tr.Click(locales.CommonSave()); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
		waitUntil(t, "保存结束", func() bool { return !d.b.isRunning() })
		tr.Frame()
		if d.Visible() || cs.saved.Load() != 1 {
			t.Errorf("打开 %v，保存回调 %d", d.Visible(), cs.saved.Load())
		}
	})
	t.Run("Enter 点当前焦点（保存）", func(t *testing.T) {
		var cs counters
		d, tr := open(&cs, func() error { return nil })
		tr.Key(0, ui.KeyEnter)
		tr.Frame()
		waitUntil(t, "保存结束", func() bool { return !d.b.isRunning() })
		tr.Frame()
		if d.Visible() || cs.saved.Load() != 1 {
			t.Errorf("Enter 应触发焦点上的保存：打开 %v，保存回调 %d", d.Visible(), cs.saved.Load())
		}
	})
}

func TestUnsavedSaveFailureKeepsDialogOpen(t *testing.T) {
	l := lightLook()
	n := NewNotifier(nil)
	log := &recordLogger{}
	d := NewUnsaved(nil)
	var saved, discarded atomic.Int32
	d.Open(UnsavedOptions{
		Save:       func() error { return errors.New("数据库被锁定") },
		ActionName: "保存笔记", Guard: Guard{Notify: n, Log: log},
		OnSaved: func() { saved.Add(1) }, OnDiscard: func() { discarded.Add(1) },
	})
	tr := newDialogTester(t, l, n, func(c *ui.Context) { d.View(c, l) })
	if err := tr.Click(locales.CommonSave()); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	waitUntil(t, "保存结束", func() bool { return !d.b.isRunning() })
	tr.Frame()
	if !d.Visible() || saved.Load() != 0 || discarded.Load() != 0 {
		t.Fatalf("保存失败后对话框应保持打开，且不能当作已保存或已放弃：打开 %v，保存 %d，放弃 %d", d.Visible(), saved.Load(), discarded.Load())
	}
	if !tr.HasText(locales.CommonOperationFailed("保存笔记")) {
		t.Errorf("应弹出「保存笔记失败」：%v", tr.Texts())
	}
	if got := log.all(); len(got) != 1 || got[0][1] != "保存笔记" || got[0][2] != "数据库被锁定" {
		t.Errorf("日志条目不对：%v", got)
	}
}

func TestDialogsInDarkModeRender(t *testing.T) {
	// 暗色下也能画出并保持可操作（面板底色是预先合成的不透明色）
	l := darkLook()
	c1, u1 := NewConfirm(nil), NewUnsaved(nil)
	c1.Open(ConfirmOptions{Title: "清空回收站", Message: "此操作无法撤销。", Destructive: true, OnConfirm: func() {}})
	tr := newDialogTester(t, l, nil, func(c *ui.Context) { c1.View(c, l); u1.View(c, l) })
	if !tr.HasText("清空回收站") {
		t.Fatal("暗色下应画出确认框")
	}
	c1.Close()
	u1.Open(UnsavedOptions{})
	tr.Frame()
	if !tr.HasText(locales.CommonUnsavedChanges()) {
		t.Fatal("暗色下应画出未保存对话框")
	}
}
