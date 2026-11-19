package library

import "testing"

// 本文件钉住图标选择器「把已选格子滚到正中」的整数算术（渲染层 useLayoutEffect 用的是 DOM 的整数度量）。
// 公式的依据是对照场景（app.window.library.dlg-collection-rename* 三个字号档的 L2/L3），这里只守住舍入规则不被改回小数算。

func TestRoundHalfUpIsTheDOMRounding(t *testing.T) {
	cases := []struct{ in, want float32 }{
		{796.5, 797}, {674.5, 675}, {0.5, 1}, {0.49, 0}, {0, 0}, {-0.5, 0}, {-0.51, -1}, {40.5, 41}, {2.5, 3}, {-1.5, -1},
	}
	for _, c := range cases {
		if got := roundHalfUp(c.in); got != c.want {
			t.Errorf("roundHalfUp(%v) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

func TestSelectedScrollTarget(t *testing.T) {
	cases := []struct {
		name                                        string
		cellTop, cellHeight, contTop, contHeight, w float32
	}{
		// 中档（根字号 16px）：格子 36px，容器 16rem = 256px，内边距盒 254px；整数位置，没有舍入问题。
		{"中档整数位置", 601, 36, 100, 256, 391},
		// 格子在容器顶部附近：算出来是负数，滚动量不能为负。
		{"贴近顶部时夹到 0", 111, 36, 100, 256, 0},
		// 大档（根字号 18px）实测过的那一例：格子 2.25rem = 40.5px，容器 16rem = 288px。
		// offsetTop 796.5 进位成 797、offsetHeight 40.5 对齐成 41、clientHeight 286，目标 674.5 进位成 675；
		// 按小数算会得 673.75 → 674，差一个像素（这正是当初的缺陷）。
		{"大档半像素位置", 898, 40.5, 100.5, 288, 675},
		// 格子上沿落在半像素上时 offsetHeight 取「下沿取整 − 上沿取整」：601.5 → 602，下沿 637.5 → 638，高 36。
		{"上沿半像素", 601.5, 36, 100, 256, 392},
	}
	for _, c := range cases {
		if got := selectedScrollTarget(c.cellTop, c.cellHeight, c.contTop, c.contHeight); got != c.w {
			t.Errorf("%s：selectedScrollTarget(%v, %v, %v, %v) = %v，期望 %v", c.name, c.cellTop, c.cellHeight, c.contTop, c.contHeight, got, c.w)
		}
	}
}

// 要不要动按「取整后的滚动量」算：滚动区的布局用整数滚动量，滚动量带小数（用户拖过滚动条之类）时，
// 拿小数当「已应用的量」会把半个像素的差漏过去——该动没动，已选格子停在目标之外一个像素。
func TestScrollCorrectionComparesTheRoundedOffset(t *testing.T) {
	// 容器外框 y=100、高 258（内边距盒 256），格子高 36：格子在内容里的上沿 223 时，目标是 223-101-(256-36)/2 = 12。
	cases := []struct {
		name       string
		cellY, off float32
		wantTarget float32
		wantMove   bool
	}{
		{"已经在目标上", 211, 12, 12, false},
		{"差一个像素要动", 212, 11, 12, true},
		// 滚动量 12.5 取整成 13：格子当前的位置 210 加上 13 才是内容里的 223，目标 12，与 13 差 1 个像素，要动；
		// 若把 12.5 直接当「已应用的量」，与目标只差 0.5，不满足「大于半个像素」，就漏掉了。
		{"滚动量带半像素", 210, 12.5, 12, true},
		{"滚动量带小数但取整后正好", 211, 11.6, 12, false},
	}
	for _, c := range cases {
		target, move := scrollCorrection(c.cellY, 36, c.off, 100, 258)
		if target != c.wantTarget || move != c.wantMove {
			t.Errorf("%s：scrollCorrection(格子 %v、滚动量 %v) = (%v, %v)，期望 (%v, %v)", c.name, c.cellY, c.off, target, move, c.wantTarget, c.wantMove)
		}
	}
}
