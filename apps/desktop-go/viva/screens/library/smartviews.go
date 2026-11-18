package library

import (
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// 智能视图（渲染层 components/library/saved-library-views.ts）：本机保存的结构化筛选，不保存转瞬即逝的搜索关键词。
//
// 渲染层存在 localStorage 的 guizhi-library-smart-views-v1；Viva 里存在界面偏好（config/ui-prefs.json）的
// library.smartViews，形状不变。首次启动时旧 localStorage 的这一项由设置导入（native/settings 的 ParseLegacy）原样带过来，
// 所以升级上来的用户保存过的视图不会丢。

// 智能视图的上限与名称长度（渲染层 MAX_SAVED_VIEWS 与 name.slice(0, 60)）。
const (
	MaxSavedViews     = 30
	MaxSavedViewName  = 60 // 按 UTF-16 码元数，与 JavaScript 的 slice 一致
	savedViewsPrefKey = settings.PrefSmartViews
)

// savedScopes 是智能视图里合法的范围；其余的值整条丢弃（渲染层 SCOPES）。
var savedScopes = map[string]bool{
	services.LibraryScopeUncategorized: true,
	services.LibraryScopeAll:           true,
	services.LibraryScopeFavorites:     true,
	services.LibraryScopeArchived:      true,
	services.LibraryScopeTrash:         true,
}

// SavedView 是一个已保存的智能视图。分面为空串表示没有（JSON 里写成 null，与渲染层一致）。
type SavedView struct {
	ID        string
	Name      string
	Filters   Filters
	CreatedAt float64 // 毫秒时间戳；旧数据里不是有限数的记 0
}

// savedViewJSON 是 SavedView 在存储里的形状（渲染层 SavedLibraryView）。
type savedViewJSON struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Scope        string  `json:"scope"`
	CollectionID *string `json:"collectionId"`
	TagID        *string `json:"tagId"`
	Platform     *string `json:"platform"`
	CreatedAt    float64 `json:"createdAt"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// MarshalJSON 写成渲染层的形状：没有的分面是 null。
func (v SavedView) MarshalJSON() ([]byte, error) {
	return json.Marshal(savedViewJSON{
		ID: v.ID, Name: v.Name, Scope: v.Filters.Scope,
		CollectionID: nullable(v.Filters.CollectionID), TagID: nullable(v.Filters.TagID), Platform: nullable(v.Filters.Platform),
		CreatedAt: v.CreatedAt,
	})
}

// Matches 报告当前筛选是否正好是这个视图（侧栏里该行是否选中）。
func (v SavedView) Matches(f Filters) bool { return v.Filters == f }

// jsTrim 是 JavaScript 的 String.prototype.trim：Go 的 unicode.IsSpace 多一个 U+0085、少一个 U+FEFF。
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r == 0xFEFF || (unicode.IsSpace(r) && r != 0x85)
	})
}

// sliceUTF16 取前 max 个 UTF-16 码元（JavaScript 的 slice(0, max)）；放不下的整个字符丢掉，不留半个代理对。
func sliceUTF16(s string, max int) string {
	units := 0
	for i, r := range s {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if units+n > max {
			return s[:i]
		}
		units += n
	}
	return s
}

// normalizeName 是名称的归一化：去首尾空白，再截到 60 个码元。
func normalizeName(name string) string { return sliceUTF16(jsTrim(name), MaxSavedViewName) }

// normalizeSavedView 把一条存储里的值归一化（渲染层 normalize）；不合法的返回 false。
func normalizeSavedView(raw json.RawMessage) (SavedView, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return SavedView{}, false
	}
	// 只认字符串：缺失、null、数字等都不算（渲染层 typeof x === "string"）。
	str := func(key string) (string, bool) {
		raw := obj[key]
		if len(raw) == 0 || string(raw) == "null" {
			return "", false
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	}
	id, ok := str("id")
	if !ok {
		return SavedView{}, false
	}
	name, ok := str("name")
	if !ok || jsTrim(name) == "" {
		return SavedView{}, false
	}
	scope, ok := str("scope")
	if !ok || !savedScopes[scope] {
		return SavedView{}, false
	}
	facet := func(key string) string {
		if s, ok := str(key); ok {
			return s // 空串与 null 都归为没有
		}
		return ""
	}
	var created float64
	if err := json.Unmarshal(obj["createdAt"], &created); err != nil || math.IsNaN(created) || math.IsInf(created, 0) {
		created = 0
	}
	return SavedView{
		ID: id, Name: normalizeName(name), CreatedAt: created,
		Filters: Filters{Scope: scope, CollectionID: facet("collectionId"), TagID: facet("tagId"), Platform: facet("platform")},
	}, true
}

// ReadSavedViews 读出已保存的智能视图：坏数据静默丢弃（整份不是数组就当没有；单条不合法就跳过那一条），最多 30 条。
func ReadSavedViews(prefs services.Prefs) []SavedView {
	if prefs == nil {
		return nil
	}
	raw, ok := prefs.Get(savedViewsPrefKey)
	if !ok {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []SavedView
	for _, item := range items {
		if view, ok := normalizeSavedView(item); ok {
			out = append(out, view)
		}
	}
	if len(out) > MaxSavedViews {
		out = out[:MaxSavedViews]
	}
	return out
}

// WriteSavedViews 保存智能视图（最多 30 条）。没有偏好存储时什么都不做。
func WriteSavedViews(prefs services.Prefs, views []SavedView) error {
	if prefs == nil {
		return nil
	}
	if len(views) > MaxSavedViews {
		views = views[:MaxSavedViews]
	}
	if views == nil {
		views = []SavedView{} // 写成 []，不是 null
	}
	return prefs.Set(savedViewsPrefKey, views)
}

// NewSavedView 用当前筛选建一个智能视图（渲染层 createSavedLibraryView）：名称去首尾空白并截断，空名返回 false。
func NewSavedView(name string, f Filters, now time.Time, newID func() string) (SavedView, bool) {
	trimmed := normalizeName(name)
	if trimmed == "" {
		return SavedView{}, false
	}
	return SavedView{ID: newID(), Name: trimmed, Filters: f, CreatedAt: float64(now.UnixMilli())}, true
}
