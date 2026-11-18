package imports

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
)

// 队列行的窗口化（台账 B-028）在页面层的守卫：行数多的队列第一次读取之后停在顶部、滚轮能把它滚走，
// 声明的行数远小于任务数。这几条是 TestFrameBudget 滚动读数的前置假设，单独成测试，不依赖 VIVA_FRAME_BUDGET。

func windowTasks(n int) []services.ImportTask {
	tasks := make([]services.ImportTask, n)
	for i := range tasks {
		tasks[i] = mkTask(fmt.Sprintf("task-%03d", i), func(task *services.ImportTask) {
			task.Status = services.ImportStatusCompleted
			task.DisplayName = fmt.Sprintf("第 %d 条任务", i)
		})
	}
	return tasks
}

func (r *rig) wheelQueue(dy float32) {
	r.t.Helper()
	if err := r.session.Scroll(gfx.ScrollEvent{X: 760, Y: 450, DeltaY: dy, Precise: true}); err != nil {
		r.t.Fatalf("滚动失败：%v", err)
	}
	r.frames(2)
}

// titlesInView 是画面里看得见的任务标题（外框与窗口相交）：序号 → 上沿。窗口外没有声明的行不在其中。
func (r *rig) titlesInView() map[int]float32 {
	r.t.Helper()
	out := map[int]float32{}
	for _, text := range r.session.Snapshot().Texts {
		var n int
		if _, err := fmt.Sscanf(text.Value, "第 %d 条任务", &n); err != nil {
			continue
		}
		b := text.Bounds
		if b.Y+b.Height <= 0 || b.Y >= 800 {
			continue
		}
		out[n] = b.Y
	}
	return out
}

// 读取之前的「读取中」状态内容很短（AtEnd 因没有可滚范围而成立），读到任务之后列表必须停在顶部：
// 窗口化的末端钉住逻辑曾经把这种「短」当成「滚到了底」，列表一出来就落在最后几行。
func TestQueueOpensAtTopAfterFirstLoad(t *testing.T) {
	r := loaded(t, pageOf(windowTasks(50)...))
	seen := r.titlesInView()
	if _, ok := seen[0]; !ok {
		t.Fatalf("第一次读取之后顶部看不到第一条任务：列表落在了别处（看得见的序号 %v）", seen)
	}
	if _, ok := seen[49]; ok {
		t.Fatalf("第一次读取之后最后一条任务就在可见范围里：列表被拽到了底部（%v）", seen)
	}
}

// 滚轮能滚动窗口化的队列：向下 800 逻辑像素之后第一条任务离开，往回滚能回来，且位置回到原处。
func TestQueueScrollsWithTheWheel(t *testing.T) {
	r := loaded(t, pageOf(windowTasks(50)...))
	top, ok := r.titlesInView()[0]
	if !ok {
		t.Fatalf("顶部看不到第一条任务")
	}
	for i := 0; i < 20; i++ {
		r.wheelQueue(40)
	}
	seen := r.titlesInView()
	if y, ok := seen[0]; ok {
		t.Fatalf("向下滚了 800 px，第一条任务仍在可见范围里（y=%.0f，原 y=%.0f）", y, top)
	}
	if len(seen) == 0 {
		t.Fatalf("向下滚了 800 px 之后一条任务都看不到：窗口没有跟上视口")
	}
	for i := 0; i < 20; i++ {
		r.wheelQueue(-40)
	}
	back, ok := r.titlesInView()[0]
	if !ok || back != top {
		t.Fatalf("滚回来之后第一条任务不在原处：回来 y=%.1f（看得见 %v），原 y=%.1f", back, ok, top)
	}
}

// 声明的行数只覆盖视口与上下各几行：500 个任务的画面里只有十几行，不是 500 行。
func TestQueueDeclaresOnlyTheWindow(t *testing.T) {
	r := loaded(t, pageOf(windowTasks(500)...))
	declared := 0
	for _, text := range r.session.Snapshot().Texts {
		var n int
		if _, err := fmt.Sscanf(text.Value, "第 %d 条任务", &n); err == nil {
			declared++
		}
	}
	if declared == 0 || declared > 30 {
		t.Fatalf("500 个任务只该声明视口附近的行（预期十几行），实际 %d 行", declared)
	}
}
