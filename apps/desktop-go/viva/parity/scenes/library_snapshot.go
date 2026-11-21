package scenes

import (
	"errors"
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 公众号网页快照（M2-5b，台账 B-066）的整窗场景：选中夹具里的网页条目（来源换成公众号文章），点标签栏里的「网页快照」，看快照工具条与各类通知条。
// 与 React 采集器（scripts/parity/scenes/react-library-detail-scenes.mjs 的 detail-snapshot-*）一一对应：同样的条目、同样的点击；
// 数据取详情夹具的 snapshots 段，两侧共用。快照 iframe 与标准排版的正文是阅读岛的内容，两侧都不参与（React 一侧 HIDE_SNAPSHOT_BODY_CSS，
// Viva 的阅读岛在无头会话里画不出来）；工具条与通知条是原生界面，完整参与 L1–L3 对拍。

// 网页快照场景的数据模式，与 React 采集器 config.detail.snapshot 同值。
const (
	snapshotToolbar  = "toolbar"  // 完整的原文快照：工具条里是模式文字、宽度下拉与「更多」
	snapshotMissing  = "missing"  // 没有保存过快照：「尚未保存网页快照……」加「补采网页快照」钮
	snapshotError    = "error"    // 读取失败：错误原因加「重试读取」钮
	snapshotWarnings = "warnings" // 改过 + 有新来源版本 + 缺失资源与警告
)

// snapshotSourceURI 是条目 id 的来源链接：网页快照场景里夹具的快照条目换成公众号文章（夹具里没有一条详情带公众号链接），其余取 own。
func (o libraryOptions) snapshotSourceURI(fix parity.LibraryDetailFixture, id, own string) string {
	if o.snapshot != "" && id == fix.Snapshots.Item {
		return fix.Snapshots.SourceURI
	}
	return own
}

// seedSnapshot 把快照读取的回复放进假服务：error 模式让读取失败（原因取夹具），其余按模式放视图。
func (o libraryOptions) seedSnapshot(svc *servicestest.Library, fix parity.LibraryDetailFixture) {
	switch o.snapshot {
	case "":
	case snapshotError:
		svc.FailOn("WebSnapshot", errors.New(fix.Snapshots.Error))
	default:
		svc.SeedSnapshot(fix.Snapshots.Item, "", fix.SnapshotView(o.snapshot))
	}
}

// detailOpenSnapshot 选中夹具里的网页条目，点标签栏里的「网页快照」（点完它变成「返回阅读」），等读取落定（对应 React 一侧的 openSnapshot）。
// 标签名在步骤执行时才取（界面语言在场景登记之后才设定，理由同 detailOpenSummary）。
func detailOpenSnapshot(s *headless.Session, look *kit.Look, e *libraryEnv) error {
	if err := listClickCard(detailWebpage)(s, look, e); err != nil {
		return err
	}
	if err := parity.ClickRole(s, gfx.RoleButton, locales.ArticleAskViewSnapshot()); err != nil {
		return err
	}
	if err := s.Rest(); err != nil {
		return err
	}
	// 读取在 Spawn 里同步完成；找不到「返回阅读」说明点击没有切到快照页。
	back, ok := s.AccessByRole(gfx.RoleButton, locales.LibraryBackToReading())
	if !ok {
		return fmt.Errorf("点了「%s」之后找不到「%s」：没有切到网页快照页", locales.ArticleAskViewSnapshot(), locales.LibraryBackToReading())
	}
	// 无头会话里没有岛，页面不会回报版面状态：按页面里 use-snapshot-reader 的公式代报（见 snapshotWideRail）。
	e.screen.ReportSnapshotLayout(snapshotWideRail(look, back.Bounds.X), false)
	return s.Rest()
}

// snapshotWideRail 是目录是否常驻正文侧边的窄轨（渲染层 use-snapshot-reader 的 wideRail）：滚动容器宽 − iframe 宽 的一半 ≥ 64px。
// 滚动容器是详情栏（右缘到窗口右缘，左缘 = 标签栏第一枚标签的左缘 − 工具栏的 px-6），内边距 p-3；iframe 宽取 min(100%, 舒适宽度)，
// 舒适宽度 = round(max(560, min(900, 字号 × 36 + 48)))：夹具的快照页没有桥接脚本、不回报字号，取默认的 16px，即 624px。
// backX 是标签栏里「返回阅读」钮的左缘（它是标签栏的第一个控件）。
func snapshotWideRail(look *kit.Look, backX float32) bool {
	const comfortable = 624
	hostW := look.Viewport - (backX - look.Space(6))
	iframeW := min(hostW-2*look.Space(3), comfortable)
	return (hostW-iframeW)/2 >= 64
}

func init() {
	detail := libraryOptions{variant: "data", list: true, detail: true}
	reg := func(id, mode string, waivers ...parity.Waiver) {
		opt := detail
		opt.snapshot = mode
		registry[id] = libraryEnvScene(id, opt, detailOpenSnapshot, parkAll, waivers...)
	}
	cards := semiboldKerningWaivers()
	reg("app.window.library.detail-snapshot-toolbar", snapshotToolbar, cards...)
	reg("app.window.library.detail-snapshot-missing", snapshotMissing, cards...)
	reg("app.window.library.detail-snapshot-error", snapshotError, cards...)
	reg("app.window.library.detail-snapshot-warnings", snapshotWarnings, cards...)
}
