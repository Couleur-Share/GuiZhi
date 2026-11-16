package parity

import (
	"fmt"
	"image"
	"math"
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// Scene 是一个可对照的屏幕：ID 与 React 采集器里的场景 ID 相同。
type Scene struct {
	ID string
	// Build 按装配好的外观返回页面函数。场景自带它需要的状态（例如内存里的设置存取），
	// 每次渲染都应从初始状态开始，保证对照是确定的。
	Build func(look *kit.Look) func(c ui.C)
	// Waivers 是这个场景已在台账登记的偏差（见 waiver.go）。
	Waivers []Waiver
}

// Spec 把变体换成外观装配的输入。
func (v Variant) Spec() kit.Spec {
	p := tokens.FindHuePreset(v.Hue)
	mode := tokens.ModeLight
	if v.Mode == "dark" {
		mode = tokens.ModeDark
	}
	return kit.Spec{Mode: mode, Hue: p.Hue, Saturation: p.Saturation, FontSize: v.Font}
}

// interfaceFont 是界面字体族：渲染层的 font-family 里 system-ui 在中文 Windows 上解析成它。
const interfaceFont = "Microsoft YaHei UI"

// settleFrames 是渲染到画面稳定的帧数上限；滚动槽、转场等要靠前一帧的版面结果，一帧不够。
const settleFrames = 12

// Render 用 Viva 把场景在变体下渲染成 region 大小的无头画布（缩放 1:1），返回同形的事实与图片。
func Render(s Scene, v Variant, region Region) (*Facts, image.Image, error) {
	vw, _, err := v.Size()
	if err != nil {
		return nil, nil, err
	}
	app.SetInterfaceFontFamily(interfaceFont)
	look := kit.NewLook(v.Spec())
	look.Viewport = float32(vw)
	look.Scale = 1
	background := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{View: s.Build(look), Theme: &look.Theme, Background: &background})
	var canvas gfx.Canvas
	session, err := headless.Open(
		page.Config(app.Config{Title: s.ID, Width: region.Width, Height: region.Height}),
		&canvas, headless.Options{Scale: 1})
	if err != nil {
		return nil, nil, fmt.Errorf("打开无头会话失败：%w", err)
	}
	defer session.Close()
	if err := settle(session); err != nil {
		return nil, nil, err
	}
	// 再出一帧专门记录图标外框（画面已稳定，这一帧与上一帧相同）。
	var icons []Box
	look.IconSink = func(r gfx.Rect) {
		// 滚出视口的图标也会经过绘制回调；渲染层的采集只收与区域相交的元素，这里同样只收画面里看得见的图标。
		if r.X+r.Width <= 0 || r.Y+r.Height <= 0 || r.X >= float32(region.Width) || r.Y >= float32(region.Height) {
			return
		}
		icons = append(icons, Box{float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height)})
	}
	if err := session.Frame(); err != nil {
		return nil, nil, fmt.Errorf("出帧失败：%w", err)
	}
	look.IconSink = nil
	facts := collectViva(session, s.ID, v, region)
	facts.Icons = icons
	img, err := session.Image()
	if err != nil {
		return nil, nil, fmt.Errorf("取图失败：%w", err)
	}
	return facts, img, nil
}

// settle 反复出帧，直到连续两帧的版面摘要相同。
func settle(session *headless.Session) error {
	prev := ""
	same := 0
	for i := 0; i < settleFrames; i++ {
		if err := session.Frame(); err != nil {
			return fmt.Errorf("出帧失败：%w", err)
		}
		cur := layoutDigest(session)
		if i > 0 && cur == prev {
			same++
			if same >= 2 {
				return nil
			}
		} else {
			same = 0
		}
		prev = cur
	}
	return fmt.Errorf("画面在 %d 帧内没有稳定", settleFrames)
}

func layoutDigest(session *headless.Session) string {
	snap := session.Snapshot()
	s := fmt.Sprintf("%d|%d|", len(snap.Primitives), len(snap.Texts))
	for _, t := range snap.Texts {
		s += fmt.Sprintf("%v;", t.Bounds)
	}
	for _, p := range snap.Primitives {
		s += fmt.Sprintf("%v%v;", p.Bounds, p.Color)
	}
	return s
}

