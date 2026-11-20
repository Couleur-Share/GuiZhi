package scenes

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 知识库批量与整理（M2-3）的整窗场景：批量条、结果条、条目菜单、移到合集菜单、标签弹层、彻底删除与清空回收站的确认框。
// 与 React 采集器（scripts/parity/scenes/react-library-bulk-scenes.mjs）一一对应：同样的条目、同样的点击位置与顺序。
// 批量命令由服务假实现就地改条目（规则与页内替换的 execute 一致）；batchFail 登记的条目在批量里失败。

const (
	// 多选用的两张卡片：置顶的第二张与普通的第一张（都是纯中文标题，避开拉丁字母的字偶距放过项）。
	pickA = "产品设计原则：极简与克制"
	pickB = "如何构建本地优先的个人知识库"
	// 回收站里的两张卡片。
	trashA = "回收站：误删的草稿"
	trashB = "回收站：过期的链接"

	// 批量里失败的那一条（pickB）与失败原因：与 React 一侧 FAIL_ID、FAIL_REASON 同值。
	batchFailID     = "item-003"
	batchFailReason = "磁盘写入失败：数据库暂时被占用"
)

// snap 把指针位置向下取整：渲染层里事件坐标（clientX、clientY）是整数（Playwright 点 65.5 时页面读到 65），
// 真实鼠标在 100% 缩放的屏幕上也只落在整数像素。会弹出锚在指针处浮层的点击，两边都点同一个整数点，浮层的锚点才一致；
// 对应 React 采集器里的 Math.floor。
func snap(v float32) float32 { return float32(math.Floor(float64(v))) }

// lazyStep 在运行时才构造步骤：按钮名取当前界面语言的文案。场景在 init 里登记，那时语言还是默认的中文，
// 英文变体要等 -lang en 设定之后才取名（对应 React 采集器里按 PARITY_LANG 取名的 label()）。
func lazyStep(build func() libraryEnvStep) libraryEnvStep {
	return func(s *headless.Session, look *kit.Look, env *libraryEnv) error { return build()(s, look, env) }
}

// clickLabel、clickLabelSnapped、facetLabel 是 listClickButton、listClickButtonSnapped、listApplyFacets 的按语言取名版本。
func clickLabel(label func() string) libraryEnvStep {
	return lazyStep(func() libraryEnvStep { return listClickButton(label()) })
}

func clickLabelSnapped(label func() string) libraryEnvStep {
	return lazyStep(func() libraryEnvStep { return listClickButtonSnapped(label()) })
}

func facetLabel(label func() string) libraryEnvStep {
	return lazyStep(func() libraryEnvStep { return listApplyFacets(label()) })
}

// prefixOf 取带占位的文案在占位之前的部分：「处理中，已核对 {n} 项」→「处理中，已核对 」，英文同理；
// 用来按前缀找「逐条结果（N）」「选择「标题」」这类名字里带动态部分的节点。
func prefixOf(format func(any) string) string {
	s := format("\x00")
	return s[:strings.IndexByte(s, 0)]
}

// listPaneLeft 是列表窗格的左缘（1280 宽下侧栏占到 368）：取行内按钮时，左边侧栏里的同名按钮（合集行的「更多操作」）不算。
const listPaneLeft float32 = 368

