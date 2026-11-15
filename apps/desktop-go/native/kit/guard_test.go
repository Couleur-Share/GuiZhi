package kit

import (
	"errors"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/locales"
)

// recordLogger 记下写给日志的条目。
type recordLogger struct {
	mu      sync.Mutex
	entries [][3]string
}

func (r *recordLogger) AppError(scope, action, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, [3]string{scope, action, message})
}

func (r *recordLogger) all() [][3]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][3]string(nil), r.entries...)
}

func TestGuardRunSuccess(t *testing.T) {
	log := &recordLogger{}
	n := NewNotifier(nil)
	g := Guard{Notify: n, Log: log}
	ran := false
	if !g.Run("批量更新", func() error { ran = true; return nil }) || !ran {
		t.Fatal("成功的操作应返回 true 并执行")
	}
	if len(log.all()) != 0 || n.queueLen() != 0 {
		t.Error("成功不该写日志或弹提示")
	}
}

func TestGuardRunFailureLogsAndToasts(t *testing.T) {
	log := &recordLogger{}
	n := NewNotifier(nil)
	g := Guard{Notify: n, Log: log}
	tr := newToastTester(t, lightLook(), n)

	if g.Run("移到回收站", func() error { return errors.New("SQLITE_BUSY: database is locked") }) {
		t.Fatal("失败的操作应返回 false：调用方据此决定要不要弹成功提示")
	}
	got := log.all()
	if len(got) != 1 || got[0] != [3]string{"native", "移到回收站", "SQLITE_BUSY: database is locked"} {
		t.Fatalf("日志条目不对：%v", got)
	}
	tr.Frame()
	if !tr.HasText(locales.CommonOperationFailed("移到回收站")) {
		t.Fatalf("应弹出「移到回收站失败」，文字：%v", tr.Texts())
	}
	if tr.HasText("SQLITE_BUSY") {
		t.Error("原因应折叠在「查看详情」后面")
	}
	_ = tr.Click(locales.CommonViewDetail())
	tr.Frame()
	if !tr.HasText("SQLITE_BUSY: database is locked") {
		t.Error("展开后应能读到完整原因")
	}
}

func TestGuardRunRecoversPanic(t *testing.T) {
	log := &recordLogger{}
	n := NewNotifier(nil)
	g := Guard{Notify: n, Log: log}
	if g.Run("导入", func() error { panic("解析器崩了") }) {
		t.Fatal("panic 应被当作失败")
	}
	got := log.all()
	if len(got) != 1 || got[0][2] != "panic: 解析器崩了" {
		t.Fatalf("panic 应记入日志，得到 %v", got)
	}
	if n.queueLen() != 1 {
		t.Error("panic 也应弹出错误提示")
	}
}

func TestGuardWorksWithoutNotifierOrLogger(t *testing.T) {
	// 缺哪一路就少哪一路，不能因为 nil 而崩
	if (Guard{}).Run("x", func() error { return errors.New("boom") }) {
		t.Error("应返回 false")
	}
	(Guard{}).Background("x", errors.New("boom"))
	onlyLog := &recordLogger{}
	if (Guard{Log: onlyLog}).Run("x", func() error { return errors.New("boom") }) || len(onlyLog.all()) != 1 {
		t.Error("只有日志时仍应记录")
	}
}

func TestGuardBackgroundOnlyLogs(t *testing.T) {
	log := &recordLogger{}
	n := NewNotifier(nil)
	g := Guard{Notify: n, Log: log}
	g.Background("刷新任务列表", nil)
	if len(log.all()) != 0 {
		t.Error("nil 错误不记日志")
	}
	g.Background("刷新任务列表", errors.New("连接已断开"))
	if got := log.all(); len(got) != 1 || got[0] != [3]string{"native", "刷新任务列表", "连接已断开"} {
		t.Errorf("后台失败应记日志：%v", got)
	}
	if n.queueLen() != 0 {
		t.Error("后台失败不该弹提示：用户没做任何操作")
	}
}

func TestGuardGoReportsResult(t *testing.T) {
	log := &recordLogger{}
	g := Guard{Log: log}
	done := make(chan bool, 2)
	g.Go("a", func() error { return nil }, func(ok bool) { done <- ok })
	g.Go("b", func() error { return errors.New("x") }, func(ok bool) { done <- ok })
	results := map[bool]int{}
	for i := 0; i < 2; i++ {
		select {
		case ok := <-done:
			results[ok]++
		case <-time.After(5 * time.Second):
			t.Fatal("后台操作没有回调")
		}
	}
	if results[true] != 1 || results[false] != 1 {
		t.Errorf("应一成功一失败，得到 %v", results)
	}
}
