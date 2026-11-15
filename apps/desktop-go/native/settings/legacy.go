package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// 旧版（React 渲染层）localStorage 的解析：纯函数，输入是「键 → 字符串」，输出是类型化的设置与偏好。
// 读 WebView2 的 Local Storage 目录、写存储都在 package main（legacy_webview2_ls.go）。
//
// 覆盖范围由 KnownLegacyKeys 登记；测试扫描渲染层源码，发现新的 localStorage 键没登记就失败，
// 所以每周合并 main 时，新增的键不会悄悄漏导。

// LegacySettingsKey 是 zustand 持久化的设置（{state, version}）。
const LegacySettingsKey = "guizhi-settings"

// legacyExact 是按名字精确匹配的旧键：settings / prefs 表示导入到哪里，ignored 表示有意不导入（原因写在行尾注释里）。
var legacyExact = map[string]string{
	LegacySettingsKey:               "settings", // → UI 设置、Go 侧设置、scenarioModelDefaults
	"ui-storage":                    "prefs",
	"guizhi-library-table-config":   "prefs",
	"guizhi-library-smart-views-v1": "prefs",
	"guizhi-content-reading-v1":     "prefs",
	"guizhi-snapshot-reading-v1":    "prefs",
	"wechat-reader-width":           "prefs",
	"guizhi-ask-active-session":     "prefs",
	"guizhi-setup-dismissed":        "prefs",
	"guizhi-migration-dismissed":    "prefs",
	"guizhi-table-config":           "ignored", // 早已退役的列配置键，没有任何代码读它（仍在传输白名单里，所以要登记）
	"guizhi-legacy-ls-imported":     "ignored", // Electron 版导入时写下的幂等标记，不是用户数据
}

// legacyPrefixes 是按前缀匹配的旧键（后面接动态部分）。
var legacyPrefixes = []string{"reading-original-catalog:", "reading-ai-catalog:"}

// KnownLegacyKeys 返回已登记的旧键（精确键与动态前缀），供测试核对渲染层源码。
func KnownLegacyKeys() (exact []string, prefixes []string) {
	for k := range legacyExact {
		exact = append(exact, k)
	}
	sort.Strings(exact)
	return exact, append([]string(nil), legacyPrefixes...)
}

// Legacy 是从旧 localStorage 解出的内容。
type Legacy struct {
	// HasSettings 报告是否读到了有效的旧设置（guizhi-settings）。
	HasSettings bool
	// UI 是旧设置里属于界面的部分，已按渲染层规则归一；HasSettings 为假时是默认值。
	UI UI
	// App 是旧设置里属于 Go 侧设置的键：标量已归一，类型不对的键不出现；
	// networkProxy 与 flareSolverr 是原始对象，交写入处用 Go 侧的归一化函数处理。
	// ytDlpPath、ffmpegPath 故意不导入：可执行文件路径只接受文件选择器返回过的值。
	App map[string]json.RawMessage
	// Prefs 是界面偏好（点号键 → JSON），只含旧数据里存在且结构有效的项。
	Prefs map[string]json.RawMessage
	// Issues 是被跳过的坏数据的说明（键名与原因，不含值：值可能含口令）。
	Issues []string
}

const maxCatalogFlags = 500

var (
	errNotJSON    = errors.New("不是有效的 JSON")
	errWrongShape = errors.New("顶层形状不对")
)

