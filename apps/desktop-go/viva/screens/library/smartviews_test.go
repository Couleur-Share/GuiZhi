package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/settings"
)

// 本文件对应渲染层 saved-library-views.ts：读取时的归一化（坏数据静默丢弃）、30 条与 60 字的上限、写回的形状。

// memPrefs 是内存里的界面偏好：Set 与真实实现一样先编码成 JSON。
type memPrefs struct {
	data    map[string]json.RawMessage
	setErr  error
	setKeys []string
}

func newMemPrefs() *memPrefs { return &memPrefs{data: map[string]json.RawMessage{}} }

func (m *memPrefs) Get(key string) (json.RawMessage, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *memPrefs) Set(key string, value any) error {
	if m.setErr != nil {
		return m.setErr
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.data[key] = raw
	m.setKeys = append(m.setKeys, key)
	return nil
}

func (m *memPrefs) putRaw(raw string) { m.data[settings.PrefSmartViews] = json.RawMessage(raw) }

func TestReadSavedViewsDropsBadDataSilently(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"整份不是数组", `{"not":"array"}`, 0},
		{"整份不是 JSON", `not json`, 0},
		{"空数组", `[]`, 0},
		{"元素不是对象", `[1, "a", null, [], true]`, 0},
		{"id 不是字符串", `[{"id":1,"name":"a","scope":"all"}]`, 0},
		{"id 是 null", `[{"id":null,"name":"a","scope":"all"}]`, 0},
		{"缺 id", `[{"name":"a","scope":"all"}]`, 0},
		{"name 不是字符串", `[{"id":"a","name":5,"scope":"all"}]`, 0},
		{"name 只有空白", `[{"id":"a","name":" \t\n ","scope":"all"}]`, 0},
		{"范围不认识", `[{"id":"a","name":"x","scope":"everything"}]`, 0},
		{"缺范围", `[{"id":"a","name":"x"}]`, 0},
		{"空 id 字符串照收", `[{"id":"","name":"x","scope":"all"}]`, 1},
		{"没有分面字段照收", `[{"id":"v1","name":"未读","scope":"all"}]`, 1},
		{"坏的夹在好的中间只丢坏的", `[{"id":"a","name":"x","scope":"all"},{"id":2},{"id":"b","name":"y","scope":"trash"}]`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newMemPrefs()
			p.putRaw(tc.raw)
			if got := len(ReadSavedViews(p)); got != tc.want {
				t.Errorf("读出 %d 条，期望 %d", got, tc.want)
			}
		})
	}
	if got := ReadSavedViews(newMemPrefs()); len(got) != 0 {
		t.Errorf("没有保存过时应为空：%v", got)
	}
	if got := ReadSavedViews(nil); len(got) != 0 {
		t.Errorf("没有偏好存储时应为空：%v", got)
	}
}

func TestReadSavedViewsNormalizesFields(t *testing.T) {
	p := newMemPrefs()
	p.putRaw(`[
		{"id":"a","name":"  工作  ","scope":"favorites","collectionId":"c1","tagId":null,"platform":"douyin","createdAt":1700000000000,"extra":1},
		{"id":"b","name":"x","scope":"all","collectionId":"","tagId":7,"platform":false,"createdAt":"yesterday"},
		{"id":"c","name":"y","scope":"archived","createdAt":null}
	]`)
	got := ReadSavedViews(p)
	if len(got) != 3 {
		t.Fatalf("读出 %d 条", len(got))
	}
	a := got[0]
	if a.ID != "a" || a.Name != "工作" || a.CreatedAt != 1700000000000 ||
		a.Filters != (Filters{Scope: "favorites", CollectionID: "c1", Platform: "douyin"}) {
		t.Errorf("第一条 %+v", a)
	}
	b := got[1]
	if b.Filters != (Filters{Scope: "all"}) {
		t.Errorf("空串、数字与布尔的分面都应归为没有：%+v", b.Filters)
	}
	if b.CreatedAt != 0 || got[2].CreatedAt != 0 {
		t.Errorf("createdAt 不是数字时记 0：%v %v", b.CreatedAt, got[2].CreatedAt)
	}
}

func TestReadSavedViewsKeepsTheFirstThirty(t *testing.T) {
	var parts []string
	for i := 0; i < 45; i++ {
		parts = append(parts, fmt.Sprintf(`{"id":"v%d","name":"视图%d","scope":"all"}`, i, i))
	}
	// 夹两条坏的：上限按「过滤之后」算，坏数据不占名额
	parts = append([]string{`{"id":1}`}, parts...)
	p := newMemPrefs()
	p.putRaw("[" + strings.Join(parts, ",") + "]")
	got := ReadSavedViews(p)
	if len(got) != MaxSavedViews {
		t.Fatalf("读出 %d 条，期望 %d", len(got), MaxSavedViews)
	}
	if got[0].ID != "v0" || got[MaxSavedViews-1].ID != "v29" {
		t.Errorf("应保留前 30 条：%s … %s", got[0].ID, got[MaxSavedViews-1].ID)
	}
}

func TestReadSavedViewsTruncatesNamesLikeJavaScript(t *testing.T) {
	long := strings.Repeat("字", 70)
	p := newMemPrefs()
	p.putRaw(fmt.Sprintf(`[{"id":"a","name":%q,"scope":"all"}]`, long))
	got := ReadSavedViews(p)
	if len(got) != 1 || got[0].Name != strings.Repeat("字", 60) {
		t.Errorf("名称应截到 60 个字：%q", got[0].Name)
	}
}

