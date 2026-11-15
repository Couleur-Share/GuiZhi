package main

// 原生界面的界面设置与偏好文件：config/ui-prefs.json。
//
// 为什么在数据库之外：备份还原会整库替换，外观与布局不该随之回退；备份、代理、工具路径这类 Go 侧要消费的设置
// 才放在 settings 表（见 native/settings 包说明）。文件内容：
//
//	{ "version": 1,
//	  "settings": { <界面设置，键同 settings.UI 的 JSON 键> },
//	  "prefs":    { <界面偏好：点号键 → 任意 JSON> },
//	  "import":   { <首次启动导入旧数据的记录，供排查> } }
//
// 文件存在即「已初始化」：它既是导入旧 localStorage 的提交点，也是之后不再导入的标记。
// 未识别的顶层键与 settings 键原样保留（新版本写的内容，降级后再升级不丢）。
// 写入是临时文件加原子替换；读到损坏的文件时改名为 .corrupt-<时间> 留作排查，按「没有文件」处理。
//
// The native UI's own settings and preferences file; its existence marks "initialized".

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"guizhi-mygo-shell/native/settings"
)

const (
	uiPrefsFileName = "ui-prefs.json"
	uiPrefsVersion  = 1
)

// uiPrefsImport 记录首次启动导入旧数据的结果。Issues 只含键名与原因，不含值（值可能含口令）。
type uiPrefsImport struct {
	Source string   `json:"source"` // webview2 | electron | none
	Origin string   `json:"origin,omitempty"`
	At     string   `json:"at"`
	Keys   int      `json:"keys"` // 读到的 localStorage 键数
	Issues []string `json:"issues,omitempty"`
}

// uiPrefsStore 是 ui-prefs.json 的读写；并发安全（去抖保存在定时器的 goroutine 里调用 Set）。
// 实现 services.Prefs（偏好）并提供界面设置的读写，被 nativeSettings 使用。
type uiPrefsStore struct {
	path string
	logf func(format string, args ...any)

	mu       sync.Mutex
	exists   bool
	top      map[string]json.RawMessage // 上次读到或写出的顶层键，写出时作为底（未识别的键因此保留）
	settings map[string]json.RawMessage
	prefs    map[string]json.RawMessage
	imported *uiPrefsImport
}

// openUIPrefsStore 读取 path；没有文件得到空存储（Initialized 为假）。logf 为 nil 时不记日志。
func openUIPrefsStore(path string, logf func(format string, args ...any)) *uiPrefsStore {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &uiPrefsStore{path: path, logf: logf, top: map[string]json.RawMessage{}, settings: map[string]json.RawMessage{}, prefs: map[string]json.RawMessage{}}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		logf("[ui-prefs] 读取 %s 失败，本次按没有文件处理：%v", path, err)
	default:
		if err := s.decode(data); err != nil {
			s.quarantine(err)
		}
	}
	return s
}

func (s *uiPrefsStore) decode(data []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return fmt.Errorf("不是 JSON 对象")
	}
	var version float64
	if err := json.Unmarshal(top["version"], &version); err != nil || version < 1 {
		return fmt.Errorf("缺少有效的 version")
	}
	object := func(key string) (map[string]json.RawMessage, error) {
		raw, ok := top[key]
		if !ok || string(raw) == "null" {
			return map[string]json.RawMessage{}, nil
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil || m == nil {
			return nil, fmt.Errorf("%s 不是对象", key)
		}
		// 文件是缩进写出的，取值时紧凑化：内存里的值与刚写入时一致，不随文件排版变化
		for k, v := range m {
			var compact bytes.Buffer
			if json.Compact(&compact, v) == nil {
				m[k] = compact.Bytes()
			}
		}
		return m, nil
	}
	settingsMap, err := object("settings")
	if err != nil {
		return err
	}
	prefsMap, err := object("prefs")
	if err != nil {
		return err
	}
	var imported *uiPrefsImport
	if raw, ok := top["import"]; ok && string(raw) != "null" {
		var rec uiPrefsImport
		if json.Unmarshal(raw, &rec) == nil { // 记录只是排查用的：读不懂就不要，不让它挡住启动
			imported = &rec
		}
	}
	s.top, s.settings, s.prefs, s.imported, s.exists = top, settingsMap, prefsMap, imported, true
	return nil
}

