package main

// 启动时渲染进程会调用、且语义自成一体的几个小频道：
//   updater:version / updater:logAutoSkip / backupRepository:syncRendererSettings
// 其余启动频道各自牵出整套主进程服务（任务调度、Wiki 编译、研究、导入、设置、快捷键），不在这里凑数。

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"sort"
	"sync"
)

// appVersion 由构建时 -ldflags "-X main.appVersion=x.y.z" 注入；对应 Electron 的 app.getVersion()。
var appVersion = "0.28.0"

// 与 apps/desktop/src/main/updater.ts 的白名单保持一致。
var (
	updateCheckTriggers = []string{"manual", "startup", "interval", "visibility"}
	autoSkipReasons     = []string{"disabled", "hidden", "offline", "in-flight", "cooldown"}
)

// rendererBackupSettings 对应 backup.ipc.ts 的 setBackupRendererSettings：只存在内存里，
// 备份任务触发时才读取。备份调度器（syncRepositoryBackupJob）随备份子系统一并迁移。
type rendererBackupSettings struct {
	mu       sync.RWMutex
	settings map[string]any
	raw      []byte // 原始 JSON：保留键序（JS 对象序），快照里的 renderer-settings.json 靠它还原
	onChange func() // 设置变化后回调（重算仓库自动备份任务）；nil 表示无
}

func (s *rendererBackupSettings) set(v map[string]any) {
	s.mu.Lock()
	s.settings = v
	s.raw = nil
	s.mu.Unlock()
}

// setRaw 记录一份已校验为对象的 JSON；raw 保留键序。
func (s *rendererBackupSettings) setRaw(raw []byte, v map[string]any) {
	s.mu.Lock()
	s.settings = v
	s.raw = append([]byte(nil), raw...)
	cb := s.onChange
	s.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// ordered 返回按原始键序解析的设置；没有设置返回 nil（对应 getBackupRendererSettings() 的 undefined）。
func (s *rendererBackupSettings) ordered() *omap {
	s.mu.RLock()
	raw, m := s.raw, s.settings
	s.mu.RUnlock()
	if raw != nil {
		if v, err := parseOrdered(raw); err == nil {
			if o, ok := v.(*omap); ok {
				return o
			}
		}
	}
	if m == nil {
		return nil
	}
	o := newOmap() // 仅测试经 set() 写入时走这里：键序不保证
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.set(k, m[k])
	}
	return o
}
func (s *rendererBackupSettings) get() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (b *Bridge) registerSimpleBoot(backup *rendererBackupSettings) {
	b.handlers["updater:version"] = func(context.Context, []json.RawMessage) (any, error) {
		return appVersion, nil
	}
	b.handlers["updater:logAutoSkip"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		var p struct {
			Trigger string `json:"trigger"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(argAt(a, 0), &p) // 非对象/缺字段时保持零值，下面的白名单会拒绝
		if !slices.Contains(updateCheckTriggers, p.Trigger) || !slices.Contains(autoSkipReasons, p.Reason) {
			return map[string]any{"success": false}, nil
		}
		log.Printf("[updater] auto_skip trigger=%s reason=%s", p.Trigger, p.Reason)
		return map[string]any{"success": true}, nil
	}
	b.handlers["backupRepository:syncRendererSettings"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		// 对象才接受：null、数组、字符串都返回 false，与 TS 一致
		var m map[string]any
		raw := argAt(a, 0)
		if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &m) != nil {
			return false, nil
		}
		backup.setRaw(raw, m)
		return true, nil
	}
}
