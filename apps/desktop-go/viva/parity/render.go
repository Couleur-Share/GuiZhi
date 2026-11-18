package parity

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"sync"

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
	// Steps 可选：画面首次稳定后、采集前，像用户那样操作界面，把它带到要对照的状态（滚动、点开下拉……）。
	// 与渲染层场景的 enter 一一对应；做完后会让动画走完、画面再次稳定。
	Steps func(s *headless.Session) error
	// Prepare 可选：一次渲染的页面与操作步骤由它一起装配，非空时替代 Build 与 Steps。
	// 操作步骤要用到装配页面时新建的状态（Store 之类）时用它：同一个 Scene 值会被并行渲染多次（单元测试里每个样本一次），
	// 状态存在场景值上就会串线；Prepare 每次渲染新建自己的一份，步骤闭包拿的是自己那份。
	// Build 仍须可用（模糊背景的「对话框打开之前」渲染只走 Build，见 BackdropSpec.BaseSteps）。
	Prepare func(look *kit.Look) (view func(c ui.C), steps func(s *headless.Session) error)
	// Spec 可选：场景自己的外观，输入是变体的外观。渲染层场景在操作中会改变主题（选「自定义」会按十六进制重新取色相与饱和度），
	// 对照的是改变之后的画面，所以这类场景用它把变体的外观换成改变之后的样子。
	Spec func(base kit.Spec) kit.Spec
	// Waivers 是这个场景已在台账登记的偏差（见 waiver.go）。
	Waivers []Waiver
	// Backdrop 非空：场景里有被遮罩背后模糊的区域（模态对话框，台账 A-007），对照时按它模拟模糊（见 backdrop.go）。
	Backdrop *BackdropSpec
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

// fontOnce 保证界面字体族只设置一次：它写的是进程内共享的文字引擎，字体族又是常量，
// 反复设置没有意义，还会让并行跑的渲染互相踩。
var fontOnce sync.Once

// Render 用 Viva 把场景在变体下渲染成 region 大小的无头画布（缩放 1:1），返回同形的事实与图片。
func Render(s Scene, v Variant, region Region) (*Facts, image.Image, error) {
	return render(s, v, region, true)
}

// RenderFacts 与 Render 相同，但不光栅化成图片：单元测试只比较 L1/L2 事实，软件光栅化一整帧是出帧之外最大的开销。
func RenderFacts(s Scene, v Variant, region Region) (*Facts, error) {
	facts, _, err := render(s, v, region, false)
	return facts, err
}

func render(s Scene, v Variant, region Region, wantImage bool) (*Facts, image.Image, error) {
	vw, _, err := v.Size()
	if err != nil {
		return nil, nil, err
	}
	fontOnce.Do(func() { app.SetInterfaceFontFamily(interfaceFont) })
	spec := v.Spec()
	if s.Spec != nil {
		spec = s.Spec(spec)
	}
	look := kit.NewLook(spec)
	look.Viewport = float32(vw)
	look.Scale = 1
	background := look.Col(look.T.Background)
	var view func(c ui.C)
	steps := s.Steps
	if s.Prepare != nil {
		view, steps = s.Prepare(look)
	} else {
		view = s.Build(look)
	}
	// 与窗口一致：每次绘制开始登记 Tab 停靠点，按键经 TabKey（Tab 可达与聚焦跟随滚动，台账 B-030）。
	declare := view
	view = func(c ui.C) {
		look.BeginPaint()
		declare(c)
	}
	page := ui.NewPage(ui.PageConfig{View: view, Theme: &look.Theme, Background: &background})
	var canvas gfx.Canvas
	session, err := headless.Open(
		page.Config(app.Config{Title: s.ID, Width: region.Width, Height: region.Height, OnKey: look.TabKey}),
		&canvas, headless.Options{Scale: 1})
	if err != nil {
		return nil, nil, fmt.Errorf("打开无头会话失败：%w", err)
	}
	defer session.Close()
	if err := settle(session); err != nil {
		return nil, nil, err
	}
	if steps != nil {
		if err := steps(session); err != nil {
			return nil, nil, fmt.Errorf("场景 %s 的操作步骤失败：%w", s.ID, err)
		}
		// 动画走完（虚拟时钟推进到没有转场为止），再等画面稳定。
		if err := session.Rest(); err != nil {
			return nil, nil, fmt.Errorf("等待动画结束失败：%w", err)
		}
		if err := settle(session); err != nil {
			return nil, nil, err
		}
	}
	// 再出一帧专门记录图标外框（画面已稳定，这一帧与上一帧相同）。
	var icons, overlayIcons []Box
	look.IconSink = func(ic kit.IconDraw) {
		// 滚出视口的图标也会经过绘制回调；渲染层的采集只收与区域相交的元素，这里同样只收画面里看得见的图标
		// （被滚动区域等祖先裁掉的，kit 在报告之前已经滤掉了）。
		r := ic.Rect
		if r.X+r.Width <= 0 || r.Y+r.Height <= 0 || r.X >= float32(region.Width) || r.Y >= float32(region.Height) {
			return
		}
		box := Box{float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height)}
		icons = append(icons, box)
		if ic.Overlay {
			overlayIcons = append(overlayIcons, box)
		}
	}
	if err := session.Frame(); err != nil {
		return nil, nil, fmt.Errorf("出帧失败：%w", err)
	}
	look.IconSink = nil
	facts := collectViva(session, s.ID, v, region)
	facts.Icons, facts.OverlayIcons = icons, overlayIcons
	if !wantImage {
		return facts, nil, nil
	}
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

