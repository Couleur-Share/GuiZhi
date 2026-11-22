package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// inboxFixtureJSON 是处理中心对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-inbox.mjs）读的是同一个文件，
// 两侧因此处在同一组合成数据里。
//
//go:embed testdata/inbox-fixture.json
var inboxFixtureJSON []byte

// InboxVariant 是一组读取结果：主列表（条目、计数、去重总数）、Wiki 编译候选的各状态条数、语义索引进度。
type InboxVariant struct {
	List     services.InboxList           `json:"list"`
	Wiki     services.InboxWikiCandidates `json:"wiki"`
	Semantic struct {
		EligibleItems int `json:"eligibleItems"`
		IndexedItems  int `json:"indexedItems"`
	} `json:"semantic"`
}

// InboxFixture 是夹具的内容。
type InboxFixture struct {
	// LoadError 是「主列表读取失败」场景里 inbox:list 返回的错误原文；SemanticError 是语义索引状态读取失败的原因。
	LoadError     string `json:"loadError"`
	SemanticError string `json:"semanticError"`
	// Collections 是批量整理的「归入合集」下拉的选项来源。
	Collections []services.LibraryCollection `json:"collections"`
	// Variants 按名字给出数据：data 是各分组都有条目的完整数据，empty 是什么都没有。
	Variants map[string]InboxVariant `json:"variants"`
}

// Variant 取名为 name 的一组数据；夹具里没有这一组时返回错误。
func (f InboxFixture) Variant(name string) (InboxVariant, error) {
	v, ok := f.Variants[name]
	if !ok {
		return InboxVariant{}, fmt.Errorf("处理中心夹具里没有数据组 %q", name)
	}
	return v, nil
}

// UnclassifiedTitles 是「未归合集」分组里各条目的标题，按列表顺序：场景步骤按标题勾选条目。
func (v InboxVariant) UnclassifiedTitles() []string {
	var out []string
	for _, it := range v.List.Items {
		if it.Kind == services.InboxKindUnclassified {
			out = append(out, it.Title)
		}
	}
	return out
}

// LoadInboxFixture 解码夹具。
func LoadInboxFixture() (InboxFixture, error) {
	var f InboxFixture
	if err := json.Unmarshal(inboxFixtureJSON, &f); err != nil {
		return InboxFixture{}, fmt.Errorf("解码处理中心夹具失败：%w", err)
	}
	return f, nil
}