// listClickButtonSnapped 点名为 name 的按钮的中心（取整，见 snap），等画面落定：用于点开锚在指针处的浮层。
func listClickButtonSnapped(name string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		node, ok := s.AccessByRole(gfx.RoleButton, name)
		if !ok {
			return fmt.Errorf("找不到 %s「%s」", gfx.RoleButton, name)
		}
		if err := s.Click(snap(node.Bounds.X+node.Bounds.Width/2), snap(node.Bounds.Y+node.Bounds.Height/2)); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listCtrlClickCard 按住 Ctrl 点一张卡片左内边距处：加入或移出多选（对应 React 采集器的 ctrlClickCard）。
func listCtrlClickCard(title string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		b, err := listCard(s, title)
		if err != nil {
			return err
		}
		if err := s.KeyDown("ctrl"); err != nil {
			return err
		}
		if err := s.Click(b.X+listCardAnchor, b.Y+b.Height/2); err != nil {
			return err
		}
		if err := s.KeyUp("ctrl"); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listSelectCards 依次 Ctrl 点几张卡片。
func listSelectCards(titles ...string) libraryEnvStep {
	steps := make([]libraryEnvStep, 0, len(titles))
	for _, title := range titles {
		steps = append(steps, listCtrlClickCard(title))
	}
	return listChain(steps...)
}

// listRightClickCard 右键点一张卡片左内边距处（对应 rightClickCard）。
func listRightClickCard(title string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		b, err := listCard(s, title)
		if err != nil {
			return err
		}
		if err := s.RightClick(snap(b.X+listCardAnchor), snap(b.Y+b.Height/2)); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listOpenDetails 点开结果条里的「逐条结果（N）」折叠区。
func listOpenDetails(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := clickRolePrefix(s, gfx.RoleButton, prefixOf(locales.LibraryWorkflowResults)); err != nil {
		return err
	}
	return s.Rest()
}

// listHoldBatches 让之后的第一次批量卡在服务里（批量进行中），并把后台动作改成异步：卡住的批量不能卡住整个会话。
// 对应 React 采集器 config.batch.holdAfter（第一次批量照常执行，之后的批量永不返回）。
func listHoldBatches(_ *headless.Session, _ *kit.Look, env *libraryEnv) error {
	env.svc.HoldBatch()
	env.store.Spawn = func(fn func()) { go fn() }
	return nil
}

// listWaitProcessing 等结果条换成「处理中，已核对 N 项」：卡住的批量在后台协程里改状态，点击返回时可能还没换。
func listWaitProcessing(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	// 大字号档下这句话在结果条里折行，折行处的空格不留在行尾，所以前缀也去掉末尾空白。
	found, err := waitTextPrefix(s, strings.TrimSpace(prefixOf(locales.LibraryWorkflowProcessing)), 400)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("等不到结果条里的「处理中」")
	}
	return nil
}

// waitTextPrefix 等画面上出现以 prefix 开头的一行文字，最多轮询 attempts 轮（每轮约 7 ms，400 轮约 3 秒）；等不到返回 (false, nil)。
//
// 后台协程改了状态之后，场景里的 Store 没接 invalidate（没有人请求出帧），而 Session.Rest 只在有过渡时才走时钟、不出帧，
// 所以每轮要先 Frame 让新状态上屏。只靠 Rest 的话，协程晚于点击之后的最后一帧改状态就永远等不到，偶发失败（M2-3 收口时撞上）。
func waitTextPrefix(s *headless.Session, prefix string, attempts int) (bool, error) {
	for range attempts {
		if err := s.Frame(); err != nil {
			return false, err
		}
		if err := s.Rest(); err != nil {
			return false, err
		}
		for _, t := range s.Snapshot().Texts {
			if strings.HasPrefix(t.Value, prefix) {
				return true, nil
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false, nil
}

// tableCheckRow 勾选表格里第 row 行（从 0 起）的复选框（对应 checkRow）。
func tableCheckRow(row int) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		boxes := tableRowBoxes(s)
		if row >= len(boxes) {
			return fmt.Errorf("表格里没有第 %d 行（只有 %d 行）", row, len(boxes))
		}
		b := boxes[row]
		if err := s.Click(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
			return err
		}
		return s.Rest()
	}
}

// tableOpenRowMore 点第 row 行内的「更多操作」（对应 openRowMore，取整见 snap）：行内操作钮按行的顺序排在可访问树里；
// 左边侧栏里合集行也有同名的「更多操作」，在列表窗格之外，不算。
func tableOpenRowMore(row int) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		seen := 0
		more := locales.LibraryMoreActions()
		for _, n := range s.Access() {
			if n.Role != gfx.RoleButton || n.Label != more || n.Bounds.X < listPaneLeft {
				continue
			}
			if seen == row {
				if err := s.Click(snap(n.Bounds.X+n.Bounds.Width/2), snap(n.Bounds.Y+n.Bounds.Height/2)); err != nil {
					return err
				}
				return s.Rest()
			}
			seen++
		}
		return fmt.Errorf("表格里没有第 %d 行的「%s」（只有 %d 个）", row, more, seen)
	}
}

// tableOpenTagPicker 右键点表格第 row 行的「类型」列，再选菜单里的「编辑标签」（对应 openTagPicker）。
func tableOpenTagPicker(row int) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		x, y, err := tableRowPoint(s, row)
		if err != nil {
			return err
		}
		if err := s.RightClick(snap(x), snap(y)); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		if err := parity.ClickRole(s, gfx.RoleMenuItem, locales.LibraryEditTags()); err != nil {
			return err
		}
		return s.Rest()
	}
}