// layoutDigest 把当前画面的文字与图元外框、颜色串成一个摘要，供 settle 判断画面是否停住。
// 用 strings.Builder 线性拼接：采集队列页有几千个图元，逐个 s += 会让整个测试集多花近一半时间（O(n²) 的内存拷贝）。
func layoutDigest(session *headless.Session) string {
	snap := session.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%d|", len(snap.Primitives), len(snap.Texts))
	for _, t := range snap.Texts {
		fmt.Fprintf(&b, "%v;", t.Bounds)
	}
	for _, p := range snap.Primitives {
		fmt.Fprintf(&b, "%v%v;", p.Bounds, p.Color)
	}
	return b.String()
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
		if t.Style.Color.Alpha < kit.VisibleInkAlpha {
			// 透明的文字看不见，不是「呈现出来的文字」：kit.Input 画大写叠层时把输入框自己的字设成透明，
			// 看得见的是叠层画的那一份（React 一侧采集的也是 text-transform 之后的字）。
			continue
		}
		painted := float64(t.Bounds.Width)
		if i < len(snap.TextWidths) && snap.TextWidths[i] > 0 {
			painted = float64(snap.TextWidths[i])
		} else if t.Style.Mono && t.CellWidth > 0 {
			// 固定间距文字 Viva 不量宽度（按字格排）：宽度是字格数乘字格宽。输入框里的文字，文字盒是整个内容区，
			// 比文字本身宽，必须按字格算，否则「绘制宽度」成了输入框的宽度。
			cells := 0
			for _, r := range t.Value {
				cells++
				if kit.IsWideRune(r) {
					cells++
				}
			}
			painted = math.Min(painted, float64(cells)*float64(t.CellWidth))
		}
		// 文字被裁剪矩形截掉的右半段不算「呈现出来」：React 一侧同样只记看得见的部分（输入框的内容盒、
		// 溢出隐藏的祖先），两边都按可见宽度比较；否则一段被裁掉的占位文字会让遮罩与墨迹区域越出输入框。
		if t.HasClip {
			if right := float64(t.ClipOf.X + t.ClipOf.Width); float64(t.Bounds.X)+painted > right {
				painted = math.Max(right-float64(t.Bounds.X), 0)
			}
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
			Overlay:  t.Layer >= kit.OverlayLayerMin,
			// Snapshot.Texts 是裁剪之前画过的所有文字；滚出滚动容器的文字不在 Labels（真正画出来的）里。
			// 标成 Hidden：比较时它不算「呈现出来的文字」，但仍可补全一段折行文字的原文（口径见台账 A-010）。
			Hidden: !isDrawn(snap.Labels, t),
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

// isDrawn 报告 text 是否真的被画出来了：labels 里有同样内容、位置相差不超过 1 个逻辑像素
// （绘制时位置会取整）的一行。被裁剪得一点不剩的文字不会成为 label。
func isDrawn(labels []app.Label, text gfx.Text) bool {
	for _, l := range labels {
		if l.Value == text.Value &&
			math.Abs(float64(l.Bounds.X-text.Bounds.X)) <= 1 && math.Abs(float64(l.Bounds.Y-text.Bounds.Y)) <= 1 {
			return true
		}
	}
	return false
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
	switch {
	case n.Role == gfx.RoleOption || n.Role == gfx.RoleTab:
		// 选项与页签的「选中」是固有状态：渲染层始终写 aria-selected（true/false），
		// Viva 的无障碍节点同样始终带这个状态位，所以这两种角色未选中也写 "false"。
		props["selected"] = strconv.FormatBool(n.Selected)
	case n.Selected:
		props["selected"] = "true"
	}
	if n.Disabled {
		props["disabled"] = "true"
	}
	if n.Level > 0 {
		props["level"] = float64(n.Level)
	}
	value := n.Value
	if n.Range.Set {
		// 滑块、分隔条的数值：与渲染层的 aria-valuenow / valuemin / valuemax 对应。
		value = strconv.FormatFloat(n.Range.Now, 'f', -1, 64)
		props["valuemin"] = strconv.FormatFloat(n.Range.Min, 'f', -1, 64)
		props["valuemax"] = strconv.FormatFloat(n.Range.Max, 'f', -1, 64)
	}
	return Node{
		Role: n.Role.String(), Name: n.Label, Value: value, Description: n.Description, Props: props,
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
