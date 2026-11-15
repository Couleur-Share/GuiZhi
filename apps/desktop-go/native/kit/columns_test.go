package kit

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// testDefs 照抄渲染层 item-table-config.ts 里几类有代表性的列：
// 固定的左列、可调且有上下限的列、默认隐藏的列、最小宽度大于默认宽度的操作列。
// 真正的知识库列定义在 M2 的 screens/library 里，由那边的对拍测试保证与渲染层一致。
func testDefs() []ColumnDef {
	return []ColumnDef{
		{ID: "checkbox", Visible: true, Width: 44, MinWidth: 44},
		{ID: "title", Visible: true, Width: 260, MinWidth: 150, MaxWidth: 480, Resizable: true},
		{ID: "snippet", Visible: true, Width: 200, MinWidth: 120, MaxWidth: 640, Resizable: true},
		{ID: "collection", Visible: false, Width: 140, MinWidth: 100, MaxWidth: 280, Resizable: true},
		{ID: "actions", Visible: true, Width: 84, MinWidth: 148},
	}
}

// normalizedDefs 是 NewColumnSet 归一化之后的默认列：默认宽度夹进范围（操作列 84 → 148）。
func normalizedDefs() []ColumnDef {
	defs := testDefs()
	for i := range defs {
		defs[i].Width = defs[i].Clamp(defs[i].Width)
	}
	return defs
}

func colByID(t *testing.T, cols []ColumnDef, id string) ColumnDef {
	t.Helper()
	for _, c := range cols {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("没有列 %s", id)
	return ColumnDef{}
}

// TestMergeStoredColumnsMatchesRenderer 逐条对照渲染层 mergeStoredColumns 的行为（含它的怪癖）。
func TestMergeStoredColumnsMatchesRenderer(t *testing.T) {
	type want struct {
		id      string
		visible bool
		width   float32
	}
	defaults := []want{
		{"checkbox", true, 44}, {"title", true, 260}, {"snippet", true, 200}, {"collection", false, 140}, {"actions", true, 84},
	}
	with := func(over ...want) []want {
		out := append([]want(nil), defaults...)
		for _, o := range over {
			for i := range out {
				if out[i].id == o.id {
					out[i] = o
				}
			}
		}
		return out
	}

	cases := []struct {
		name   string
		stored string
		want   []want
	}{
		{"没有保存", ``, defaults},
		{"null", `null`, defaults},
		{"不是对象：字符串", `"abc"`, defaults},
		{"不是对象：数字", `123`, defaults},
		{"不是对象：数组", `[]`, defaults},
		{"空对象", `{}`, defaults},
		{"columns 是 null", `{"columns":null}`, defaults},
		{"columns 不是数组", `{"columns":{"id":"title"}}`, defaults},
		{"columns 是字符串", `{"columns":"x"}`, defaults},
		{"不是 JSON", `{"columns":[`, defaults},
		{"隐藏一列", `{"columns":[{"id":"snippet","visible":false}]}`, with(want{"snippet", false, 200})},
		{"显示默认隐藏的列", `{"columns":[{"id":"collection","visible":true,"width":200}]}`, with(want{"collection", true, 200})},
		{"visible 不是布尔：字符串", `{"columns":[{"id":"title","visible":"false"}]}`, defaults},
		{"visible 不是布尔：null", `{"columns":[{"id":"title","visible":null}]}`, defaults},
		{"visible 不是布尔：数字", `{"columns":[{"id":"title","visible":0}]}`, defaults},
		{"宽度夹到上限", `{"columns":[{"id":"title","width":9999}]}`, with(want{"title", true, 480})},
		{"宽度夹到下限", `{"columns":[{"id":"title","width":10}]}`, with(want{"title", true, 150})},
		{"宽度在范围内", `{"columns":[{"id":"title","width":333.5}]}`, with(want{"title", true, 333.5})},
		{"宽度不是数字：字符串", `{"columns":[{"id":"title","width":"wide"}]}`, defaults},
		{"宽度不是数字：null", `{"columns":[{"id":"title","width":null}]}`, defaults},
		{"宽度缺失", `{"columns":[{"id":"title","visible":true}]}`, defaults},
		// 渲染层 clampColumnWidth(actions, 84) = max(84, 148) = 148；而没写宽度时保持默认的 84
		{"操作列：保存了小于下限的宽度", `{"columns":[{"id":"actions","width":84}]}`, with(want{"actions", true, 148})},
		{"操作列：没保存宽度", `{"columns":[{"id":"actions","visible":true}]}`, defaults},
		// 渲染层只对「可调」做 UI 上的限制，合并时不看 resizable：固定列的保存宽度照样生效
		{"固定列也吃保存的宽度", `{"columns":[{"id":"checkbox","width":200}]}`, with(want{"checkbox", true, 200})},
		{"未知列 id 丢弃", `{"columns":[{"id":"nope","visible":false,"width":10}]}`, defaults},
		{"id 不是字符串", `{"columns":[{"id":7,"visible":false},{"id":null,"visible":false}]}`, defaults},
		{"重复的 id 取第一条", `{"columns":[{"id":"title","width":300},{"id":"title","width":400}]}`, with(want{"title", true, 300})},
		{"不是对象的元素只跳过那一项", `{"columns":[5,"x",null,[],true,{"id":"title","width":300}]}`, with(want{"title", true, 300})},
		{"保存顺序无关，按声明顺序", `{"columns":[{"id":"snippet","visible":false},{"id":"title","width":200}]}`,
			with(want{"snippet", false, 200}, want{"title", true, 200})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defs := testDefs()
			before := append([]ColumnDef(nil), defs...)
			got := MergeStoredColumns(defs, json.RawMessage(c.stored))
			if len(got) != len(c.want) {
				t.Fatalf("列数 %d，期望 %d", len(got), len(c.want))
			}
			for i, w := range c.want {
				if got[i].ID != w.id || got[i].Visible != w.visible || got[i].Width != w.width {
					t.Errorf("第 %d 列：得到 %s/%v/%v，期望 %s/%v/%v", i, got[i].ID, got[i].Visible, got[i].Width, w.id, w.visible, w.width)
				}
				// 范围与可调性永远以代码里的定义为准，保存的内容改不了
				if got[i].MinWidth != before[i].MinWidth || got[i].MaxWidth != before[i].MaxWidth || got[i].Resizable != before[i].Resizable {
					t.Errorf("第 %d 列的范围/可调性不该被保存的内容改变", i)
				}
			}
			if !reflect.DeepEqual(defs, before) {
				t.Error("defs 不应被修改")
			}
			if len(got) > 0 && len(defs) > 0 && &got[0] == &defs[0] {
				t.Error("返回的应是新切片，改它不能影响 defs")
			}
		})
	}
}

