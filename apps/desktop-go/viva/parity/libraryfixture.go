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

// LibraryVariant 是知识库的一份数据：合集、标签与计数（侧栏），以及列表里的条目（M2-2）。
// 条目与计数互不推导：计数是侧栏上显示的数字，条目是列表里实际有的行，两者不必相符。
type LibraryVariant struct {
	Counts      services.LibraryCounts       `json:"counts"`
	Collections []services.LibraryCollection `json:"collections"`
	Tags        []services.LibraryTag        `json:"tags"`
	Entries     []services.LibraryEntry      `json:"entries"`
}

// LibraryFixture 是夹具的内容。
type LibraryFixture struct {
	// Now、Timezone 与 Locale 是两侧共用的「现在」、时区与区域：条目时间的写法（今天写时分，其余写日期）取决于它们。
	// React 采集器把 Date.now() 与时区钉在这里（lib/react-library.mjs），Viva 场景用同一组值。
	Now      int64  `json:"now"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
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

// libraryDetailFixtureJSON 是条目详情（M2-4）的数据夹具：列表投影里没有的正文、摘要、文字稿、来源与复核状态，
// 来源版本，以及冲突与失败的场景数据。React 采集器（scripts/parity/lib/react-library.mjs）读的是同一个文件。
//
//go:embed testdata/library-detail-fixture.json
var libraryDetailFixtureJSON []byte

// LibraryDetail 是一条的详情字段（叠在列表夹具的同 ID 条目上）。
type LibraryDetail struct {
	Content    string `json:"content"`
	Summary    string `json:"summary"`
	Transcript string `json:"transcript"`
	SourceURI  string `json:"sourceUri"`
	// ReviewStatus 与 ReviewReasons 为空表示不改（列表夹具里全是 clear）。
	ReviewStatus  string   `json:"reviewStatus"`
	ReviewReasons []string `json:"reviewReasons"`
}

// LibraryDetailFixture 是详情夹具的内容。
type LibraryDetailFixture struct {
	// Details 按条目 ID 给出详情字段。
	Details map[string]LibraryDetail `json:"details"`
	// SourceVersions 按条目 ID 给出来源版本（新的在前）。
	SourceVersions map[string][]services.LibrarySourceVersion `json:"sourceVersions"`
	// Conflict 是保存冲突场景的两个标题：另一入口已改成的标题，与用户在标题框里改成的标题。
	Conflict struct {
		SavedTitle string `json:"savedTitle"`
		MineTitle  string `json:"mineTitle"`
	} `json:"conflict"`
	// Failures 是读取与保存失败场景的失败原因（两侧同文）。
	Failures struct {
		Get  string `json:"get"`
		Save string `json:"save"`
	} `json:"failures"`
}

// LoadLibraryDetailFixture 解码详情夹具。
func LoadLibraryDetailFixture() (LibraryDetailFixture, error) {
	var f LibraryDetailFixture
	if err := json.Unmarshal(libraryDetailFixtureJSON, &f); err != nil {
		return LibraryDetailFixture{}, fmt.Errorf("解码知识库详情夹具失败：%w", err)
	}
	return f, nil
}
