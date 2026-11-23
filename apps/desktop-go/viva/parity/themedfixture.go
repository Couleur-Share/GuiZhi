package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// themedFixtureJSON 是主题阅读（AI 阅读，B-064）对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-themed.mjs）读的是同一个文件，
// 两侧因此处在同一组合成数据里。任务、页面的 JSON 形状沿用 services 的 DTO（与渲染层 ThemedReadingTask / ThemedReadingVersion 一一对应；
// 页面里 DTO 之外的字段——参考资料、成稿章节——只给 React 一侧的页面接口用，Go 一侧另取）。
//
//go:embed testdata/themed-fixture.json
var themedFixtureJSON []byte

// ThemedFixturePage 是夹具里的页面：DTO 加上 React 一侧才读的 reconstruction（参考资料与成稿章节引用）。
type ThemedFixturePage struct {
	services.ThemedPage
	Reconstruction struct {
		References []services.ThemedReference `json:"references"`
		Draft      []struct {
			ReferenceIDs []string `json:"referenceIds"`
		} `json:"draft"`
	} `json:"reconstruction"`
}

// UsedReferences 是页面成稿真正引用到、并且读取成功的参考资料（渲染层「参考资料」面板的筛选，ThemedReadingPageTools.tsx）。
func (p ThemedFixturePage) UsedReferences() []services.ThemedReference {
	var out []services.ThemedReference
	for _, ref := range p.Reconstruction.References {
		if ref.Status != "ready" {
			continue
		}
		for _, section := range p.Reconstruction.Draft {
			if contains(section.ReferenceIDs, ref.ID) {
				out = append(out, ref)
				break
			}
		}
	}
	return out
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// ThemedFixture 是夹具的内容。
type ThemedFixture struct {
	// Now、Timezone、Locale 是对照用的固定时钟（与知识库夹具同值）。
	Now      int64  `json:"now"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	// ItemID、ItemTitle、SourceKind 是知识库场景里被阅读的条目与内容来源（列表夹具 data 变体里的笔记）。
	ItemID     string `json:"itemId"`
	ItemTitle  string `json:"itemTitle"`
	SourceKind string `json:"sourceKind"`
	// Models 是已配置的文本与生图模型；Search 是联网搜索状态。
	Models struct {
		Text  string `json:"text"`
		Image string `json:"image"`
	} `json:"models"`
	Search services.ThemedSearchStatus `json:"search"`
	// Running、Failed 是知识库场景里这篇内容的最近任务（生成中、失败）；Page 是已生成的阅读页。
	Running services.ThemedTask `json:"running"`
	Failed  services.ThemedTask `json:"failed"`
	Page    ThemedFixturePage   `json:"page"`
	// Inbox.Tasks 是处理中心「主题页任务」里的任务（已按更新时间从新到旧排好）。
	Inbox struct {
		Tasks []services.ThemedTask `json:"tasks"`
	} `json:"inbox"`
}

// LoadThemedFixture 解码夹具。
func LoadThemedFixture() (ThemedFixture, error) {
	var f ThemedFixture
	if err := json.Unmarshal(themedFixtureJSON, &f); err != nil {
		return ThemedFixture{}, fmt.Errorf("解码主题阅读夹具失败：%w", err)
	}
	return f, nil
}
