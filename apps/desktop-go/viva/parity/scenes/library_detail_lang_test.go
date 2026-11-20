package scenes

import (
	"path/filepath"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 详情里「先点开一个折叠块再取图」的三个场景按界面语言找按钮：按钮名必须在执行步骤时才取，
// 不能在包初始化（登记场景）时取——那时还是中文，英文变体里就拿中文名找英文按钮、步骤失败
// （M2-4 搭英文变体时暴露，台账 G-017 的更新；这是对照夹具的约定，不是 Viva 的行为）。
// 语言是进程内的全局状态：本测试不并行，结束时还原。
func TestDetailOpenScenesResolveTheirButtonNamesInTheActiveLanguage(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	for _, id := range []string{
		"app.window.library.detail-review-open",
		"app.window.library.detail-revisions-open",
		"app.window.library.detail-conflict-open",
	} {
		// 变体与区域取自入库的中文样本；渲染只用它们定窗口与区域，按钮名来自此刻的界面语言。
		react, err := parity.LoadFacts(filepath.Join("..", "testdata", "react", "light-royal-blue-medium-1280x800", id+".facts.json"))
		if err != nil {
			t.Fatal(err)
		}
		scene, err := Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parity.RenderFacts(scene, react.Variant, react.Region); err != nil {
			t.Errorf("场景 %s 在英文界面下执行步骤失败（按钮名是不是在登记时就取了？）：%v", id, err)
		}
	}
}
