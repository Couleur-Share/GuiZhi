package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// libraryFixtureJSON 是知识库侧栏对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-library.mjs）读的是同一个文件，
// 两侧因此处在同一组合集、标签与计数里。
//
// 计数与筛选无关：夹具对任何筛选都返回同一份计数。真实库里分面计数会随筛选变化，但两侧输入相同，
// 对照比较的是「同样的数据画成什么样」，不是计数的算法（算法由主进程的处理器测试守住）。
//
//go:embed testdata/library-fixture.json
var libraryFixtureJSON []byte

// LibraryVariant 是侧栏的一份数据：合集、标签与计数。
type LibraryVariant struct {
	Counts      services.LibraryCounts       `json:"counts"`
	Collections []services.LibraryCollection `json:"collections"`
	Tags        []services.LibraryTag        `json:"tags"`
}

// LibraryFixture 是夹具的内容。
type LibraryFixture struct {
	// Variants 按名称给出几份数据：data（日常）、empty（空库）、long（超长名称与超 99 的计数）、many（成排的行）。
	Variants map[string]LibraryVariant `json:"variants"`
	// SmartViews 按名称给出智能视图的存储内容（渲染层 localStorage 里的原始 JSON 数组，Viva 一侧原样放进偏好）。
	SmartViews map[string]json.RawMessage `json:"smartViews"`
}

// Variant 取名为 name 的数据；没有这份数据是程序错误（场景写错了名字）。
func (f LibraryFixture) Variant(name string) LibraryVariant {
	v, ok := f.Variants[name]
	if !ok {
		panic("知识库夹具里没有数据 " + name)
	}
	return v
}

// LoadLibraryFixture 解码夹具。
func LoadLibraryFixture() (LibraryFixture, error) {
	var f LibraryFixture
	if err := json.Unmarshal(libraryFixtureJSON, &f); err != nil {
		return LibraryFixture{}, fmt.Errorf("解码知识库夹具失败：%w", err)
	}
	return f, nil
}
