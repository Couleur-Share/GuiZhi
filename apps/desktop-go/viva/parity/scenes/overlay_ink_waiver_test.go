package scenes

import (
	"math"
	"testing"

	"guizhi-mygo-shell/viva/parity"
)

// 以下守住台账 G-024 的两条结论：
//  1. 墨迹总量（ink-mass）的登记必须同时有方向与上限，不能再有「不管多少、朝哪边都放过」的条款——
//     overlayInkWaiver 曾经就是这样，Viva 的墨迹少到 0.36 倍也被当成 A-009（LCD 偏重）放过，C-008 因此藏了三个模块；
//  2. overlayInkWaiver 只放过「Viva 偏多、比值不超过 1.5」，浮层里彩色表情偏少那一类单独登记在 overlayEmojiInkWaiver，
//     而且只登记在出现它的那一个场景里。

// inkIssue 造一条墨迹总量问题：ratio 是 Viva / React；偏差大小与方向的写法同比较器（|ratio−1|，Viva 偏多为 +1）。
func inkIssue(subject string, ratio float64, overlay bool) parity.Issue {
	dir := int8(1)
	if ratio < 1 {
		dir = -1
	}
	return parity.Issue{
		Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, Overlay: overlay,
		Delta: math.Abs(ratio - 1), Dir: dir,
	}
}

