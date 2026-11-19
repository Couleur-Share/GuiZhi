package library

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
)

// TestFrameBudget 读出知识库侧栏的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 侧栏的合集与标签行是窗口化的（kit.FlexColumn.Run，台账 B-035），耗时与行数基本无关；按「合集与标签各有多少」取五档：
// 夹具、基准库副本的规模（40 个合集、120 个标签：`D:\Project\GuiZhi-bench\seed\data\knowledge.db` 的实际数量）、较大、压力、极端。
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）；
// 栅格化加回读另量一次（Image()），作为真窗口里绘制成本的上界。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestFrameBudget -v -count=1。
// 结果记进开发文档 §8.4。滚动读数前先核对滚动确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	cases := []struct {
		name              string
		collections, tags int // 0 表示用夹具
	}{
		{"fixture", 0, 0},
		{"40_collections_120_tags_seed_size", 40, 120},
		{"100_collections_300_tags", 100, 300},
		{"200_collections_500_tags", 200, 500},
		{"1000_collections_5000_tags", 1000, 5000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			heapBefore := frametime.HeapInUse()
			r := newRig(t, func(r *rig) {
				if c.collections == 0 {
					return
				}
				counts, collections, tags := bigLibrary(c.collections, c.tags)
				r.svc.Seed(counts, collections, tags)
			})
			t.Logf("%s 打开侧栏后 Go 堆增量（含无头会话）：%d KiB", c.name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

			// 稳态帧：画面不变，连出 300 帧。
			idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%s 稳态帧（布局+记录）：%s", c.name, idle)

			// 悬停帧：指针在两行之间来回，每次移动都改变悬停态（行尾的计数换成「更多」按钮）。
			points := [][2]float32{{120, 250}, {120, 300}}
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

			// 滚动一步：每步 40 逻辑像素。夹具的侧栏放得下，不滚动；其余两档先预检：向下 20 步（800 px）画面必须变，再原路滚回。
			scrollStep := func(dy float32) error {
				if err := r.session.Scroll(gfx.ScrollEvent{X: 120, Y: 400, DeltaY: dy, Precise: true}); err != nil {
					return err
				}
				return r.session.Frame()
			}
			if c.collections > 0 {
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
			}

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

// bigLibrary 造 collections 个合集、tags 个标签（每个都有条目），平台沿用夹具。
func bigLibrary(collections, tags int) (services.LibraryCounts, []services.LibraryCollection, []services.LibraryTag) {
	counts, _, _ := libraryFixture()
	counts.ByCollection = map[string]int{}
	counts.ByTag = map[string]int{}
	colors := []string{"red", "orange", "green", "blue", "purple", "gray"}
	cols := make([]services.LibraryCollection, collections)
	for i := range cols {
		id := fmt.Sprintf("c%03d", i)
		cols[i] = services.LibraryCollection{ID: id, Name: fmt.Sprintf("合集 %03d：读书笔记与项目资料", i)}
		if i%3 == 0 {
			cols[i].Icon = "📚"
		}
		counts.ByCollection[id] = i%120 + 1
	}
	ts := make([]services.LibraryTag, tags)
	for i := range ts {
		id := fmt.Sprintf("t%03d", i)
		ts[i] = services.LibraryTag{ID: id, Name: fmt.Sprintf("标签 %03d", i), ColorKey: colors[i%len(colors)]}
		counts.ByTag[id] = i%40 + 1
	}
	return counts, cols, ts
}
