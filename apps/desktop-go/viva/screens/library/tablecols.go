package library

import (
	"encoding/json"
	"math"

	"guizhi-mygo-shell/viva/locales"
)

// 列表视图（全宽表格）的列定义与偏好（渲染层 components/library/item-table-config.ts）。
// 只把「是否显示」和「列宽」交给用户，其余属性（最小/最大宽度、吸附侧、是否可调）永远以代码里的默认定义为准，
// 避免旧版本写入的脏配置把表格搞坏。偏好的形状与旧 localStorage 一致：{"columns":[{id,visible,width}]}。

// columnID 是表格列的标识（与渲染层的 LIBRARY_COLUMN_IDS 同名，也是偏好里的 id）。
type columnID string

const (
	colCheckbox   columnID = "checkbox"
	colTitle      columnID = "title"
	colSnippet    columnID = "snippet"
	colTags       columnID = "tags"
	colType       columnID = "type"
	colSource     columnID = "source"
	colCollection columnID = "collection"
	colStatus     columnID = "status"
	colCreatedAt  columnID = "createdAt"
	colUpdatedAt  columnID = "updatedAt"
	colActions    columnID = "actions"
)

// stickySide 是横向滚动时列吸附的一侧。
type stickySide uint8

const (
	stickyNone stickySide = iota
	stickyLeft
	stickyRight
)

// tableColumn 是一列的定义。
type tableColumn struct {
	ID    columnID
	Label func() string
	// Visible 与 Width 是用户可改的两项；其余是代码里的固定定义。
	Visible   bool
	Width     float32
	MinWidth  float32
	MaxWidth  float32 // 0 表示不设上限
	Resizable bool
	Sticky    stickySide
	Centered  bool
}

// defaultColumns 是默认的列（渲染层 DEFAULT_LIBRARY_COLUMNS）；声明顺序天然满足「左吸附 → 常规列 → 右吸附」。
func defaultColumns() []tableColumn {
	return []tableColumn{
		{ID: colCheckbox, Label: locales.CommonSelect, Visible: true, Width: 44, MinWidth: 44, Sticky: stickyLeft},
		{ID: colTitle, Label: locales.LibraryColumnTitle, Visible: true, Width: 260, MinWidth: 150, MaxWidth: 480, Resizable: true},
		{ID: colSnippet, Label: locales.LibraryColumnSnippet, Visible: true, Width: 200, MinWidth: 120, MaxWidth: 640, Resizable: true},
		{ID: colTags, Label: locales.LibraryColumnTags, Visible: true, Width: 120, MinWidth: 90, MaxWidth: 320, Resizable: true},
		{ID: colType, Label: locales.LibraryColumnType, Visible: true, Width: 84, MinWidth: 72, MaxWidth: 160, Resizable: true, Centered: true},
		// 108 而不是 96：多了平台 logo 那 14px，96 会让「哔哩哔哩」默认就被截断。
		{ID: colSource, Label: locales.LibraryColumnSource, Visible: true, Width: 108, MinWidth: 80, MaxWidth: 180, Resizable: true, Centered: true},
		{ID: colCollection, Label: locales.LibraryColumnCollection, Width: 140, MinWidth: 100, MaxWidth: 280, Resizable: true},
		{ID: colStatus, Label: locales.LibraryColumnStatus, Width: 96, MinWidth: 84, MaxWidth: 180, Resizable: true, Centered: true},
		{ID: colCreatedAt, Label: locales.LibraryColumnCreatedAt, Width: 116, MinWidth: 100, MaxWidth: 220, Resizable: true},
		{ID: colUpdatedAt, Label: locales.LibraryColumnUpdatedAt, Visible: true, Width: 116, MinWidth: 100, MaxWidth: 220, Resizable: true},
		// 操作列：默认宽 84，MinWidth 148 只在夹取已存的宽度时起作用（列不可调，渲染层同）。
		{ID: colActions, Label: locales.LibraryColumnActions, Visible: true, Width: 84, MinWidth: 148, Sticky: stickyRight},
	}
}

// clampColumnWidth 把宽度夹在列的上下限里；不是有限数字时取列的默认宽度（渲染层 clampColumnWidth）。
func clampColumnWidth(col tableColumn, width float64) float32 {
	if math.IsNaN(width) || math.IsInf(width, 0) {
		return col.Width
	}
	upper := math.Inf(1)
	if col.MaxWidth > 0 {
		upper = float64(col.MaxWidth)
	}
	return float32(math.Min(math.Max(width, float64(col.MinWidth)), upper))
}

// mergeStoredColumns 把已存的偏好合并进默认列定义（渲染层 mergeStoredColumns）：未知列 id 丢弃，
// 缺少的列保持默认，字段类型不对的取默认；整个偏好坏了（不是对象、columns 不是数组）就用默认。
func mergeStoredColumns(stored json.RawMessage) []tableColumn {
	cols := defaultColumns()
	var doc struct {
		Columns any `json:"columns"`
	}
	if len(stored) == 0 || json.Unmarshal(stored, &doc) != nil {
		return cols
	}
	saved, ok := doc.Columns.([]any)
	if !ok {
		return cols
	}
	for i, col := range cols {
		for _, item := range saved {
			entry, ok := item.(map[string]any)
			if !ok || entry["id"] != string(col.ID) {
				continue
			}
			if visible, ok := entry["visible"].(bool); ok {
				cols[i].Visible = visible
			}
			if width, ok := entry["width"].(float64); ok {
				cols[i].Width = clampColumnWidth(col, width)
			}
			break
		}
	}
	return cols
}

// columnsPayload 是写进偏好的内容：只存 id、是否显示与宽度。
func columnsPayload(cols []tableColumn) map[string]any {
	items := make([]map[string]any, 0, len(cols))
	for _, col := range cols {
		items = append(items, map[string]any{"id": string(col.ID), "visible": col.Visible, "width": float64(col.Width)})
	}
	return map[string]any{"columns": items}
}

// visibleColumns 是要画的列，保持声明顺序。
func visibleColumns(cols []tableColumn) []tableColumn {
	out := make([]tableColumn, 0, len(cols))
	for _, col := range cols {
		if col.Visible {
			out = append(out, col)
		}
	}
	return out
}

// columnsMinWidth 是可见列的宽度之和（渲染层 tableMinWidth）：表格的最小宽度。
func columnsMinWidth(cols []tableColumn) float32 {
	var sum float32
	for _, col := range cols {
		sum += col.Width
	}
	return sum
}
