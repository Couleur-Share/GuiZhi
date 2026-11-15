package kit

import (
	"encoding/json"
	"maps"
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
)

// ColumnDef 是表格一列的定义与当前偏好。
//
// 只有「是否显示」和「宽度」是用户的：最小/最大宽度、是否可调永远以代码里的定义为准，
// 旧版本写下的脏配置不能把表格搞坏（与渲染层 item-table-config.ts 一致）。
type ColumnDef struct {
	ID        string
	Visible   bool
	Width     float32
	MinWidth  float32
	MaxWidth  float32 // 0 表示没有上限
	Resizable bool
}

// Clamp 把宽度夹进这一列的范围；非有限数取默认宽度。
func (d ColumnDef) Clamp(w float32) float32 { return d.clamp(float64(w)) }

func (d ColumnDef) clamp(w float64) float32 {
	if math.IsNaN(w) || math.IsInf(w, 0) {
		return d.Width
	}
	upper := math.MaxFloat32
	if d.MaxWidth > 0 {
		upper = float64(d.MaxWidth)
	}
	return float32(min(max(w, float64(d.MinWidth)), upper))
}

// columnPref 是保存下来的一列：只有 id、可见与宽度。
// 形状与渲染层 localStorage 里的 guizhi-library-table-config 一致（{"columns":[{"id","visible","width"}]}），
// 首次启动导入旧设置时可以原样搬过来。
type columnPref struct {
	ID      string  `json:"id"`
	Visible bool    `json:"visible"`
	Width   float64 `json:"width"`
}

type columnPrefs struct {
	Columns []columnPref `json:"columns"`
}

// MergeStoredColumns 把保存下来的偏好合并进默认定义：未知列 id 丢弃；
// visible 不是布尔、width 不是有限数时沿用默认值；宽度夹进该列范围。保存里没有的列保持默认。
// 返回的切片是新的，defs 不被修改。
func MergeStoredColumns(defs []ColumnDef, stored json.RawMessage) []ColumnDef {
	out := slices.Clone(defs)
	if len(stored) == 0 {
		return out
	}
	// 逐项宽松解析：任何一项类型不对只影响那一项，不让整份配置作废。
	// 元素与字段都按动态类型取（不是对象的元素、null、字符串都不算布尔/数字），与渲染层的 typeof 判断一致。
	var doc struct {
		Columns []any `json:"columns"`
	}
	if err := json.Unmarshal(stored, &doc); err != nil {
		return out
	}
	for i := range out {
		for _, raw := range doc.Columns {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := item["id"].(string); id != out[i].ID {
				continue
			}
			if visible, ok := item["visible"].(bool); ok {
				out[i].Visible = visible
			}
			// encoding/json 解出的数字不会是 NaN 或无穷（那样的 JSON 不合法），不用再判
			if width, ok := item["width"].(float64); ok {
				out[i].Width = out[i].clamp(width)
			}
			break
		}
	}
	return out
}

// ColumnSet 是一张表格的列偏好：当前各列、切换显示、改宽度、恢复默认，改动去抖保存。
// 只在 UI 线程读写。
//
// 偏好（ColumnSet）是真相源；ui.Table 在 ListState.Columns 里另存一份用户拖出来的布局，
// 每帧由 Sync 把布局读回偏好。代码改了偏好（Resize、Reset）之后，布局就过时了：
// 下一次 Sync 以偏好为准重写布局，否则表格会继续按旧布局画，还把旧宽度当成用户新拖的写回偏好。
type ColumnSet struct {
	key   string
	defs  []ColumnDef
	cols  []ColumnDef
	store PrefStore
	save  saver
	// layoutStale 表示偏好被代码改过，表格保存的布局已过时，见类型说明。
	layoutStale bool
}

// NewColumnSet 读取 key 下保存的偏好并合并进 defs；store 为 nil 时只在内存里记。
//
// 默认宽度也夹进该列的范围：渲染层的操作列默认 84、下限 148，界面上实际是 148；
// 若保存 84，下次读回时按下限夹成 148，同一列前后两个值。统一用实际显示的值，读写才稳定。
func NewColumnSet(key string, defs []ColumnDef, store PrefStore) *ColumnSet {
	defs = slices.Clone(defs)
	for i := range defs {
		defs[i].Width = defs[i].Clamp(defs[i].Width)
	}
	s := &ColumnSet{key: key, defs: defs, store: store}
	var raw json.RawMessage
	if store != nil {
		raw, _ = store.Get(key)
	}
	s.cols = MergeStoredColumns(s.defs, raw)
	return s
}

// Columns 返回全部列（含隐藏的，按声明顺序）；是副本。
func (s *ColumnSet) Columns() []ColumnDef { return slices.Clone(s.cols) }

