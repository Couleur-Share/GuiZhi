package library

import (
	"image"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
)

// 本文件守住表情字形的两件事（台账 A-001 曾误判的那条与槽内居中）：
//   - Viva 的文字标签只栅格化自己的矩形，彩色字形的墨迹比行盒高时超出的一行被裁掉，Chromium 不裁；
//     emojiGlyph 把标签放高（emojiOverflow）来补，这里量成品图像，并且验证布局高度没有被改动；
//   - 字形比容器宽时 CSS 居中让内容向两侧各溢出一半，Viva 的居中把负的剩余空间当 0，emojiGlyph 用负外边距补。

const emojiProbeTop = 12 // 探针画面里内容离画面边缘的距离

// renderEmoji 在白底、1:1 缩放的无头会话里画 draw 声明的内容（内容区左上角在 (emojiProbeTop, emojiProbeTop)），返回成品图像。
func renderEmoji(t *testing.T, width, height int, draw func(c ui.C, s *Screen, look *kit.Look)) *image.RGBA {
	t.Helper()
	rigFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	look := kit.NewLook(kit.DefaultSpec())
	look.Scale = 1
	look.Viewport = 1280
	s := &Screen{Look: look}
	white := gfx.RGBA(255, 255, 255, 1)
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		look.BeginPaint()
		ui.Row(c).Pad(emojiProbeTop).Gap(30).Background(white).Children(func() { draw(c, s, look) })
	}, Theme: &look.Theme})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: width, Height: height}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	defer session.Close()
	for i := 0; i < 3; i++ {
		if err := session.Frame(); err != nil {
			t.Fatalf("出帧失败：%v", err)
		}
	}
	img, err := session.Image()
	if err != nil {
		t.Fatalf("取图失败：%v", err)
	}
	return img
}

// inked 报告像素是否是字形的墨迹：「明显非白」（背景是纯白，覆盖度 ≥ 约 10% 的像素都算），并且不是探针里的红色标记条。
func inked(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return 0xFFFF-min(r, g, b) > 0x1800 && !isBar(img, x, y)
}

// isBar 报告像素是否是探针里的纯红标记条。
func isBar(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r > 0xE000 && g < 0x2000 && b < 0x2000
}

// extent 返回区域内满足 pred 的像素的最小与最大行、列号（绝对坐标）；没有则全为 -1。
func extent(img image.Image, x, y, w, h int, pred func(image.Image, int, int) bool) (top, bottom, left, right int) {
	top, bottom, left, right = -1, -1, -1, -1
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if !pred(img, xx, yy) {
				continue
			}
			if top < 0 {
				top = yy
			}
			bottom = yy
			if left < 0 || xx < left {
				left = xx
			}
			right = max(right, xx)
		}
	}
	return
}

// 并排画两列：左边是标签高度恰好等于行高 16 的 16px「🌊」对照，右边是 emojiGlyph；每列底下各放一条 2px 的红色标记条，
// 用来比两列的布局高度。「🌊」在 Segoe UI Emoji 的 16px 下内容区 20px，底边超出行盒 0.79px（渲染层实测），
// 所以行盒之外的第 16 行（行盒占 0…15 行）有 79% 覆盖的墨迹。
func probeEmojiColumns(t *testing.T) (controlBottom, glyphBottom, controlBar, glyphBar int) {
	t.Helper()
	img := renderEmoji(t, 160, 80, func(c ui.C, s *Screen, look *kit.Look) {
		bar := func(key string) {
			ui.Row(c.Key(key)).FillWidth().Height(2).MarginTop(6).Background(gfx.RGBA(255, 0, 0, 1)) // 与字形隔开 6px，免得盖住超出行盒的那一行
		}
		ui.Column(c.Key("control")).Width(40).Children(func() {
			look.TextPx(c.Key("control-text"), "🌊", 16, 16, kit.WeightNormal, look.T.Foreground)
			bar("control-bar")
		})
		ui.Column(c.Key("glyph")).Width(40).Children(func() {
			s.emojiGlyph(c.Key("glyph-emoji"), "🌊", 16, 0, look.T.Foreground)
			bar("glyph-bar")
		})
	})
	const top, h = emojiProbeTop - 6, 44
	_, controlBottom, _, _ = extent(img, emojiProbeTop, top, 40, h, inked)
	_, glyphBottom, _, _ = extent(img, emojiProbeTop+70, top, 40, h, inked)
	controlBar, _, _, _ = extent(img, emojiProbeTop, top, 40, h, isBar)
	glyphBar, _, _, _ = extent(img, emojiProbeTop+70, top, 40, h, isBar)
	return
}