// wideTrackingWaivers 登记 A-013 在 tracking-wide（0.025em）小标题上的延伸：标签浮层里的「选择已有标签」，
// Viva 的文字没有字距，绘制宽度窄 字符数 × 0.025em（小、中、大字号档的 text-xs 是 10.5、12、13.5px，即每字 0.2625、0.3、0.3375px）；
// 文字盒宽已按字数补齐（kit.TextTracked），后面的元素位置一致。与 headingTrackingWaivers（tracking-wider，0.05em）同一类，字距减半。
func wideTrackingWaivers(title string) []parity.Waiver {
	return wideTracking(title, 0, "", "tracking-wide 的字距：Viva 没有字距，绘制宽度窄 字符数 × 0.025em（盒宽已补齐，后面的元素位置一致）")
}

// resultsDisclosureWaivers 登记 G-022（比较器的缺口，不是 Viva 的偏差）：比较器的角色映射表（a11y_map.go，哈希守卫）里没有
// <details>/<summary> 导出的 DisclosureTriangle，也没有 <ul>/<li> 导出的 list / listitem，所以「逐条结果」这一块的 L1
// 没法逐节点比较：React 的 DisclosureTriangle 被报为「未映射」，Viva 侧对应的 button 被报为「多出的节点」，是同一件事的两面。
// 只放过这三种角色与这一枚钮；这一块的角色、名称与展开状态由 L4（viva/screens/library/bulk_strip_ui_test.go）兜底。
// 假设性实验：把映射表补成 DisclosureTriangle → button、list / listitem → 忽略，四个批量结果场景的 L1 全部对齐，没有别的差异；
// 映射表的改动要用户追认，追认之前按这里登记放过。报告里「未映射角色」一栏照常列出。
var resultsDisclosureWaivers = []parity.Waiver{
	{Ledger: "G-022", Layer: parity.LayerL1, Kind: "unmapped-role", Subject: "DisclosureTriangle", Note: "比较器的角色映射表没有 <summary> 的 DisclosureTriangle（逐条结果的折叠钮）"},
	{Ledger: "G-022", Layer: parity.LayerL1, Kind: "unmapped-role", Subject: "listitem", Note: "比较器的角色映射表没有 <li> 的 listitem（逐条结果的每一行）"},
	{Ledger: "G-022", Layer: parity.LayerL1, Kind: "unmapped-role", Subject: "list", Note: "比较器的角色映射表没有 <ul> 的 list（逐条结果的列表）"},
	{Ledger: "G-022", Layer: parity.LayerL1, Kind: "extra-node", Subject: "button 「逐条结果（", Note: "Viva 的折叠钮是 button 并带展开状态；React 一侧对应的 DisclosureTriangle 未映射，所以被报为多出的节点"},
	// 英文界面里折叠钮的名字是「Per-item results (N)」（与 library_bulk_waivers_test.go 核对目录，目录改了这里要跟着改）。
	{Ledger: "G-022", Layer: parity.LayerL1, Kind: "extra-node", Subject: "button 「Per-item results (", Note: "同上（英文界面的折叠钮名称）"},
}