func TestColumnDefClamp(t *testing.T) {
	d := ColumnDef{Width: 260, MinWidth: 150, MaxWidth: 480}
	if d.Clamp(10) != 150 || d.Clamp(9999) != 480 || d.Clamp(300) != 300 {
		t.Error("夹紧结果不对")
	}
	// 没有上限（MaxWidth=0）
	free := ColumnDef{Width: 44, MinWidth: 44}
	if free.Clamp(5000) != 5000 || free.Clamp(1) != 44 {
		t.Error("MaxWidth 为 0 表示没有上限")
	}
}

func decodeColumns(t *testing.T, s *MemStore, key string) []columnPref {
	t.Helper()
	raw, ok := s.Get(key)
	if !ok {
		t.Fatalf("%s 没有保存", key)
	}
	var doc columnPrefs
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("保存的不是 {columns:[...]}：%s", raw)
	}
	return doc.Columns
}

func newFastColumnSet(store PrefStore) *ColumnSet {
	s := NewColumnSet("library.tableConfig", testDefs(), store)
	s.save.delay = 20 * time.Millisecond
	return s
}

func TestColumnSetToggleResizeAndPersist(t *testing.T) {
	var store MemStore
	s := newFastColumnSet(&store)

	if got := s.Visible(); len(got) != 4 || got[0].ID != "checkbox" || got[3].ID != "actions" {
		t.Fatalf("默认显示的列不对：%+v", got)
	}

	s.Toggle("snippet")
	s.Toggle("collection")
	s.Toggle("nope") // 未知 id：什么都不做
	s.Resize("title", 9999)
	s.Resize("checkbox", 300) // 不可调：忽略
	s.Resize("actions", 300)
	s.Resize("nope", 100)
	s.Flush()

	vis := s.Visible()
	var ids []string
	for _, c := range vis {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"checkbox", "title", "collection", "actions"}) {
		t.Errorf("切换后显示的列（声明顺序）：%v", ids)
	}
	if got := colByID(t, s.Columns(), "title").Width; got != 480 {
		t.Errorf("title 应夹到 480：%v", got)
	}
	if got := colByID(t, s.Columns(), "checkbox").Width; got != 44 {
		t.Errorf("不可调的列不该改宽：%v", got)
	}

	// 保存的形状与渲染层 localStorage 一致：全部列、声明顺序、只含 id/visible/width
	saved := decodeColumns(t, &store, "library.tableConfig")
	if len(saved) != 5 || saved[0].ID != "checkbox" || saved[4].ID != "actions" {
		t.Fatalf("保存的列：%+v", saved)
	}
	raw, _ := store.Get("library.tableConfig")
	var generic struct {
		Columns []map[string]any `json:"columns"`
	}
	_ = json.Unmarshal(raw, &generic)
	for _, col := range generic.Columns {
		if len(col) != 3 {
			t.Errorf("每列只应有 id/visible/width：%v", col)
		}
	}

	// 重新打开读回来
	again := NewColumnSet("library.tableConfig", testDefs(), &store)
	if !reflect.DeepEqual(again.Columns(), s.Columns()) {
		t.Errorf("重新打开应得到同样的列：\n%+v\n%+v", again.Columns(), s.Columns())
	}
}