// Visible 返回当前显示的列，声明顺序。
func (s *ColumnSet) Visible() []ColumnDef {
	var out []ColumnDef
	for _, c := range s.cols {
		if c.Visible {
			out = append(out, c)
		}
	}
	return out
}

func (s *ColumnSet) index(id string) int {
	return slices.IndexFunc(s.cols, func(c ColumnDef) bool { return c.ID == id })
}

// Toggle 切换一列的显示。未知 id 什么都不做。
func (s *ColumnSet) Toggle(id string) {
	if i := s.index(id); i >= 0 {
		s.cols[i].Visible = !s.cols[i].Visible
		s.persist()
	}
}

// Resize 由代码设置一列的宽度（夹进该列范围）；不可调的列与未知 id 什么都不做。
// 用户拖动改的宽度不走这里，由 Sync 读回。
func (s *ColumnSet) Resize(id string, w float32) {
	if s.setWidth(id, w) {
		s.layoutStale = true
	}
}

// setWidth 改宽度并保存，返回宽度是否真的变了。
func (s *ColumnSet) setWidth(id string, w float32) bool {
	i := s.index(id)
	if i < 0 || !s.cols[i].Resizable {
		return false
	}
	w = s.cols[i].Clamp(w)
	if w == s.cols[i].Width {
		return false
	}
	s.cols[i].Width = w
	s.persist()
	return true
}

// Reset 恢复默认的显示与宽度（保存下来的偏好随之回到默认）。
func (s *ColumnSet) Reset() {
	s.cols = slices.Clone(s.defs)
	s.layoutStale = true
	s.persist()
}

// Flush 立即保存还没写出去的改动；窗口关闭前调用。
func (s *ColumnSet) Flush() { s.save.flush() }

func (s *ColumnSet) persist() {
	if s.store == nil {
		return
	}
	doc := columnPrefs{Columns: make([]columnPref, len(s.cols))}
	for i, c := range s.cols {
		doc.Columns[i] = columnPref{ID: c.ID, Visible: c.Visible, Width: float64(c.Width)}
	}
	store, key := s.store, s.key
	s.save.schedule(func() error { return store.Set(key, doc) })
}

// TableColumns 把显示中的列转成 ui.Table 的列：不可调的列 Fixed（不挪不改宽）。
// title 给出列的表头文字。
func (s *ColumnSet) TableColumns(title func(id string) string) []ui.TableColumn {
	vis := s.Visible()
	out := make([]ui.TableColumn, len(vis))
	for i, c := range vis {
		out[i] = ui.TableColumn{
			ID: c.ID, Title: title(c.ID),
			Width: c.Width, MinWidth: c.MinWidth, MaxWidth: c.MaxWidth,
			Fixed: !c.Resizable,
		}
	}
	return out
}

// layoutWidths 是按偏好生成的表格宽度布局：每个可调列一项。
func (s *ColumnSet) layoutWidths() map[string]float32 {
	out := make(map[string]float32, len(s.cols))
	for _, c := range s.cols {
		if c.Resizable {
			out[c.ID] = c.Width
		}
	}
	return out
}

// Sync 在每帧 Table 构建之后调用，把表格的布局和偏好对齐；返回 true 表示改了布局、
// 需要再画一帧（调用方 c.Invalidate()），否则这一帧画出来的还是旧布局。
//
//   - 偏好刚被代码改过（Resize、Reset）：以偏好为准重写布局。
//   - 否则把用户拖出的列宽读回偏好（夹紧、去抖保存）。
//   - 清掉列顺序：渲染层的列顺序固定为声明顺序，不允许拖动调换；表头被拖动调换后
//     layout.Order 非空，清空后列回到原位。
func (s *ColumnSet) Sync(layout *ui.TableLayout) (redraw bool) {
	if layout == nil {
		return false
	}
	if s.layoutStale {
		s.layoutStale = false
		layout.Order = nil
		layout.Widths = s.layoutWidths()
		return true
	}
	if len(layout.Order) > 0 {
		layout.Order = nil
		redraw = true
	}
	// 夹紧后与布局里不一致的宽度要回写，否则布局与偏好各存一份、互相打架。
	// 不能原地改 layout.Widths：ui.Table 靠「布局变化时换新的切片与映射」来判断一次拖动改没改布局，
	// 原地改会让它拿着的拖动起点一起变掉。所以换一个新映射。
	var fixed map[string]float32
	for id, w := range layout.Widths {
		s.setWidth(id, w)
		if i := s.index(id); i >= 0 && s.cols[i].Width != w {
			if fixed == nil {
				fixed = maps.Clone(layout.Widths)
			}
			fixed[id] = s.cols[i].Width
		}
	}
	if fixed != nil {
		layout.Widths = fixed
	}
	return redraw
}