// quarantine 把读不懂的文件改名留存，之后按没有文件处理。
func (s *uiPrefsStore) quarantine(cause error) {
	dst := s.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
	if err := os.Rename(s.path, dst); err != nil {
		s.logf("[ui-prefs] %s 无法解析（%v），改名留存也失败（%v）：下次保存会覆盖它", s.path, cause, err)
		return
	}
	s.logf("[ui-prefs] %s 无法解析（%v），已改名为 %s，按没有文件处理", s.path, cause, dst)
}

// Initialized 报告文件是否存在（已读到、或本次运行里写出过）。
func (s *uiPrefsStore) Initialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exists
}

// Import 返回导入记录；没有时为 nil。
func (s *uiPrefsStore) Import() *uiPrefsImport {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.imported == nil {
		return nil
	}
	rec := *s.imported
	return &rec
}

// UI 返回界面设置：文件里没有的键取默认值，类型不对的退回默认值。
func (s *uiPrefsStore) UI() settings.UI {
	s.mu.Lock()
	defer s.mu.Unlock()
	return settings.DecodeUI(s.settings, settings.PersistedVersion)
}

func cloneRaw(m map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// writeLocked 把给定内容写成文件（临时文件加原子替换）；成功后才更新内存里的顶层状态。
func (s *uiPrefsStore) writeLocked(settingsMap, prefsMap map[string]json.RawMessage, imported *uiPrefsImport) error {
	top := cloneRaw(s.top)
	version := float64(uiPrefsVersion)
	var existing float64
	if json.Unmarshal(top["version"], &existing) == nil && existing > version {
		version = existing // 不把更新版本写的文件降级
	}
	set := func(key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		top[key] = raw
		return nil
	}
	for _, p := range []struct {
		key string
		v   any
	}{{"version", version}, {"settings", settingsMap}, {"prefs", prefsMap}} {
		if err := set(p.key, p.v); err != nil {
			return err
		}
	}
	if imported != nil {
		if err := set("import", imported); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(s.path, append(data, '\n')); err != nil {
		return fmt.Errorf("写入 %s 失败：%w", uiPrefsFileName, err)
	}
	s.top, s.exists = top, true
	return nil
}

// initialize 写出首个文件（导入的提交点）。文件已存在时报错：导入只发生一次。
func (s *uiPrefsStore) initialize(settingsMap, prefsMap map[string]json.RawMessage, rec uiPrefsImport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists {
		return errors.New("界面设置文件已存在，不再导入")
	}
	if settingsMap == nil {
		settingsMap = map[string]json.RawMessage{}
	}
	if prefsMap == nil {
		prefsMap = map[string]json.RawMessage{}
	}
	if err := s.writeLocked(settingsMap, prefsMap, &rec); err != nil {
		return err
	}
	s.settings, s.prefs, s.imported = settingsMap, prefsMap, &rec
	return nil
}

// ApplyUI 把变化的界面设置键合并进文件。返回的 undo 把这些键恢复成写入前的样子（其他调用写入的偏好不动）；
// 调用方在后续步骤失败时用它回滚。
func (s *uiPrefsStore) ApplyUI(changed map[string]json.RawMessage) (undo func() error, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prevSettings, prevExisted := s.settings, s.exists
	next := cloneRaw(s.settings)
	for k, v := range changed {
		next[k] = v
	}
	if err := s.writeLocked(next, s.prefs, s.imported); err != nil {
		return nil, err
	}
	s.settings = next
	return func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		restored := cloneRaw(s.settings)
		for k := range changed {
			if old, ok := prevSettings[k]; ok {
				restored[k] = old
			} else {
				delete(restored, k)
			}
		}
		if !prevExisted && len(restored) == 0 && len(s.prefs) == 0 && s.imported == nil {
			// 写入前根本没有文件、现在也没有别的内容：恢复成没有文件
			if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			s.settings, s.exists = restored, false
			return nil
		}
		if err := s.writeLocked(restored, s.prefs, s.imported); err != nil {
			return err
		}
		s.settings = restored
		return nil
	}, nil
}

// Get 读取一个偏好的原始 JSON。
func (s *uiPrefsStore) Get(key string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.prefs[key]
	return v, ok
}

// Set 保存一个偏好；值没有变化时不写盘。
func (s *uiPrefsStore) Set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("编码偏好 %s 失败：%w", key, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.prefs[key]; ok && bytes.Equal(cur, raw) {
		return nil
	}
	next := cloneRaw(s.prefs)
	next[key] = raw
	if err := s.writeLocked(s.settings, next, s.imported); err != nil {
		return err
	}
	s.prefs = next
	return nil
}
