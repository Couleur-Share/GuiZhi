package themed

import (
	"context"
	"errors"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// waitStable 要求条件在一小段时间内持续成立（用来证明「晚到的后台结果没有改写状态」）。
func waitStable(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !cond() {
			t.Fatal("状态被后台结果改写")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTaskRig(t *testing.T) (*rig, *TaskList) {
	r := newRig(t)
	l := NewTaskList(r.svc, r.guard, func() {})
	l.Spawn = runNow
	t.Cleanup(l.Detach)
	return r, l
}

func TestTaskListLoadsSortedAndEmptyState(t *testing.T) {
	r, l := newTaskRig(t)
	if v := l.View(); !v.Loading {
		t.Fatal("首次读取前处于读取中")
	}
	l.Attach()
	if v := l.View(); v.Loading || !v.Empty() {
		t.Fatalf("没有任务应是空态: %+v", v)
	}
	a := r.svc.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "body", UpdatedAt: 10})
	b := r.svc.PutTask(services.ThemedTask{ItemID: "b", SourceKind: "body", UpdatedAt: 30})
	c := r.svc.PutTask(services.ThemedTask{ItemID: "c", SourceKind: "body", UpdatedAt: 20})
	l.Reload()
	v := l.View()
	if len(v.Tasks) != 3 || v.Tasks[0].ID != b.ID || v.Tasks[1].ID != c.ID || v.Tasks[2].ID != a.ID || v.Empty() {
		t.Fatalf("按最近更新倒序: %+v", v.Tasks)
	}
}

func TestTaskListFailureIsErrorStateAndRetry(t *testing.T) {
	r, l := newTaskRig(t)
	r.svc.Fail(servicestest.ThemedOpListTasks, errors.New("db busy"))
	l.Attach()
	v := l.View()
	if v.Error != "db busy" || v.Loading || v.Empty() {
		t.Fatalf("失败不能渲染成「还没有任务」: %+v", v)
	}
	r.svc.Fail(servicestest.ThemedOpListTasks, nil)
	l.Reload()
	if v := l.View(); v.Error != "" {
		t.Fatalf("重试后恢复: %+v", v)
	}
}

func TestTaskListProgressMergesAndResorts(t *testing.T) {
	r, l := newTaskRig(t)
	old := r.svc.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "body", UpdatedAt: 10, State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign})
	r.svc.PutTask(services.ThemedTask{ItemID: "b", SourceKind: "body", UpdatedAt: 20})
	l.Attach()
	if l.View().Tasks[0].ItemID != "b" {
		t.Fatal("初始顺序")
	}
	r.svc.UpdateTask(old.ID, func(t *services.ThemedTask) { t.Completed = 2 }) // UpdatedAt 前进到最新
	v := l.View()
	if len(v.Tasks) != 2 || v.Tasks[0].ID != old.ID || v.Tasks[0].Completed != 2 {
		t.Fatalf("推送应就地更新并重新排序: %+v", v.Tasks)
	}
	// 新任务（没见过的标识）插到最前
	r.svc.PutPage(reqA, services.ThemedPage{})
	fresh, _ := r.svc.Generate(l.ctx(), reqA, services.ThemedOptions{})
	if v := l.View(); len(v.Tasks) != 3 || v.Tasks[0].ID != fresh.ID {
		t.Fatalf("新任务应出现: %+v", v.Tasks)
	}
	l.Detach()
	r.svc.UpdateTask(fresh.ID, func(t *services.ThemedTask) { t.Completed = 9 })
	if l.View().Tasks[0].Completed == 9 {
		t.Fatal("退订后不再更新")
	}
}

// staleList 返回读取开始时拍下的旧快照，并在返回前卡住：模拟「读取结果到达时，任务已经有了新进展」。
type staleList struct {
	*servicestest.ThemedFake
	snap    []services.ThemedTask
	arrived chan struct{}
	release chan struct{}
}

func (s *staleList) ListTasks(context.Context) ([]services.ThemedTask, error) {
	close(s.arrived)
	<-s.release
	return s.snap, nil
}

