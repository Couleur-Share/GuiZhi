package app

import (
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/screens/shell"
)

// waitUntil 轮询一个条件（后台订阅与首次读取跑在别的 goroutine 里），超时按失败处理。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func importTask(id, name string) services.ImportTask {
	return services.ImportTask{
		ID: id, Status: "completed", SourceKind: "url", SourceInput: "https://example.com/" + id, DisplayName: name,
		CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000,
	}
}

func hasTask(r *Root, id string) bool {
	for _, task := range r.importsStore.View().Tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

// Services.Imports 装配后：第一帧启动后台工作（订阅任务推送并读取队列），rail 角标取「运行中 + 排队」，
// 后端推来的任务变更进入列表；关窗口取消订阅，之后的推送不再进列表。
func TestRootMountsImportsModule(t *testing.T) {
	svc := &servicestest.Imports{}
	svc.Set(services.ImportPage{
		Total:   1,
		Counts:  map[string]int64{"all": 1, "completed": 1},
		Entries: []services.ImportTask{importTask("t1", "示例页面")},
	}, nil)
	svc.SetQueueState(services.ImportQueueState{RunningCount: 1, PendingCount: 2, Concurrency: 2})

	f := openRoot(t, func(o *Options) {
		o.Services.Imports = svc
		o.Page, o.Module = shell.PageHome, shell.ModuleImports
	})

	waitUntil(t, "首次读取队列", func() bool { return len(svc.Queries()) > 0 })
	waitUntil(t, "rail 角标取到进行中任务数", func() bool {
		f.frames(t, 1)
		return f.root.Window().ActiveImports == 3
	})
	waitUntil(t, "工作区显示读到的任务", func() bool {
		f.frames(t, 1)
		return f.session.HasText("示例页面")
	})

	// 订阅在位：后端推来一条新任务，它进入列表并出现在画面上。
	svc.Emit(importTask("t2", "推送来的页面"))
	waitUntil(t, "推送的任务进入列表", func() bool { return hasTask(f.root, "t2") })
	waitUntil(t, "推送的任务出现在画面上", func() bool {
		f.frames(t, 1)
		return f.session.HasText("推送来的页面")
	})

	// 关窗口取消订阅：之后的推送不再进入列表。
	f.root.Close()
	svc.Emit(importTask("t3", "关闭之后的推送"))
	if hasTask(f.root, "t3") {
		t.Error("关闭后仍在接收采集队列的推送")
	}
}

// 没有装配 Imports 的 Root（自动化实例）不挂采集模块，也不启动后台订阅；Close 不崩。
func TestRootWithoutImportsService(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Page, o.Module = shell.PageHome, shell.ModuleImports })
	if f.root.Window().ActiveImports != 0 {
		t.Errorf("没有采集服务时角标应为 0，得 %d", f.root.Window().ActiveImports)
	}
	f.root.Close()
}