// ParseLegacy 解析旧 localStorage 的全部键。坏数据只记入 Issues、不影响其余键。
func ParseLegacy(entries map[string]string) Legacy {
	res := Legacy{UI: DefaultUI(), App: map[string]json.RawMessage{}, Prefs: map[string]json.RawMessage{}}
	// 说明里只有键名与原因，不带值（值可能含口令）；原因用 error 承载：诊断文字不是界面文案
	issue := func(key string, why error) { res.Issues = append(res.Issues, fmt.Sprintf("%s: %v", key, why)) }
	setPref := func(key string, v any) {
		if raw, err := json.Marshal(v); err == nil {
			res.Prefs[key] = raw
		}
	}

	if raw, ok := entries[LegacySettingsKey]; ok {
		state, version, err := unwrapPersisted(raw)
		if err != nil {
			issue(LegacySettingsKey, err)
		} else {
			res.HasSettings = true
			res.UI = DecodeUI(state, version)
			res.App = legacyApp(state)
		}
	}

	if raw, ok := entries["ui-storage"]; ok {
		state, _, err := unwrapPersisted(raw)
		if err != nil {
			issue("ui-storage", err)
		} else {
			parseUIStorage(fields(state), setPref)
		}
	}

	// 整份 JSON 的偏好：只校验顶层形状，内容由各模块容错读取。
	for _, p := range []struct {
		legacy, key string
		kind        byte // '{' 对象，'[' 数组
	}{
		{"guizhi-library-table-config", PrefLibraryColumns, '{'},
		{"guizhi-library-smart-views-v1", PrefSmartViews, '['},
		{"guizhi-content-reading-v1", PrefContentReading, '{'},
		{"guizhi-snapshot-reading-v1", PrefSnapshotReading, '{'},
	} {
		raw, ok := entries[p.legacy]
		if !ok {
			continue
		}
		var probe json.RawMessage
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			issue(p.legacy, errNotJSON)
			continue
		}
		if len(probe) == 0 || probe[0] != p.kind {
			issue(p.legacy, errWrongShape)
			continue
		}
		res.Prefs[p.key] = probe
	}

	// 纯字符串的偏好（localStorage 里不是 JSON）。
	if v, ok := entries["guizhi-ask-active-session"]; ok && v != "" {
		setPref(PrefAskActiveSession, v)
	}
	if v, ok := entries["wechat-reader-width"]; ok && (v == "original" || v == "wide") {
		setPref(PrefWechatWidth, v)
	}
	if v, ok := entries["guizhi-setup-dismissed"]; ok && v != "" {
		setPref(PrefSetupDismissed, true)
	}
	if v, ok := entries["guizhi-migration-dismissed"]; ok && v != "" {
		setPref(PrefMigrationDismissed, true)
	}

	// 阅读视图的目录开关：每个视图一个键，合并成一个对象；只有 true 才有意义（缺省就是收起）。
	flags := map[string]bool{}
	for k, v := range entries {
		switch {
		case strings.HasPrefix(k, "reading-original-catalog:"):
			if v == "true" {
				flags["original:"+strings.TrimPrefix(k, "reading-original-catalog:")] = true
			}
		case strings.HasPrefix(k, "reading-ai-catalog:"):
			if v == "true" {
				flags["ai:"+strings.TrimPrefix(k, "reading-ai-catalog:")] = true
			}
		}
	}
	if len(flags) > 0 {
		if len(flags) > maxCatalogFlags {
			keys := make([]string, 0, len(flags))
			for k := range flags {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys[maxCatalogFlags:] {
				delete(flags, k)
			}
			issue("reading-*-catalog:*", fmt.Errorf("条目过多，只保留 %d 项", maxCatalogFlags))
		}
		setPref(PrefCatalogOpen, flags)
	}
	return res
}

// unwrapPersisted 取 zustand persist 写下的 {state, version}。version 缺省按当前版本（3）处理：
// 渲染层 migrate 对 undefined 版本不套用 v3 之前的规则。
func unwrapPersisted(raw string) (state map[string]json.RawMessage, version int, err error) {
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &top) != nil || top == nil {
		return nil, 0, fmt.Errorf("不是有效的持久化对象")
	}
	state, ok := fields(top).object("state")
	if !ok {
		return nil, 0, fmt.Errorf("缺少 state 对象")
	}
	version = PersistedVersion
	if n, ok := fields(top).number("version"); ok {
		version = int(n)
	}
	return state, version, nil
}

// legacyApp 取旧设置里属于 Go 侧设置的键。
func legacyApp(state map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	f := fields(state)
	put := func(key string, v any) {
		if raw, err := json.Marshal(v); err == nil {
			out[key] = raw
		}
	}
	if _, present := state["language"]; present {
		s, _ := f.str("language") // 渲染层：非字符串按空串处理，归一为 en
		put("language", NormalizeLanguage(s))
	}
	for _, key := range []string{"launchAtStartup", "minimizeOnLaunch", "backgroundTasksEnabled", "enableNotifications", "backupAutoEnabled", "transcribeDiarize"} {
		if b, ok := f.boolean(key); ok {
			put(key, b)
		}
	}
	if n, ok := f.number("backupIntervalHours"); ok {
		put("backupIntervalHours", ClampBackup(n, DefaultBackupIntervalHours, MinBackupIntervalHours, MaxBackupIntervalHours))
	}
	if n, ok := f.number("backupKeepCount"); ok {
		put("backupKeepCount", ClampBackup(n, DefaultBackupKeepCount, MinBackupKeepCount, MaxBackupKeepCount))
	}
	for _, key := range []string{"networkProxy", "flareSolverr"} {
		if m, ok := f.object(key); ok {
			put(key, m)
		}
	}
	return out
}

// parseUIStorage 对应 ui.store.ts 的 merge：窗格宽度夹到范围内（非数字取下限），应用模块与视图模式归一。
// 只输出旧数据里存在的项；缺省项在使用处取默认值。
func parseUIStorage(f fields, set func(key string, v any)) {
	if s, ok := f.str("appModule"); ok {
		set(PrefAppModule, NormalizeAppModule(s))
	}
	if b, ok := f.boolean("isSidebarCollapsed"); ok {
		set(PrefSidebarCollapsed, b)
	}
	for _, p := range PaneWidths {
		raw, present := f[p.LegacyUIField]
		if !present || string(raw) == "null" {
			continue // 渲染层 ?? 默认值：null 与缺省等同
		}
		n, ok := f.number(p.LegacyUIField)
		if !ok {
			n = math.NaN() // 非数字：渲染层的 Number.isFinite 为假，夹取后得到下限
		}
		set(p.Key, p.Clamp(n))
	}
	mode, present := f["libraryViewMode"]
	if !present || string(mode) == "null" {
		mode, present = f["libraryListDensity"] // v0.3 早期的字段名
	}
	if present && string(mode) != "null" {
		s, _ := rawString(mode) // 非字符串：渲染层归一为 card
		set(PrefLibraryViewMode, NormalizeLibraryViewMode(s))
	}
}