func TestTaskListProgressDuringLoadIsNotLost(t *testing.T) {
	fake := &servicestest.ThemedFake{}
	task := fake.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "body", State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign})
	svc := &staleList{ThemedFake: fake, snap: []services.ThemedTask{task}, arrived: make(chan struct{}), release: make(chan struct{})}
	r := newRig(t)
	l := NewTaskList(svc, r.guard, func() {})
	t.Cleanup(l.Detach)
	l.Spawn = func(fn func()) { go fn() }
	l.Attach()
	<-svc.arrived
	// 读取卡住期间任务完成并推送：随后到达的读取结果是旧快照，不能把进展盖掉
	fake.UpdateTask(task.ID, func(t *services.ThemedTask) {
		t.State, t.Stage = services.ThemedTaskCompleted, services.ThemedStageDone
	})
	close(svc.release)
	waitFor(t, "读取完成", func() bool { return !l.View().Loading })
	if got := l.View().Tasks[0]; got.State != services.ThemedTaskCompleted {
		t.Fatalf("读取期间的新进展要并回去: %+v", got)
	}
}
func TestTaskListCancelResumeAndBusy(t *testing.T) {
	r, l := newTaskRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{})
	l.Attach()
	task, _ := r.svc.Generate(l.ctx(), reqA, services.ThemedOptions{})
	if !l.Cancel(task.ID) {
		t.Fatal("停止应成功")
	}
	if got := l.View().Tasks[0]; got.State != services.ThemedTaskCancelled || l.View().IsBusy(task.ID) {
		t.Fatalf("%+v", got)
	}
	if !l.Resume(task.ID) {
		t.Fatal("继续应成功")
	}
	// 活动任务不能「继续」：不发请求
	n := len(r.svc.Calls().Resumes)
	if l.Resume(task.ID) || len(r.svc.Calls().Resumes) != n {
		t.Fatal("活动任务没有继续按钮，也不应发请求")
	}
	if l.Resume("missing") || l.Cancel("") {
		t.Fatal("未知任务什么也不做")
	}
}

func TestTaskListLegacyTaskCannotResume(t *testing.T) {
	r, l := newTaskRig(t)
	task := r.svc.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "body", State: services.ThemedTaskFailed, LegacyFormat: true})
	l.Attach()
	if l.Resume(task.ID) || len(r.svc.Calls().Resumes) != 0 {
		t.Fatal("旧格式任务不能继续")
	}
}

func TestTaskListActionFailureToastsReasonAndBusyReleased(t *testing.T) {
	r, l := newTaskRig(t)
	task := r.svc.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "body", State: services.ThemedTaskFailed})
	l.Attach()
	r.svc.Fail(servicestest.ThemedOpResume, errors.New("no text model"))
	if l.Resume(task.ID) {
		t.Fatal("失败不能报成功")
	}
	q := r.lastToast()
	if q.Kind != kit.ToastError || q.Detail != "no text model" {
		t.Fatalf("要弹带原因的错误提示: %+v", q)
	}
	if l.View().IsBusy(task.ID) {
		t.Fatal("失败后要能重试")
	}
	r.svc.Fail(servicestest.ThemedOpResume, nil)
	if !l.Resume(task.ID) {
		t.Fatal("重试应成功")
	}
}

func TestTaskListSameTaskActionsDoNotOverlap(t *testing.T) {
	r, l := newTaskRig(t)
	task := r.svc.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "body", State: services.ThemedTaskFailed})
	l.Attach()
	release, arrived := r.svc.Hold(servicestest.ThemedOpResume)
	done := make(chan bool, 1)
	go func() { done <- l.Resume(task.ID) }()
	<-arrived
	if !l.View().IsBusy(task.ID) || l.Resume(task.ID) {
		t.Fatal("同一任务的操作进行中，第二次点击应被忽略")
	}
	release()
	if !<-done {
		t.Fatal("第一次应成功")
	}
}

func TestTaskListOpenHook(t *testing.T) {
	r, l := newTaskRig(t)
	task := r.svc.PutTask(services.ThemedTask{ItemID: "a", SourceKind: "summary"})
	l.Attach()
	if l.Open(task.ID) {
		t.Fatal("没有挂钩时不执行")
	}
	var got services.ThemedTask
	l.OnOpen = func(t services.ThemedTask) { got = t }
	if !l.Open(task.ID) || got.ItemID != "a" || got.SourceKind != "summary" {
		t.Fatalf("应带上内容与来源: %+v", got)
	}
	if l.Open("missing") {
		t.Fatal("未知任务不打开")
	}
}

func TestTaskFilter(t *testing.T) {
	tasks := []services.ThemedTask{
		{ID: "q", State: services.ThemedTaskQueued},
		{ID: "r", State: services.ThemedTaskRunning},
		{ID: "f", State: services.ThemedTaskFailed},
		{ID: "lf", State: services.ThemedTaskFailed, LegacyFormat: true},
		{ID: "c", State: services.ThemedTaskCompleted},
		{ID: "p", State: services.ThemedTaskPartial},
	}
	ids := func(f TaskFilter) (out []string) {
		for _, x := range FilterTasks(tasks, f) {
			out = append(out, x.ID)
		}
		return out
	}
	if got := ids(FilterAll); len(got) != 6 {
		t.Fatalf("%v", got)
	}
	if got := ids(FilterActive); len(got) != 2 || got[0] != "q" || got[1] != "r" {
		t.Fatalf("%v", got)
	}
	if got := ids(FilterUnfinished); len(got) != 2 || got[0] != "f" || got[1] != "p" {
		t.Fatalf("旧格式任务不能继续，不算「可继续」: %v", got)
	}
}
