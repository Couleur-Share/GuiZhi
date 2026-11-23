package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// overlaysFixtureJSON 是全局浮层（M3-6a：命令面板与更新、设置清单、旧版迁移三个对话框）对照场景的数据夹具：
// 与 React 采集器（scripts/parity/lib/react-overlays.mjs）读的是同一个文件，两侧因此处在同一组合成数据里。
// 渲染层的命令面板只读知识条目、Wiki 页与采集任务的标题与说明；更新对话框只画版本号与更新说明；迁移确认页只画条目数。
// 夹具里的 path 是探测接口原本就会给的旧库路径，渲染层不显示它：Viva 的确认页多画一行来源路径，所以对照场景不把它交给 Viva（见 viva/parity/scenes/overlays.go）。
//
//go:embed testdata/overlays-fixture.json
var overlaysFixtureJSON []byte

// OverlaysFixture 是夹具的内容。
type OverlaysFixture struct {
	// Palette 是命令面板「搜索命中态」：Query 是输入的搜索词，其余三组是远程搜索各来源返回的结果。
	Palette struct {
		Query     string                   `json:"query"`
		Knowledge []OverlaysKnowledgeEntry `json:"knowledge"`
		Wiki      []services.WikiSearchHit `json:"wiki"`
		Tasks     []OverlaysTask           `json:"tasks"`
	} `json:"palette"`
	// Update 是更新对话框「发现新版本」态：当前版本、新版本号与更新说明（Markdown）。
	Update struct {
		CurrentVersion string `json:"currentVersion"`
		Version        string `json:"version"`
		ReleaseNotes   string `json:"releaseNotes"`
	} `json:"update"`
	// Setup 是首次使用设置清单态：OptionalOpen 为真时展开「更多可选设置」。
	Setup struct {
		OptionalOpen bool `json:"optionalOpen"`
	} `json:"setup"`
	// Migration 是旧版数据迁移确认态：探测到的旧库条目数与路径。
	Migration struct {
		ItemCount int    `json:"itemCount"`
		Path      string `json:"path"`
	} `json:"migration"`
}

// OverlaysKnowledgeEntry 是命令面板里一条知识条目命中（渲染层只读 id、title、snippet）。
type OverlaysKnowledgeEntry struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// OverlaysTask 是命令面板里一条采集任务命中（渲染层只读 id、displayName、status、error）。
type OverlaysTask struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	Error       string `json:"error"`
}

// LoadOverlaysFixture 解码夹具。
func LoadOverlaysFixture() (OverlaysFixture, error) {
	var f OverlaysFixture
	if err := json.Unmarshal(overlaysFixtureJSON, &f); err != nil {
		return OverlaysFixture{}, fmt.Errorf("解码全局浮层夹具失败：%w", err)
	}
	return f, nil
}
