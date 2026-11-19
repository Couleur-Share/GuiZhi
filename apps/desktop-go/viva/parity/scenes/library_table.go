package scenes

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
)

// 知识库列表视图（表格，M2-2）的整窗场景：工具条 + 表格 + 分页条，没有常驻详情栏。
// 与 React 采集器（scripts/parity/scenes/react-library-table-scenes.mjs）一一对应：
// React 一侧点工具条上的「列表视图」切到表格，这里直接把视图模式偏好预置成 list（终态相同；需要先在卡片里点一张卡片的场景才点按钮）。

// tableHoverRow 把指针停在第 row 行（从 0 起）的「类型」列：整行进入悬停态，指针不落在标题按钮或操作按钮上
// （对应 React 采集器的 hoverRow：该行第 5 个单元格的中心）。
// 类型列的横向范围取自它左右两边的拖动把手（「标签」与「类型」的列宽把手各压在列的右缘），纵向取这一行复选框的中心。
func tableHoverRow(row int) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		tags, ok := s.AccessByRole(gfx.RoleSeparator, "调整「标签」列宽")
		if !ok {
			return fmt.Errorf("找不到「标签」列的宽度把手")
		}
		kind, ok := s.AccessByRole(gfx.RoleSeparator, "调整「类型」列宽")
		if !ok {
			return fmt.Errorf("找不到「类型」列的宽度把手")
		}
		var boxes []gfx.Rect
		for _, n := range s.Access() {
			if n.Role == gfx.RoleCheckbox && strings.HasPrefix(n.Label, "选择「") {
				boxes = append(boxes, n.Bounds)
			}
		}
		if row >= len(boxes) {
			return fmt.Errorf("表格里没有第 %d 行（只有 %d 行）", row, len(boxes))
		}
		x := (tags.Bounds.X + tags.Bounds.Width/2 + kind.Bounds.X + kind.Bounds.Width/2) / 2
		y := boxes[row].Y + boxes[row].Height/2
		if err := s.Move(x, y); err != nil {
			return err
		}
		return s.Rest()
	}
}

// tableHoverResizer 把指针停在「标题」列右缘的拖动把手上：中间那条细线显出（group-hover:bg-primary/70）。
func tableHoverResizer(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	n, ok := s.AccessByRole(gfx.RoleSeparator, "调整「标题」列宽")
	if !ok {
		return fmt.Errorf("找不到「标题」列的宽度把手")
	}
	if err := s.Move(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		return err
	}
	if err := s.Rest(); err != nil {
		return err
	}
	// 把手带 title：悬停 320 ms 后出气泡（React 一侧采集时气泡已出）。
	return s.Advance(kit.TooltipDelay + 80*time.Millisecond)
}

// tableOpenColumnMenu 点「列设置」，让菜单展开。
func tableOpenColumnMenu(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := parity.ClickRole(s, gfx.RoleCombobox, "列设置"); err != nil {
		return err
	}
	return s.Rest()
}

// tableToggleColumns 打开「列设置」，依次切换几列的显示（菜单保持打开），最后按 Esc 收起。
func tableToggleColumns(labels ...string) libraryEnvStep {
	return func(s *headless.Session, look *kit.Look, e *libraryEnv) error {
		if err := tableOpenColumnMenu(s, look, e); err != nil {
			return err
		}
		for _, label := range labels {
			if err := parity.ClickRole(s, gfx.RoleMenuItem, label); err != nil {
				return err
			}
			if err := s.Rest(); err != nil {
				return err
			}
		}
		if err := s.Keys("Escape"); err != nil {
			return err
		}
		return s.Rest()
	}
}

// tableScroll 把表格滚到最右、纵向让第 3 行的上缘落在视口上缘下 10px（对应 React 采集器的 scrollTable）：
// 看吸附的操作列与吸附的表头。纵向的量按行的实际位置算（理由见 React 一侧，台账 A-027）。
// 滚轮落在表格区里（表头复选框右下方），横向给一个远大于可滚范围的量，Viva 夹到最大值。
func tableScroll(s *headless.Session, _ *kit.Look, e *libraryEnv) error {
	n, ok := s.AccessByRole(gfx.RoleCheckbox, "选择本页全部")
	if !ok {
		return fmt.Errorf("找不到表头的「选择本页全部」复选框")
	}
	top, ok := e.screen.RowContentTop(2)
	if !ok {
		return fmt.Errorf("表格里没有第 3 行")
	}
	ev := gfx.ScrollEvent{X: n.Bounds.X + 200, Y: n.Bounds.Y + 200, DeltaX: 100000, DeltaY: float32(math.Round(float64(top - 10))), Precise: true}
	if err := s.Scroll(ev); err != nil {
		return err
	}
	return s.Rest()
}

// tableViewButton 点工具条上的视图切换钮「列表视图」（卡片视图里选中一张卡片之后切到表格用）。
func tableViewButton(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, "列表视图"); err != nil {
		return err
	}
	return s.Rest()
}

func init() {
	table := libraryOptions{variant: "data", list: true, view: "list"}
	reg := func(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) {
		registry[id] = libraryEnvScene(id, opt, steps, mode, waivers...)
	}
	base := tableBaseWaivers()
	reg("app.window.library.table", table, nil, parkAll, base...)
	reg("app.window.library.table-hover", table, tableHoverRow(1), parkNone, base...)
	// 先在卡片视图里点一张卡片（它成为选中行），再切到表格：该行是「当前行」。
	reg("app.window.library.table-active", libraryOptions{variant: "data", list: true},
		listChain(listClickCard(cardPinned), tableViewButton), parkAll, base...)
	reg("app.window.library.table-resizer", table, tableHoverResizer, parkNone, base...)
	// 菜单打开：标题「列设置」带字距（A-013），菜单里的文字在浮层里（A-009）。
	menu := append(append(append([]parity.Waiver{}, base...), headingTrackingWaivers("列设置")...), overlayInkWaiver)
	reg("app.window.library.table-columns-open", table, tableOpenColumnMenu, parkKeepFocus, menu...)
	reg("app.window.library.table-columns", table, tableToggleColumns("摘要", "合集", "状态", "创建时间"), parkAll, base...)
	reg("app.window.library.table-scrolled", table, tableScroll, parkAll, base...)
	reg("app.window.library.table-trash", table, listApplyFacets("回收站"), parkAll, base...)
	reg("app.window.library.table-trash-empty", libraryOptions{variant: "empty", list: true, view: "list"}, listApplyFacets("回收站"), parkAll)
	reg("app.window.library.table-empty", libraryOptions{variant: "empty", list: true, view: "list"}, nil, parkAll)
	reg("app.window.library.table-loading", libraryOptions{variant: "data", list: true, view: "list", fault: faultHold}, listWaitSpinner, parkAll)
	reg("app.window.library.table-error", libraryOptions{variant: "data", list: true, view: "list", fault: faultFail}, nil, parkAll)
	reg("app.window.library.table-page2", table, listChain(listPickPageSize("10"), listClickButton("下一页")), parkAll, base...)
}
