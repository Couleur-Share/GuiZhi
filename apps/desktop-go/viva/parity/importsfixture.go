package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// importsFixtureJSON 是采集队列对照场景的任务夹具：与 React 采集器（scripts/parity/lib/react-imports.mjs）读的是同一个文件，
// 两侧因此处在同一组数据与同一个「现在」里。
//
//go:embed testdata/imports-fixture.json
var importsFixtureJSON []byte

// ImportsFixture 是夹具的内容。
type ImportsFixture struct {
	// Now 是固定的「现在」（Unix 毫秒）：相对时间与「今天/昨天」的日期标签都以它为准。
	Now      int64  `json:"now"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	// LoadError 是「读取失败」场景里列表请求返回的错误原文。
	LoadError string `json:"loadError"`
	// NoMatchQuery 是「当前筛选下没有任务」场景里搜索框的内容：夹具里没有任何任务含这个词。
	NoMatchQuery string `json:"noMatchQuery"`
	Queue        struct {
		Concurrency int `json:"concurrency"`
	} `json:"queue"`
	Tasks []services.ImportTask `json:"tasks"`
	// Items 是任务详情弹窗要读的条目事实（按 id）：React 一侧据此在页内合成条目，Viva 一侧直接取用。
	Items map[string]services.ImportItemFacts `json:"items"`
}

// NamedSourceInputs 是带显示名的任务的原始地址：这些行的标题挂着「原始地址」的提示（渲染层 title={sourceTooltip}）。
func (f ImportsFixture) NamedSourceInputs() []string {
	var out []string
	for _, t := range f.Tasks {
		if t.DisplayName != "" && t.SourceInput != "" {
			out = append(out, t.SourceInput)
		}
	}
	return out
}

// SourceInputOf 是编号为 id 的任务的原始地址；夹具里没有这个任务时返回空串。
func (f ImportsFixture) SourceInputOf(id string) string {
	for _, t := range f.Tasks {
		if t.ID == id {
			return t.SourceInput
		}
	}
	return ""
}

// LoadImportsFixture 解码夹具。
func LoadImportsFixture() (ImportsFixture, error) {
	var f ImportsFixture
	if err := json.Unmarshal(importsFixtureJSON, &f); err != nil {
		return ImportsFixture{}, fmt.Errorf("解码采集队列夹具失败：%w", err)
	}
	return f, nil
}
