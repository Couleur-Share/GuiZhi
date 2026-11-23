package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// wikiFixtureJSON 是 Wiki 对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-wiki.mjs）读的是同一个文件，
// 两侧因此处在同一组合成数据里。页面、链接、来源、历史版本与更新建议的 JSON 形状沿用 services 的 DTO（与渲染层 WikiPage 等一一对应）。
//
//go:embed testdata/wiki-fixture.json
var wikiFixtureJSON []byte

// WikiFixtureLink 是一条有向边：from 页引用 to 页。
type WikiFixtureLink struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// WikiFixtureStatus 是顶栏状态行里的「已编译 / 可编译」条目数（页数取实际页面数）。
type WikiFixtureStatus struct {
	CompiledItemCount int `json:"compiledItemCount"`
	EligibleItemCount int `json:"eligibleItemCount"`
}

// WikiFixture 是夹具的内容。
type WikiFixture struct {
	// Now、Timezone、Locale 是对照用的固定时钟（两侧同值：页面时间的写法取决于它们）。
	Now      int64  `json:"now"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	// Models.Chat 是「已配置」的对话模型（React 一侧经 settings.get 的 aiModels 给出；Viva 一侧由假服务的 CompileConfigured 表达）。
	Models struct {
		Chat string `json:"chat"`
	} `json:"models"`
	Status      WikiFixtureStatus                    `json:"status"`
	Pages       []services.WikiPage                  `json:"pages"`
	Links       []WikiFixtureLink                    `json:"links"`
	Sources     map[string][]services.WikiPageSource `json:"sources"`
	Revisions   map[string][]services.WikiRevision   `json:"revisions"`
	Suggestions map[string]services.WikiSuggestion   `json:"suggestions"`
	// Compile.Preview 是有页面的场景里的编译预览（候选、各状态计数、调用次数估算）。
	Compile struct {
		Preview services.WikiCompilePreview `json:"preview"`
	} `json:"compile"`
	// Empty 是「没有任何页面」场景的状态行与预览。
	Empty struct {
		Status  WikiFixtureStatus           `json:"status"`
		Preview services.WikiCompilePreview `json:"preview"`
	} `json:"empty"`
	// Scene 指定各场景用的页面。
	Scene struct {
		DetailPageID     string `json:"detailPageId"`
		EditPageID       string `json:"editPageId"`
		SuggestionPageID string `json:"suggestionPageId"`
	} `json:"scene"`
}

// Page 取夹具里 ID 为 id 的页面；没有时返回错误。
func (f WikiFixture) Page(id string) (services.WikiPage, error) {
	for _, p := range f.Pages {
		if p.ID == id {
			return p, nil
		}
	}
	return services.WikiPage{}, fmt.Errorf("Wiki 夹具里没有页面 %q", id)
}

// LoadWikiFixture 解码夹具。
func LoadWikiFixture() (WikiFixture, error) {
	var f WikiFixture
	if err := json.Unmarshal(wikiFixtureJSON, &f); err != nil {
		return WikiFixture{}, fmt.Errorf("解码 Wiki 夹具失败：%w", err)
	}
	return f, nil
}
