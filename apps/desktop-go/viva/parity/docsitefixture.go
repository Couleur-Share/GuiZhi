package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// docsiteFixtureJSON 是采集文档站对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-docsite.mjs）读的是同一个文件，
// 两侧因此处在同一组合成批次与页面明细里。
//
//go:embed testdata/docsite-fixture.json
var docsiteFixtureJSON []byte

// DocSiteVariant 是一组读取结果：批次列表、各批次的页面明细，以及场景里点开页面明细的那个批次（空表示不点）。
type DocSiteVariant struct {
	Jobs     []services.CrawlJob             `json:"jobs"`
	Pages    map[string][]services.CrawlPage `json:"pages"`
	Selected string                          `json:"selected"`
}

// DocSiteFixture 是夹具的内容。
type DocSiteFixture struct {
	// Scope 是「已填入口与目录范围」场景里往两个输入框里键入的内容。
	Scope struct {
		URL       string `json:"url"`
		Directory string `json:"directory"`
	} `json:"scope"`
	Variants map[string]DocSiteVariant `json:"variants"`
}

// Variant 取名为 name 的一组数据；夹具里没有这一组时返回错误。
func (f DocSiteFixture) Variant(name string) (DocSiteVariant, error) {
	v, ok := f.Variants[name]
	if !ok {
		return DocSiteVariant{}, fmt.Errorf("文档站夹具里没有数据组 %q", name)
	}
	return v, nil
}

// Job 取 id 对应的批次；找不到时返回错误。
func (v DocSiteVariant) Job(id string) (services.CrawlJob, error) {
	for _, j := range v.Jobs {
		if j.ID == id {
			return j, nil
		}
	}
	return services.CrawlJob{}, fmt.Errorf("文档站夹具里没有批次 %q", id)
}

// LoadDocSiteFixture 解码夹具。
func LoadDocSiteFixture() (DocSiteFixture, error) {
	var f DocSiteFixture
	if err := json.Unmarshal(docsiteFixtureJSON, &f); err != nil {
		return DocSiteFixture{}, fmt.Errorf("解码文档站夹具失败：%w", err)
	}
	return f, nil
}
