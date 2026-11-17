package imports

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
)

// TestFrameBudget 读出采集队列的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）；
// 栅格化加回读另量一次（Image()），作为真窗口里绘制成本的上界。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/imports -run TestFrameBudget -v。
// 结果记进开发文档 §8.4。滚动读数前先核对滚动确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	mixed := []string{
		services.ImportStatusCompleted, services.ImportStatusProcessing, services.ImportStatusFailed,
		services.ImportStatusPending, services.ImportStatusCanceled, services.ImportStatusDuplicate,
	}
	settled := []string{ // 全是终态任务：画面上没有转圈的进度图标
		services.ImportStatusCompleted, services.ImportStatusFailed, services.ImportStatusCanceled, services.ImportStatusDuplicate,
	}
	cases := []struct {
		name     string
		n        int
		statuses []string
	}{
		{"50_rows", 50, mixed},
		{"500_rows", 500, mixed},
		{"50_rows_settled", 50, settled},
	}
	for _, c := range cases {
		n := c.n
		t.Run(c.name, func(t *testing.T) {
			tasks := make([]services.ImportTask, n)
			for i := range tasks {
				status := c.statuses[i%len(c.statuses)]
				tasks[i] = mkTask(fmt.Sprintf("task-%03d", i), func(task *services.ImportTask) {
					task.Status = status
					task.DisplayName = fmt.Sprintf("第 %d 条：手写一个混合检索 RAG 系统并比较三种重排策略的效果", i)
				})
			}
			heapBefore := frametime.HeapInUse()
			r := loaded(t, pageOf(tasks...))
			t.Logf("%s 打开页面后 Go 堆增量（含无头会话）：%d KiB", c.name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

			// 稳态帧：画面不变，连出 300 帧。
			idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%s 稳态帧（布局+记录）：%s", c.name, idle)

			// 悬停帧：指针在两行之间来回，每次移动都改变悬停态（行内动作钮出现与消失）。
			points := [][2]float32{{700, 300}, {700, 380}}
			hover, err := frametime.Measure(200, func(i int) error {
				p := points[i%2]
				if err := r.session.Move(p[0], p[1]); err != nil {
					return err
				}
				return r.session.Frame()
			})
			if err != nil {
				t.Fatalf("悬停帧失败：%v", err)
			}
			t.Logf("%s 悬停帧（移动指针+出帧）：%s", c.name, hover)

			// 滚动一步：每步 40 逻辑像素。先预检：向下 20 步（800 px）画面必须变，再原路滚回。
			scrollStep := func(dy float32) error {
				if err := r.session.Scroll(gfx.ScrollEvent{X: 760, Y: 450, DeltaY: dy, Precise: true}); err != nil {
					return err
				}
				return r.session.Frame()
			}
			top := frametime.Fingerprint(r.session.Access())
			for i := 0; i < 20; i++ {
				if err := scrollStep(40); err != nil {
					t.Fatalf("预检滚动失败：%v", err)
				}
			}
			if top == frametime.Fingerprint(r.session.Access()) {
				t.Fatalf("向下滚动 800 px 之后画面没有变化，滚动读数无效")
			}
			for i := 0; i < 20; i++ {
				if err := scrollStep(-40); err != nil {
					t.Fatalf("预检回滚失败：%v", err)
				}
			}
			// 正式读数：向下 20 步、向上 20 步，共 200 步。
			scroll, err := frametime.Measure(200, func(i int) error {
				if (i/20)%2 == 1 {
					return scrollStep(-40)
				}
				return scrollStep(40)
			})
			if err != nil {
				t.Fatalf("滚动帧失败：%v", err)
			}
			t.Logf("%s 滚动一步（滚动+出帧）：%s", c.name, scroll)

			// 栅格化加回读：真窗口里绘制成本的上界。
			raster, err := frametime.Measure(30, func(int) error {
				_, err := r.session.Image()
				return err
			})
			if err != nil {
				t.Fatalf("栅格化失败：%v", err)
			}
			t.Logf("%s 栅格化+回读（Image，上界）：%s", c.name, raster)
		})
	}
}
