package settings

// 界面偏好：设置之外零散的「键 → JSON 值」，经 Prefs 接口存取（物理上在 config/ui-prefs.json 的 prefs 段）。
// 键是点号分隔的名字；值的形状由各自的模块定义并容错读取（「坏数据静默丢弃」，与渲染层一致），
// 这里只集中登记键名、窗格宽度的取值范围，以及从旧 localStorage 导入时的结构校验。

// 偏好键。窗格宽度的三个键与 native/kit 的 PaneSpec 一致（settings 的测试核对）。
const (
	PrefAppModule            = "ui.appModule"
	PrefSidebarCollapsed     = "ui.sidebarCollapsed"
	PrefSidebarPanelWidth    = "ui.sidebarPanelWidth"
	PrefItemListPaneWidth    = "ui.itemListPaneWidth"
	PrefWikiCatalogPaneWidth = "ui.wikiCatalogPaneWidth"
	PrefLibraryViewMode      = "ui.libraryViewMode"

	// PrefLibraryColumns 是知识库表格的列配置，形状同旧 guizhi-library-table-config：{"columns":[{id,visible,width}]}。
	PrefLibraryColumns = "library.columns"
	// PrefSmartViews 是已保存的知识库视图（数组，最多 30 项）。
	PrefSmartViews = "library.smartViews"
	// PrefContentReading 是按条目记住的阅读位置（条目 ID → 记忆，最多 100 项）。
	PrefContentReading = "reading.content"
	// PrefSnapshotReading 是网页快照的阅读位置（记忆键 → 位置，最多 100 项）。
	PrefSnapshotReading = "reading.snapshot"
	// PrefCatalogOpen 是各阅读视图的目录是否展开：键 original:<editKey> 或 ai:<条目ID>:<来源类别>，值只有 true。
	PrefCatalogOpen = "reading.catalogOpen"
	// PrefWechatWidth 是微信文章阅读宽度：original | wide（缺省 fluid）。
	PrefWechatWidth = "reader.wechatWidth"

	PrefAskActiveSession   = "ask.activeSession"
	PrefSetupDismissed     = "setup.dismissed"
	PrefMigrationDismissed = "migration.dismissed"
)

// 窗格宽度的范围（与渲染层 ui.store.ts 的 *_WIDTH_DEFAULT/MIN/MAX 一致）。
type PaneBounds struct {
	Key           string
	Default       float64
	Min, Max      float64
	LegacyUIField string // 旧 ui-storage 里的字段名
}

// PaneWidths 是三个可拖动窗格的范围。
var PaneWidths = []PaneBounds{
	{PrefSidebarPanelWidth, 288, 220, 640, "sidebarPanelWidth"},
	{PrefItemListPaneWidth, 320, 240, 720, "itemListPaneWidth"},
	{PrefWikiCatalogPaneWidth, 300, 220, 560, "wikiCatalogPaneWidth"},
}

// Clamp 对应渲染层的 clamp：非有限数取下限。
func (b PaneBounds) Clamp(x float64) float64 {
	if !finite(x) {
		return b.Min
	}
	return clamp(x, b.Min, b.Max)
}

// NormalizeAppModule 对应渲染层 normalizeAppModule：未知取知识库。
func NormalizeAppModule(v string) string {
	switch v {
	case "inbox", "ask", "wiki", "imports":
		return v
	}
	return "library"
}

// NormalizeLibraryViewMode 对应 normalizeLibraryViewMode：兼容 v0.3 早期的行密度值（compact → list）。
func NormalizeLibraryViewMode(v string) string {
	if v == "list" || v == "compact" {
		return "list"
	}
	return "card"
}