func init() {
	list := libraryOptions{variant: "data", list: true}
	failing := libraryOptions{variant: "data", list: true, batchFail: map[string]string{batchFailID: batchFailReason}}
	holding := libraryOptions{variant: "data", list: true, batchFail: map[string]string{batchFailID: batchFailReason}, holdBatch: true}
	table := libraryOptions{variant: "data", list: true, view: "list"}
	// 每个场景都附上英文变体的登记（按 Subject 与变体匹配，中文变体不会命中，见 library_bulk_waivers.go）。
	reg := func(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) {
		registry[id] = libraryEnvScene(id, opt, steps, mode, withEnglishBulk(waivers)...)
	}
	// 确认框是模态：遮罩背后的页面模糊（台账 A-007，ConfirmDialog 的 backdrop-blur-sm 是 4），遮罩之上是名为标题的对话框面板。
	// before 是打开对话框之前把页面带到的状态（选中回收站里的两张卡片、切到回收站）：模糊背景要用这一页的清晰版本合成，
	// 之后指针停到窗口角落（采集 React 时同样，对话框一打开背后就没有悬停）。这些步骤只走界面，不用场景的内部状态。
	regConfirm := func(id string, before, steps libraryEnvStep, title func() string, waivers ...parity.Waiver) {
		spec := confirmBackdrop("")
		spec.PanelNameFn = title
		spec.BaseSteps = func(s *headless.Session, look *kit.Look) error {
			if err := before(s, look, nil); err != nil {
				return err
			}
			return parity.ParkPointer(s)
		}
		registry[id] = withBackdrop(libraryEnvScene(id, list, steps, parkKeepFocus, withEnglishBulk(waivers)...), spec)
	}
	cards := semiboldKerningWaivers()
	results := append(append([]parity.Waiver{}, cards...), resultsDisclosureWaivers...)
	overlay := append([]parity.Waiver{overlayInkWaiver}, cards...)
	base := tableBaseWaivers()
	tableOverlay := append([]parity.Waiver{overlayInkWaiver}, base...)
	// 标签浮层里的小标题「选择已有标签」「AI 建议标签」带字距（A-013；批量浮层里只有前一个）。
	tagHeading := wideTrackingWaivers("选择已有标签")
	overlayTag := append(append([]parity.Waiver{}, overlay...), tagHeading...)
	tableOverlayTag := append(append(append([]parity.Waiver{}, tableOverlay...), tagHeading...), wideTrackingWaivers("AI 建议标签")...)

	selectBoth := listSelectCards(pickA, pickB)
	selectTrash := listChain(facetLabel(locales.LibraryScopeTrash), listSelectCards(trashA, trashB))
	favoriteBoth := listChain(selectBoth, clickLabel(locales.LibraryBulkFavorite))

	reg("app.window.library.list-bulk", list, selectBoth, parkAll, cards...)
	reg("app.window.library.list-bulk-move", list, listChain(selectBoth, clickLabelSnapped(locales.LibraryBulkMoveTitle)), parkKeepFocus, overlay...)
	reg("app.window.library.list-bulk-tag", list, listChain(selectBoth, clickLabelSnapped(locales.LibraryBulkAddTag)), parkKeepFocus, overlayTag...)
	reg("app.window.library.list-bulk-trash", list, selectTrash, parkAll, cards...)
	// 确认框的标题与批量钮同一个键（library.deleteForever / library.emptyTrash）。
	regConfirm("app.window.library.list-confirm-delete", selectTrash, listChain(selectTrash, clickLabel(locales.LibraryDeleteForever)), locales.LibraryDeleteForever, overlay...)
	regConfirm("app.window.library.list-confirm-empty-trash", facetLabel(locales.LibraryScopeTrash),
		listChain(facetLabel(locales.LibraryScopeTrash), clickLabel(locales.LibraryEmptyTrash)), locales.LibraryEmptyTrash, overlay...)
	reg("app.window.library.list-menu-entry", list, listRightClickCard(pickB), parkKeepFocus, overlay...)
	reg("app.window.library.list-menu-bulk", list, listChain(selectBoth, listRightClickCard(pickB)), parkKeepFocus, overlay...)
	reg("app.window.library.list-batch-done", list, favoriteBoth, parkAll, results...)
	reg("app.window.library.list-batch-failed", failing, favoriteBoth, parkAll, results...)
	reg("app.window.library.list-batch-failed-open", failing, listChain(favoriteBoth, listOpenDetails), parkAll, results...)
	// 批量进行中：失败一项之后点「仅重试失败项」，重试卡住不返回（结果条「处理中，已核对 2 项」，两枚钮禁用）。
	// 两枚钮 disabled:opacity-50：整组带透明度的文字在合成层里用灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（A-009 的延伸）。
	busyInk := append(append([]parity.Waiver{}, results...), opacityInkWaiver("「仅重试失败项」"), opacityInkWaiver("「收起」"))
	reg("app.window.library.list-batch-busy", holding,
		listChain(favoriteBoth, listHoldBatches, clickLabel(locales.LibraryWorkflowRetryFailed), listWaitProcessing), parkAll, busyInk...)
	reg("app.window.library.table-bulk", table, listChain(tableCheckRow(0), tableCheckRow(2)), parkAll, base...)
	reg("app.window.library.table-menu-more", table, tableOpenRowMore(1), parkKeepFocus, tableOverlay...)
	// 800×600 里标签浮层的投影落在表格最后一行与吸附列底下的文字上：比较器的背景取样在投影渐变上不稳定（G-023）。
	reg("app.window.library.table-tag-picker", table, tableOpenTagPicker(1), parkKeepFocus, append(append([]parity.Waiver{}, tableOverlayTag...), shadowRingWaivers()...)...)
}
