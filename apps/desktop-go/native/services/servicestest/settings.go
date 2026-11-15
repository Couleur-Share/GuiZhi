package servicestest

import (
	"encoding/json"
	"sort"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// Settings 是 services.Settings 的假实现：内存里存一份 Values，可注入失败，记录写入次数。
// 零值可用，初值是默认设置。语义与真实实现一致：写入前归一化，没有变化不通知，失败不改值。
type Settings struct {
	mu      sync.Mutex
	values  settings.Values
	started bool
	err     error
	updates int
	next    int
	subs    map[int]func(settings.Values)
}

var _ services.Settings = (*Settings)(nil)

func (f *Settings) init() {
	if !f.started {
		f.values, f.started = settings.Default(), true
	}
}

// Set 直接设置当前值（不归一化、不通知），用来摆好测试的初始状态。
func (f *Settings) Set(v settings.Values) {
	f.mu.Lock()
	f.values, f.started = v.Clone(), true
	f.mu.Unlock()
}

// FailWith 让之后的 Update 都返回 err 且不改值；传 nil 取消。
func (f *Settings) FailWith(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// Values 返回当前值（副本）。
func (f *Settings) Values() settings.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	return f.values.Clone()
}

// Updates 返回成功写入（有变化）的次数。
func (f *Settings) Updates() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updates
}

func (f *Settings) Snapshot() (settings.Values, error) { return f.Values(), nil }

func (f *Settings) Update(fn func(*settings.Values)) (settings.Values, error) {
	f.mu.Lock()
	f.init()
	if f.err != nil {
		err, prev := f.err, f.values.Clone()
		f.mu.Unlock()
		return prev, err
	}
	next := f.values.Clone()
	fn(&next)
	next.Normalize()
	if next.Equal(f.values) {
		prev := f.values.Clone()
		f.mu.Unlock()
		return prev, nil
	}
	f.values = next.Clone()
	f.updates++
	ids := make([]int, 0, len(f.subs))
	for id := range f.subs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	fns := make([]func(settings.Values), len(ids))
	for i, id := range ids {
		fns[i] = f.subs[id]
	}
	f.mu.Unlock()
	for _, fire := range fns { // 锁外通知：订阅者里再调用 Snapshot/Update 不会死锁
		fire(next.Clone())
	}
	return next, nil
}

func (f *Settings) Subscribe(fn func(settings.Values)) (cancel func()) {
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[int]func(settings.Values){}
	}
	f.next++
	id := f.next
	f.subs[id] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
	}
}

// Subscribers 返回当前订阅者数量。
func (f *Settings) Subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

// Prefs 是 services.Prefs 的假实现：内存里的键值表，可让 Set 失败。零值可用。
type Prefs struct {
	mu      sync.Mutex
	values  map[string]json.RawMessage
	setErr  error
	setKeys []string
}

var _ services.Prefs = (*Prefs)(nil)

// FailSetWith 让之后的 Set 都返回 err 且不写入；传 nil 取消。
func (f *Prefs) FailSetWith(err error) {
	f.mu.Lock()
	f.setErr = err
	f.mu.Unlock()
}

// Put 直接放入一个原始 JSON 值，用来摆好测试的初始状态（含损坏的数据）。
func (f *Prefs) Put(key, rawJSON string) {
	f.mu.Lock()
	if f.values == nil {
		f.values = map[string]json.RawMessage{}
	}
	f.values[key] = json.RawMessage(rawJSON)
	f.mu.Unlock()
}

// SetKeys 返回成功写入过的键（按写入顺序，含重复）。
func (f *Prefs) SetKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.setKeys...)
}

func (f *Prefs) Get(key string) (json.RawMessage, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	return v, ok
}

func (f *Prefs) Set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = map[string]json.RawMessage{}
	}
	f.values[key] = raw
	f.setKeys = append(f.setKeys, key)
	return nil
}