func TestColumnSetResizeSameWidthDoesNotWrite(t *testing.T) {
	var store MemStore
	s := newFastColumnSet(&store)
	s.Resize("title", 260) // 与当前相同
	s.Resize("title", 100) // 夹到 150
	s.Resize("title", 140) // 仍夹到 150：没变化
	s.Flush()
	if store.Writes() != 1 {
		t.Errorf("只有第一次真正改变宽度才写：%d", store.Writes())
	}
}

func TestColumnSetDebouncesDrag(t *testing.T) {
	var store MemStore
	s := newFastColumnSet(&store)
	for w := float32(261); w < 300; w += 3 {
		s.Resize("title", w)
		time.Sleep(time.Millisecond)
	}
	waitUntil(t, "去抖后保存", func() bool { return store.Writes() > 0 })
	time.Sleep(60 * time.Millisecond)
	if store.Writes() != 1 {
		t.Errorf("拖动列宽不该每帧写：%d 次", store.Writes())
	}
}

func TestColumnSetReset(t *testing.T) {
	var store MemStore
	store.Put("library.tableConfig", `{"columns":[{"id":"title","visible":false,"width":300}]}`)
	s := newFastColumnSet(&store)
	if colByID(t, s.Columns(), "title").Visible || colByID(t, s.Columns(), "title").Width != 300 {
		t.Fatal("应读到保存的偏好")
	}
	s.Reset()
	if !reflect.DeepEqual(s.Columns(), normalizedDefs()) {
		t.Errorf("Reset 应回到默认：%+v", s.Columns())
	}
	s.Flush()
	// 重新打开也是默认
	if !reflect.DeepEqual(NewColumnSet("library.tableConfig", testDefs(), &store).Columns(), normalizedDefs()) {
		t.Error("Reset 的结果应保存")
	}
}

func TestColumnSetColumnsReturnsACopy(t *testing.T) {
	s := NewColumnSet("k", testDefs(), nil)
	cols := s.Columns()
	cols[0].Visible = false
	if !s.Columns()[0].Visible {
		t.Error("改返回的切片不该影响内部状态")
	}
	vis := s.Visible()
	vis[0].Width = 1
	if s.Columns()[0].Width != 44 {
		t.Error("Visible 返回的也是副本")
	}
}

func TestColumnSetWithoutStore(t *testing.T) {
	s := NewColumnSet("k", testDefs(), nil)
	s.Toggle("snippet")
	s.Resize("title", 300)
	s.Flush()
	if colByID(t, s.Columns(), "snippet").Visible || colByID(t, s.Columns(), "title").Width != 300 {
		t.Error("无 store 时偏好仍应记在内存里")
	}
}

