package capture

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestCaptureFrameBudget 读出快速采集对话框的帧耗时（口径同 viva/screens/inbox 的 TestInboxFrameBudget）。
//
// 两个场景：
//   - capture_empty：刚打开的空对话框（对照组）；
//   - capture_files_tag_suggestions：草稿里是一段分享口令（混合态，状态栏带改判钮）、已选 12 个长路径文件（文件列表）、
//     已加 2 个标签，标签输入框里输入了 go，已有标签建议浮层（6 项）打开——对话框里最重的一帧。
//
// 量三个动作：稳态帧（画面不变，连出 300 帧）、悬停一步（指针在「选择文件」钮与第一行文件的「移除」钮之间来回，每次移动都改变悬停态）、
// 栅格化加回读（上界）；再给出打开后的 Go 堆增量（含无头会话）。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/capture -run TestCaptureFrameBudget -v -count=1。
func TestCaptureFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("capture_empty", func(t *testing.T) { measureCaptureFrames(t, "capture_empty", false) })
	t.Run("capture_files_tag_suggestions", func(t *testing.T) { measureCaptureFrames(t, "capture_files_tag_suggestions", true) })
}

const budgetFileCount = 12

// budgetPaths 造 12 个长路径（需要省略号截断的那种）。
func budgetPaths() []string {
	paths := make([]string, 0, budgetFileCount)
	for i := 0; i < budgetFileCount; i++ {
		paths = append(paths, fmt.Sprintf(`C:\Users\someone\Documents\资料整理\2026年第四季度\会议纪要与调研素材\素材 %02d - 一个文件名偏长的音视频或文档文件.md`, i))
	}
	return paths
}

// measureCaptureFrames 在对话框里量帧；loaded 为真时摆满文件、标签与建议浮层。
func measureCaptureFrames(t *testing.T, name string, loaded bool) {
	heapBefore := frametime.HeapInUse()
	r := opened(t)
	if loaded {
		r.draftWith(douyinShareText)
		r.imports.SetFiles(budgetPaths())
		r.click(gfx.RoleButton, locales.CaptureChooseFiles())
		r.focusTags()
		for _, tag := range []string{"资料", "待读"} {
			r.typeText(tag)
			r.keys("enter")
		}
		r.typeText("go")
		r.frames(2)

		// 夹具核对：混合态改判钮、12 行文件、2 个标签小块、6 项建议都在。
		if !r.has(gfx.RoleButton, locales.CaptureSwitchToText()) {
			t.Fatalf("夹具应是混合态（有改判钮）；画面上有 %q", r.session.Texts())
		}
		if got := r.count(gfx.RoleButton, locales.CommonRemove()); got != budgetFileCount {
			t.Fatalf("夹具应有 %d 行文件，实际 %d", budgetFileCount, got)
		}
		if !r.has(gfx.RoleButton, locales.LibraryRemoveTag("资料")) || !r.has(gfx.RoleButton, locales.LibraryRemoveTag("待读")) {
			t.Fatalf("夹具应有两个标签小块；画面上有 %q", r.session.Texts())
		}
		if got := len(r.optionNames()); got != maxSuggestions {
			t.Fatalf("夹具应有 %d 项标签建议，实际 %d：%q", maxSuggestions, got, r.optionNames())
		}
	}
	t.Logf("%s 打开后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	// 稳态帧：画面不变，连出 300 帧。
	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	// 悬停一步：指针在「选择文件」钮与第一个有悬停反馈的控件之间来回（空对话框里是「空白笔记」钮，摆满时是第一行文件的「移除」）。
	pick, ok := r.node(gfx.RoleButton, locales.CaptureChooseFiles())
	if !ok {
		t.Fatal("找不到「选择文件」钮")
	}
	otherName := locales.CaptureBlankNote()
	if loaded {
		otherName = locales.CommonRemove()
	}
	other, ok := r.node(gfx.RoleButton, otherName)
	if !ok {
		t.Fatalf("找不到悬停目标「%s」", otherName)
	}
	ax, ay := pick.Bounds.X+pick.Bounds.Width/2, pick.Bounds.Y+pick.Bounds.Height/2
	bx, by := other.Bounds.X+other.Bounds.Width/2, other.Bounds.Y+other.Bounds.Height/2
	if ax == bx && ay == by {
		t.Fatal("两个悬停点重合，悬停读数无效")
	}
	hover, err := frametime.Measure(200, func(i int) error {
		x, y := ax, ay
		if i%2 == 1 {
			x, y = bx, by
		}
		if err := r.session.Move(x, y); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("悬停帧失败：%v", err)
	}
	t.Logf("%s 悬停一步（移动指针+出帧）：%s", name, hover)

	// 栅格化加回读：真窗口里绘制成本的上界。
	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}
