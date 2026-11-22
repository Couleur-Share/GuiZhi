package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// captureFixtureJSON 是快速采集对话框对照场景的数据夹具：与 React 采集器（scripts/parity/lib/react-capture.mjs）读的是同一个文件，
// 两侧因此处在同一组合成数据与同样的输入里。
//
//go:embed testdata/capture-fixture.json
var captureFixtureJSON []byte

// CaptureFixture 是夹具的内容。
type CaptureFixture struct {
	// Collections 是目标合集下拉里的合集（collection:list 的回复）。
	Collections []services.LibraryCollection `json:"collections"`
	Scenes      struct {
		// URLs 是「输入链接」场景草稿框里的内容（两行各一个链接）。
		URLs struct {
			Draft string `json:"draft"`
		} `json:"urls"`
		// Mixed 是「混合内容」场景的草稿：小红书分享口令，文字里夹着有专用连接器的链接，默认采集链接并出现改判按钮。
		Mixed struct {
			Draft string `json:"draft"`
		} `json:"mixed"`
		// FilesTags 是「文件、标签与合集」场景：选择文件框返回的路径、逐个回车添加的标签、下拉里选的合集 ID。
		FilesTags struct {
			Files      []string `json:"files"`
			Tags       []string `json:"tags"`
			Collection string   `json:"collection"`
		} `json:"filesTags"`
	} `json:"scenes"`
}

// CollectionName 是夹具里编号为 id 的合集的名称（下拉选项的文字）；没有这个合集时返回空串。
func (f CaptureFixture) CollectionName(id string) string {
	for _, c := range f.Collections {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

// LoadCaptureFixture 解码夹具。
func LoadCaptureFixture() (CaptureFixture, error) {
	var f CaptureFixture
	if err := json.Unmarshal(captureFixtureJSON, &f); err != nil {
		return CaptureFixture{}, fmt.Errorf("解码快速采集夹具失败：%w", err)
	}
	return f, nil
}