// 引擎事实：标签只画自己的矩形，对照（标签高度 = 行高）把 🌊 的第 16 行裁掉。Viva 升级后若这条先红，说明引擎不再裁，
// 可以删掉 emojiOverflow 与本文件的另两条。
func TestVivaClipsGlyphOverflowAtTheLabelRectangle(t *testing.T) {
	controlBottom, _, _, _ := probeEmojiColumns(t)
	if rel := controlBottom - emojiProbeTop; rel < 0 || rel > 15 {
		t.Fatalf("对照的最后墨迹行（相对行盒顶）= %d，期望落在行盒内（0…15）", rel)
	}
}

// emojiGlyph 把标签放高，超出行盒的那一行画得出来，与渲染层一致（渲染层第 16 行有 79% 覆盖）。
func TestEmojiGlyphIsNotClippedAtTheLineBox(t *testing.T) {
	controlBottom, glyphBottom, _, _ := probeEmojiColumns(t)
	t.Logf("最后墨迹行（相对行盒顶）：对照 %d，emojiGlyph %d", controlBottom-emojiProbeTop, glyphBottom-emojiProbeTop)
	if rel := glyphBottom - emojiProbeTop; rel < 16 {
		t.Errorf("emojiGlyph 的最后墨迹行 = %d，期望越过行盒（≥ 16）", rel)
	}
	if glyphBottom > controlBottom+2 {
		t.Errorf("emojiGlyph 的最后墨迹行比对照多出 %d 行，位置可能被挪动了", glyphBottom-controlBottom)
	}
}

// 标签放高靠的是上下各 −4px 的外边距：布局高度与基线位置都不变，列底下的标记条与对照在同一行。
func TestEmojiGlyphKeepsTheLayoutHeight(t *testing.T) {
	_, _, controlBar, glyphBar := probeEmojiColumns(t)
	if controlBar < 0 || glyphBar < 0 {
		t.Fatalf("没找到标记条：对照 %d，emojiGlyph %d", controlBar, glyphBar)
	}
	if glyphBar != controlBar {
		t.Errorf("emojiGlyph 一列的高度与对照不同：标记条在第 %d 行，对照在第 %d 行", glyphBar-emojiProbeTop, controlBar-emojiProbeTop)
	}
}

// 字形比容器宽（侧栏行的图标槽 1rem 放 0.875rem 的表情）：CSS 居中让字形向两侧各溢出一半，起点比槽的左缘靠左约 1.6px；
// Viva 的居中把负剩余空间当 0，溢出全落右边——emojiGlyph 用负外边距补上。以不放进槽里的同一枚字形作参照量起点的位移。
func TestEmojiGlyphOverflowsASmallSlotOnBothSides(t *testing.T) {
	const size, slot = 14, 16
	img := renderEmoji(t, 160, 60, func(c ui.C, s *Screen, look *kit.Look) {
		ui.Row(c.Key("free")).Width(40).Children(func() {
			s.emojiGlyph(c.Key("free-emoji"), "📚", size, 0, look.T.Foreground)
		})
		ui.Row(c.Key("slot")).Width(slot).Justify(ui.Center).Children(func() {
			s.emojiGlyph(c.Key("slot-emoji"), "📚", size, slot, look.T.Foreground)
		})
	})
	const x0, y0, h = emojiProbeTop, emojiProbeTop - 4, 36
	_, _, freeLeft, _ := extent(img, x0-8, y0, 48, h, inked)
	_, _, slotLeft, _ := extent(img, x0+70-8, y0, 48, h, inked)
	if freeLeft < 0 || slotLeft < 0 {
		t.Fatalf("没找到字形墨迹：自由 %d，槽内 %d", freeLeft, slotLeft)
	}
	// 自由摆放起点在 x0；槽内起点在 x0 + 70 + (slot − advance)/2，其中 advance = 14 × 1.3736 ≈ 19.23，即 70 + x0 − 1.6。
	shift := float64(slotLeft-(x0+70)) - float64(freeLeft-x0)
	if shift > -1 || shift < -2.5 {
		t.Errorf("槽内字形起点相对自由摆放的位移 = %.1f，期望约 −1.6（溢出一半落在左边）", shift)
	}
}
