package settings_test

import (
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/internal/frametime"
)

// TestFrameBudget 读出两个设置页的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
// 口径与 viva/screens/imports 的同名测试相同：Frame() 只记录不栅格化，量的是界面线程的出帧成本；
// 栅格化加回读另量一次（Image()）作为上界。默认跳过：VIVA_FRAME_BUDGET=1 时运行，结果记进开发文档 §8.4。
func TestFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	pages := []struct {
		name string
		open func(t *testing.T) *rig
	}{
		{"general", func(t *testing.T) *rig { return openGeneral(t, newStore()) }},
		{"appearance", func(t *testing.T) *rig {
			r, _ := openAppearance(t, newStore())
			return r
		}},
	}
	for _, pg := range pages {
		t.Run(pg.name, func(t *testing.T) {
			heapBefore := frametime.HeapInUse()
			r := pg.open(t)
			t.Logf("%s 打开页面后 Go 堆增量（含无头会话）：%d KiB", pg.name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

			idle, err := frametime.Measure(300, func(int) error { return r.s.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%s 稳态帧（布局+记录）：%s", pg.name, idle)

			points := [][2]float32{{640, 300}, {640, 380}}
			hover, err := frametime.Measure(200, func(i int) error {
				p := points[i%2]
				if err := r.s.Move(p[0], p[1]); err != nil {
					return err
				}
				return r.s.Frame()
			})
			if err != nil {
				t.Fatalf("悬停帧失败：%v", err)
			}
			t.Logf("%s 悬停帧（移动指针+出帧）：%s", pg.name, hover)

			scrollStep := func(dy float32) error {
				if err := r.s.Scroll(gfx.ScrollEvent{X: 640, Y: 450, DeltaY: dy, Precise: true}); err != nil {
					return err
				}
				return r.s.Frame()
			}
			top := frametime.Fingerprint(r.s.Access())
			for i := 0; i < 20; i++ {
				if err := scrollStep(40); err != nil {
					t.Fatalf("预检滚动失败：%v", err)
				}
			}
			if top == frametime.Fingerprint(r.s.Access()) {
				t.Fatalf("向下滚动 800 px 之后画面没有变化，滚动读数无效（页面不够长，或滚动位置不在滚动区里）")
			}
			for i := 0; i < 20; i++ {
				if err := scrollStep(-40); err != nil {
					t.Fatalf("预检回滚失败：%v", err)
				}
			}
			scroll, err := frametime.Measure(200, func(i int) error {
				if (i/20)%2 == 1 {
					return scrollStep(-40)
				}
				return scrollStep(40)
			})
			if err != nil {
				t.Fatalf("滚动帧失败：%v", err)
			}
			t.Logf("%s 滚动一步（滚动+出帧）：%s", pg.name, scroll)

			raster, err := frametime.Measure(30, func(int) error {
				_, err := r.s.Image()
				return err
			})
			if err != nil {
				t.Fatalf("栅格化失败：%v", err)
			}
			t.Logf("%s 栅格化+回读（Image，上界）：%s", pg.name, raster)
		})
	}
}