// TestInkMassWaiversAreDirectedAndBounded 守住「每条 ink-mass 登记都有方向与上限」：
// 没有方向的登记会让 Viva 偏少的缺陷冒充 Viva 偏多的已登记原因，没有上限的登记会放过任意大的偏差。
// 新增场景时漏写其一，这里会直接指到那条登记。
func TestInkMassWaiversAreDirectedAndBounded(t *testing.T) {
	seen := map[string]bool{}
	checked := 0
	for _, id := range IDs() {
		scene, err := Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range scene.Waivers {
			if w.Layer != parity.LayerL2 || w.Kind != "ink-mass" {
				continue
			}
			checked++
			key := w.Ledger + "|" + w.Subject + "|" + w.Note
			if seen[key] {
				continue
			}
			seen[key] = true
			// 唯一的例外：A-014 的「折行点差一个词」——渲染层与 Viva 在哪一边多放一个词都有可能，所以墨迹总量两个方向都可能偏，
			// 由 MaxDelta 与具体的摘要文字（Subject）框住。别的登记都对应一个有方向的物理原因，必须写方向。
			if w.Dir == 0 && !(w.Ledger == "A-014" && w.Subject != "") {
				t.Errorf("场景 %s 的 ink-mass 登记 %s %q 没有方向（Dir）：Viva 偏少的缺陷也会被放过（台账 G-024）", id, w.Ledger, w.Subject)
			}
			if w.MaxDelta <= 0 {
				t.Errorf("场景 %s 的 ink-mass 登记 %s %q 没有上限（MaxDelta）：任意大的偏差都会被放过（台账 G-024）", id, w.Ledger, w.Subject)
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有找到任何 ink-mass 登记：测试没有守住东西")
	}
}

// TestOverlayInkWaiverOnlyCoversHeavierWithinCap 守住 overlayInkWaiver 的范围：
// 实测最大比值 1.478 与阈值边缘 1.153 放过，上限 1.5 含边界，超过一点点就不放过；
// Viva 偏少（C-008 的 0.36–0.64、彩色表情的 0.84）、页面层的同类文字、别的类别与别的层都不放过；没有记大小的问题也不放过。
func TestOverlayInkWaiverOnlyCoversHeavierWithinCap(t *testing.T) {
	cases := []struct {
		name  string
		issue parity.Issue
		want  bool
	}{
		{"实测最大值（导入详情「入队」）", inkIssue("「入队」", 1.478, true), true},
		{"阈值边缘", inkIssue("「保存」", 1.153, true), true},
		{"上限含边界", inkIssue("「保存」", 1.5, true), true},
		{"超过上限一点点", inkIssue("「保存」", 1.51, true), false},
		{"超过上限很多", inkIssue("「保存」", 2.0, true), false},
		{"C-008：被淡出两次的按钮文字", inkIssue("「添加」", 0.64, true), false},
		{"C-008：最差的一次", inkIssue("「添加」", 0.381, true), false},
		{"Viva 偏少一点点", inkIssue("「保存」", 0.84, true), false},
		{"页面层同类文字", inkIssue("「保存」", 1.30, false), false},
	}
	for _, c := range cases {
		if got := overlayInkWaiver.MatchesIn(c.issue, tierMedium); got != c.want {
			t.Errorf("%s：overlayInkWaiver 放过 = %v，期望 %v（%+v）", c.name, got, c.want, c.issue)
		}
	}
	other := inkIssue("「保存」", 1.3, true)
	other.Kind = "ink"
	if overlayInkWaiver.MatchesIn(other, tierMedium) {
		t.Error("墨迹位置（ink）问题不应被墨迹总量的登记放过")
	}
	other = inkIssue("「保存」", 1.3, true)
	other.Layer = parity.LayerL3
	if overlayInkWaiver.MatchesIn(other, tierMedium) {
		t.Error("L3 的问题不应被 L2 的登记放过")
	}
	other = inkIssue("「保存」", 1.3, true)
	other.Delta = 0
	if overlayInkWaiver.MatchesIn(other, tierMedium) {
		t.Error("没有记大小的问题不应被放过")
	}
}

// TestOverlayEmojiInkWaiverCoversOnlyThePaleEmoji 守住 overlayEmojiInkWaiver 的范围：浮层里、单个彩色表情、Viva 偏少、
// 偏差在 [0.15, 0.17]（实测 0.837–0.848 即偏差 0.152–0.163）；方向相反、偏差更大、普通文字、页面层的表情、带纵向错位的都不放过。
func TestOverlayEmojiInkWaiverCoversOnlyThePaleEmoji(t *testing.T) {
	w := overlayEmojiInkWaiver
	cases := []struct {
		name  string
		issue parity.Issue
		want  bool
	}{
		{"实测最低", inkIssue("「📖」", 0.837, true), true},
		{"实测最高", inkIssue("「📖」", 0.848, true), true},
		{"上限含边界", inkIssue("「📖」", 0.83, true), true},
		{"比上限更少", inkIssue("「📖」", 0.82, true), false},
		{"Viva 偏多（A-023 在侧栏行上的方向）", inkIssue("「📖」", 1.2, true), false},
		{"普通文字", inkIssue("「添加」", 0.84, true), false},
		{"页面层的表情", inkIssue("「📖」", 0.84, false), false},
	}
	for _, c := range cases {
		if got := w.MatchesIn(c.issue, tierMedium); got != c.want {
			t.Errorf("%s：overlayEmojiInkWaiver 放过 = %v，期望 %v（%+v）", c.name, got, c.want, c.issue)
		}
	}
	shifted := inkIssue("「📖」", 0.84, true)
	shifted.DY = 1
	if w.MatchesIn(shifted, tierMedium) {
		t.Error("带纵向错位的表情墨迹问题不应被放过（G-012）")
	}
}

// TestNoSceneWaivesAHalvedOverlayText 把 C-008 的现象固定成回归：任何场景的登记合起来都不应放过
// 「浮层里文字的墨迹总量只有渲染层一半」。这是 C-008 藏了三个模块的那种缺陷。
func TestNoSceneWaivesAHalvedOverlayText(t *testing.T) {
	halved := inkIssue("「添加」", 0.5, true)
	for _, id := range IDs() {
		scene, err := Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if anyWaives(scene.Waivers, halved, tierMedium) {
			t.Errorf("场景 %s 的登记放过了浮层文字墨迹总量只有一半的问题（C-008 一类的缺陷会被藏起来）", id)
		}
	}
}

// TestOverlayEmojiInkWaiverOnlyOnTheFilledCollectionDialog 守住登记的作用域：只有「新建合集（已选图标）」对话框登记了它，
// 别的对话框（包括同一个对话框没选图标的状态）里的表情偏少照常判定。
func TestOverlayEmojiInkWaiverOnlyOnTheFilledCollectionDialog(t *testing.T) {
	const filled = "app.window.library.dlg-collection-new-filled"
	pale := inkIssue("「📖」", 0.84, true)
	found := false
	for _, id := range IDs() {
		scene, err := Get(id)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, w := range scene.Waivers {
			if w == overlayEmojiInkWaiver {
				has = true
			}
		}
		if id == filled {
			found = has
			if !has || !anyWaives(scene.Waivers, pale, tierMedium) {
				t.Errorf("场景 %s 应登记 overlayEmojiInkWaiver 并放过 0.84 的浮层表情", id)
			}
			continue
		}
		if has || anyWaives(scene.Waivers, pale, tierMedium) {
			t.Errorf("场景 %s 不应放过浮层里偏少的表情（overlayEmojiInkWaiver 只属于 %s）", id, filled)
		}
	}
	if !found {
		t.Fatalf("没有找到场景 %s", filled)
	}
}