// collectViva 把无头会话当前的画面整理成事实：可访问节点、文字、绘制图元。
func collectViva(session *headless.Session, scene string, v Variant, region Region) *Facts {
	snap := session.Snapshot()
	f := &Facts{
		Schema: SchemaVersion, Scene: scene, Variant: v, Region: region,
		Capture: Capture{DPR: 1, Engine: "viva v0.6.4"},
	}
	for _, n := range session.Access() {
		f.Nodes = append(f.Nodes, nodeOf(n))
	}
	for i, t := range snap.Texts {
		painted := float64(t.Bounds.Width)
		if i < len(snap.TextWidths) && snap.TextWidths[i] > 0 {
			painted = float64(snap.TextWidths[i])
		}
		rows := int(math.Ceil(float64(t.Bounds.Height) - 1e-6))
		baseline := float64(t.Bounds.Y) + float64(kit.VivaBaselineRow(rows, float64(t.Style.Size)))
		f.Texts = append(f.Texts, Text{
			Text:     t.Value,
			Rects:    []Box{{float64(t.Bounds.X), float64(t.Bounds.Y), painted, float64(t.Bounds.Height)}},
			Size:     float64(t.Style.Size),
			Weight:   cssWeight(t.Style.Weight),
			Color:    cssColor(t.Style.Color),
			Painted:  []float64{painted},
			Baseline: []float64{baseline},
		})
		f.TextRects = append(f.TextRects, f.Texts[len(f.Texts)-1].Rects...)
	}
	for i, p := range snap.Primitives {
		switch p.Kind {
		case app.RectanglePrimitive:
			f.Paints = append(f.Paints, Paint{
				Order: i, Tag: "rect", Opacity: 1,
				Box: Box{float64(p.Bounds.X), float64(p.Bounds.Y), float64(p.Bounds.Width), float64(p.Bounds.Height)},
				Bg:  cssColor(p.Color), Radius: []string{fmt.Sprintf("%gpx", p.Radius)},
			})
		case app.ShadowPrimitive:
			f.Paints = append(f.Paints, Paint{
				Order: i, Tag: "shadow", Opacity: 1,
				Box:    Box{float64(p.Bounds.X), float64(p.Bounds.Y), float64(p.Bounds.Width), float64(p.Bounds.Height)},
				Shadow: fmt.Sprintf("%s %gpx %gpx %gpx %gpx", cssColor(p.Color), p.Shadow.OffsetX, p.Shadow.OffsetY, p.Shadow.Blur, p.Shadow.Spread),
				Radius: []string{fmt.Sprintf("%gpx", p.Radius)},
			})
		}
	}
	return f
}

// nodeOf 把 Viva 的可访问节点写成与 React 同形的 Node，状态用字符串（"true"/"false"），与 Node.Prop 的口径一致。
func nodeOf(n app.AccessNode) Node {
	props := map[string]any{"focusable": strconv.FormatBool(n.Focusable)}
	switch n.Role {
	case gfx.RoleSwitch, gfx.RoleCheckbox, gfx.RoleRadio:
		props["checked"] = strconv.FormatBool(n.Checked)
	}
	if n.Expandable {
		props["expanded"] = strconv.FormatBool(n.Expanded)
	}
	if n.Selected {
		props["selected"] = "true"
	}
	if n.Disabled {
		props["disabled"] = "true"
	}
	if n.Level > 0 {
		props["level"] = float64(n.Level)
	}
	return Node{
		Role: n.Role.String(), Name: n.Label, Value: n.Value, Props: props,
		Box:    []float64{float64(n.Bounds.X), float64(n.Bounds.Y), float64(n.Bounds.Width), float64(n.Bounds.Height)},
		Parent: -1,
	}
}

// cssWeight 把 Viva 的字重刻度换回 CSS 字重（刻度见 kit/text.go）。
func cssWeight(w float32) float64 {
	switch {
	case w >= 0.35:
		return 700
	case w >= 0.2:
		return 600
	case w >= 0.1:
		return 500
	default:
		return 400
	}
}

func cssColor(c gfx.Color) string {
	to8 := func(v float32) int { return int(math.Round(float64(v) * 255)) }
	if c.Alpha >= 0.999 {
		return fmt.Sprintf("rgb(%d, %d, %d)", to8(c.Red), to8(c.Green), to8(c.Blue))
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", to8(c.Red), to8(c.Green), to8(c.Blue), strconv.FormatFloat(float64(c.Alpha), 'f', -1, 32))
}