func TestColumnSetTableColumns(t *testing.T) {
	s := NewColumnSet("k", testDefs(), nil)
	cols := s.TableColumns(func(id string) string { return "列:" + id })
	if len(cols) != 4 {
		t.Fatalf("只含显示中的列：%d", len(cols))
	}
	byID := map[string]ui.TableColumn{}
	for _, c := range cols {
		byID[c.ID] = c
	}
	if c := byID["checkbox"]; !c.Fixed || c.Width != 44 || c.Title != "列:checkbox" {
		t.Errorf("checkbox 应固定：%+v", c)
	}
	if c := byID["title"]; c.Fixed || c.Width != 260 || c.MinWidth != 150 || c.MaxWidth != 480 {
		t.Errorf("title 应可调且带上下限：%+v", c)
	}
	if !byID["actions"].Fixed {
		t.Error("actions 应固定")
	}
	if _, ok := byID["collection"]; ok {
		t.Error("隐藏的列不该出现")
	}
}

// TestColumnSetSyncRecordsAndNormalizesLayout 用合成的布局验证：用户拖出的宽度夹紧后记下，
// 布局里不一致的值被回写（换新映射，不原地改），顺序被清空。
func TestColumnSetSyncRecordsAndNormalizesLayout(t *testing.T) {
	var store MemStore
	s := newFastColumnSet(&store)
	layout := &ui.TableLayout{
		Order:  []string{"snippet", "title"},
		Widths: map[string]float32{"title": 9999, "snippet": 300, "actions": 500, "nope": 1},
	}
	old := layout.Widths
	s.Sync(layout)
	s.Flush()

	if len(layout.Order) != 0 {
		t.Errorf("渲染层的列顺序固定，Sync 应清掉拖出来的顺序：%v", layout.Order)
	}
	if got := colByID(t, s.Columns(), "title").Width; got != 480 {
		t.Errorf("title 应夹到 480：%v", got)
	}
	if got := colByID(t, s.Columns(), "snippet").Width; got != 300 {
		t.Errorf("snippet 应是 300：%v", got)
	}
	if got := colByID(t, s.Columns(), "actions").Width; got != 148 {
		t.Errorf("固定列不接受布局里的宽度：%v", got)
	}
	// 布局里与偏好不一致的值回写成偏好的值，且是换了一个新映射
	if layout.Widths["title"] != 480 || layout.Widths["actions"] != 148 {
		t.Errorf("布局里应回写夹紧后的宽度：%v", layout.Widths)
	}
	if layout.Widths["snippet"] != 300 || layout.Widths["nope"] != 1 {
		t.Errorf("没问题的项保持原样：%v", layout.Widths)
	}
	if reflect.ValueOf(layout.Widths).Pointer() == reflect.ValueOf(old).Pointer() {
		t.Error("必须换新映射：原地修改会让 ui.Table 认不出这次拖动")
	}
	if old["title"] != 9999 {
		t.Error("旧映射不该被改")
	}

	// 一致时不换映射、不重复写
	writes := store.Writes()
	m := layout.Widths
	s.Sync(layout)
	s.Flush()
	if reflect.ValueOf(layout.Widths).Pointer() != reflect.ValueOf(m).Pointer() {
		t.Error("布局与偏好一致时不该换映射")
	}
	if store.Writes() != writes {
		t.Error("布局与偏好一致时不该再写")
	}

	s.Sync(nil) // 不崩
}

// tableHarness 把 ColumnSet 接到真实的 ui.Table 上：每帧建表后调用 Sync。
type tableHarness struct {
	cs    *ColumnSet
	state ui.ListState
	rows  int
}

func newTableTester(t *testing.T, h *tableHarness) *ui.Tester {
	t.Helper()
	l := lightLook()
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		ui.Column(c).Fill().Children(func() {
			cols := h.cs.TableColumns(func(id string) string { return "列:" + id })
			ui.Table(c, &h.state, cols, h.rows, func(row, col int) {
				ui.Text(c, "单元").SingleLine()
			}).Grow(1)
			if h.cs.Sync(&h.state.Columns) {
				c.Invalidate()
			}
		})
	}, 1400, 500)
	tr.Frame()
	return tr
}