func TestSavedViewsRoundTripKeepsTheRendererShape(t *testing.T) {
	p := newMemPrefs()
	views := []SavedView{
		{ID: "a", Name: "工作库的收藏", Filters: Filters{Scope: "favorites", CollectionID: "c1"}, CreatedAt: 1700000000000},
		{ID: "b", Name: "全部", Filters: Filters{Scope: "all"}, CreatedAt: 1},
	}
	if err := WriteSavedViews(p, views); err != nil {
		t.Fatal(err)
	}
	raw := string(p.data[settings.PrefSmartViews])
	want := `[{"id":"a","name":"工作库的收藏","scope":"favorites","collectionId":"c1","tagId":null,"platform":null,"createdAt":1700000000000},` +
		`{"id":"b","name":"全部","scope":"all","collectionId":null,"tagId":null,"platform":null,"createdAt":1}]`
	if raw != want {
		t.Errorf("写出的形状\n got %s\nwant %s", raw, want)
	}
	back := ReadSavedViews(p)
	if len(back) != 2 || back[0] != views[0] || back[1] != views[1] {
		t.Errorf("读回来 %+v", back)
	}
}

func TestWriteSavedViewsCapsAtThirtyAndWritesAnEmptyArray(t *testing.T) {
	p := newMemPrefs()
	var many []SavedView
	for i := 0; i < 40; i++ {
		many = append(many, SavedView{ID: fmt.Sprint(i), Name: "n", Filters: Filters{Scope: "all"}})
	}
	if err := WriteSavedViews(p, many); err != nil {
		t.Fatal(err)
	}
	if got := len(ReadSavedViews(p)); got != MaxSavedViews {
		t.Errorf("写入后读出 %d 条，期望 %d", got, MaxSavedViews)
	}
	if err := WriteSavedViews(p, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(p.data[settings.PrefSmartViews]); got != "[]" {
		t.Errorf("清空应写成 []，得到 %s", got)
	}
	if err := WriteSavedViews(nil, many); err != nil {
		t.Errorf("没有偏好存储时不该报错：%v", err)
	}
}

func TestWriteSavedViewsReturnsTheStoreError(t *testing.T) {
	p := newMemPrefs()
	p.setErr = errors.New("磁盘已满")
	if err := WriteSavedViews(p, []SavedView{{ID: "a", Name: "x"}}); err == nil || err.Error() != "磁盘已满" {
		t.Errorf("存储失败应原样返回：%v", err)
	}
}

func TestNewSavedViewTrimsTruncatesAndRejectsEmptyNames(t *testing.T) {
	now := time.UnixMilli(1700000123456)
	id := func() string { return "uuid-1" }
	f := Filters{Scope: "favorites", Platform: "douyin"}

	v, ok := NewSavedView("  抖音收藏  ", f, now, id)
	if !ok || v.Name != "抖音收藏" || v.ID != "uuid-1" || v.Filters != f || v.CreatedAt != 1700000123456 {
		t.Errorf("得到 %+v %v", v, ok)
	}
	if _, ok := NewSavedView(" \t ", f, now, id); ok {
		t.Error("空名不该建出视图（保存按钮本来就禁用）")
	}
	// JavaScript 的 slice 按 UTF-16 码元数：59 个 ASCII 后面的表情占 2 个码元，放不下就不要
	name := strings.Repeat("a", 59) + "😀tail"
	v, _ = NewSavedView(name, f, now, id)
	if v.Name != strings.Repeat("a", 59) {
		t.Errorf("按码元截断：%q", v.Name)
	}
	// 58 个 ASCII 加表情刚好 60 个码元
	v, _ = NewSavedView(strings.Repeat("a", 58)+"😀tail", f, now, id)
	if v.Name != strings.Repeat("a", 58)+"😀" {
		t.Errorf("刚好放得下的表情应保留：%q", v.Name)
	}
}

func TestJSTrimMatchesJavaScript(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  a  ", "a"},
		{"\u00a0a\u00a0", "a"},             // 不换行空格：JS 去、Go 也去
		{"\ufeffa\ufeff", "a"},             // BOM：JS 去、Go 的 IsSpace 不认
		{"\u0085a\u0085", "\u0085a\u0085"}, // NEL：Go 的 IsSpace 认、JS 不去
		{"\u2028a\u2029", "a"},             // 行分隔与段分隔
		{"a b", "a b"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := jsTrim(tc.in); got != tc.want {
			t.Errorf("jsTrim(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestSavedViewMatchesTheWholeFilter(t *testing.T) {
	v := SavedView{Filters: Filters{Scope: "favorites", TagID: "t1"}}
	if !v.Matches(Filters{Scope: "favorites", TagID: "t1"}) {
		t.Error("相同的筛选应匹配")
	}
	for _, other := range []Filters{
		{Scope: "all", TagID: "t1"}, {Scope: "favorites"}, {Scope: "favorites", TagID: "t1", Platform: "web"},
		{Scope: "favorites", TagID: "t1", CollectionID: "c1"},
	} {
		if v.Matches(other) {
			t.Errorf("%+v 不该匹配（范围与三个分面都要一致）", other)
		}
	}
}

// 旧 localStorage 导入时原样带过来的数据（native/settings/legacy_test.go 里的样本）能被读出来。
func TestReadSavedViewsAcceptsTheLegacyImportSample(t *testing.T) {
	p := newMemPrefs()
	p.putRaw(`[{"id":"v1","name":"未读","scope":"all"}]`)
	got := ReadSavedViews(p)
	if len(got) != 1 || got[0].Name != "未读" || got[0].Filters != (Filters{Scope: "all"}) {
		t.Errorf("%+v", got)
	}
}
