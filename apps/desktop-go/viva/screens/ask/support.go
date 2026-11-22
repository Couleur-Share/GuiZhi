// Package ask 是 AI 问答（渲染层 AskWorkspace / SidebarAskPanel / ArticleConversation 及
// stores/ask.store.ts、article-ask.store.ts、ask-history.store.ts）的状态机。
//
// 状态机只依赖 services.Ask 的窄接口：会话切换、保存重试、流式回答、取消、过时请求丢弃、证据清除都在这里，
// 界面层只读 View 快照。所有方法同步、可从任意 goroutine 调用；变化后调用 invalidate 请求重绘。
//
// Package ask holds the state machines of the AI Q&A screens.
package ask

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"guizhi-mygo-shell/native/services"
)

// 一个会话标题最多取第一个问题的前 30 个 UTF-16 码元（ask.store.ts 的 SESSION_TITLE_MAX_LENGTH）。
const sessionTitleMax = 30

// PrefActiveSession 是「上次活跃会话」在界面偏好里的键（渲染层存在 localStorage 的 guizhi-ask-active-session）。
// 值是会话 ID 字符串，空串表示没有。
const PrefActiveSession = "ask.activeSession"

// isJSSpace 与 JS String.prototype.trim 的空白集合一致（含 U+00A0、全角空格与 BOM）。
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// utf16Prefix 取 s 的前 n 个 UTF-16 码元（JS 的 slice(0, n)）。切口落在代理对中间时丢掉那半个字符：
// Go 的字符串存不下孤立代理项，写进数据库也只会是乱码。
func utf16Prefix(s string, n int) string {
	units := 0
	for i, r := range s {
		w := len(utf16.Encode([]rune{r}))
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// randomHex 返回 n 个字节的十六进制随机串。
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// defaultID 生成 `<前缀>-<毫秒时间>-<6 位随机串>`，与渲染层 createMessageId / createSessionId 同形。
func defaultID(prefix string) string {
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixMilli(), randomHex(3))
}

// defaultUUID 生成 v4 UUID（本文会话 ID，对应 crypto.randomUUID）。
func defaultUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// saveQueue 让同一个会话 ID 的写入按提交顺序串行执行（conversation-persistence.ts 的 queueConversationSave）：
// 旧快照不能在新快照之后落库。前一个任务失败不影响后一个继续。进程内全局共享——全局问答与本文问答共用同一个 ID 空间。
type saveQueue struct {
	mu    sync.Mutex
	tails map[string]chan struct{}
}

var saves = &saveQueue{tails: map[string]chan struct{}{}}

// do 排在该 ID 之前所有任务之后执行 fn，并阻塞到 fn 返回。
func (q *saveQueue) do(id string, fn func()) {
	done := make(chan struct{})
	q.mu.Lock()
	prev := q.tails[id]
	q.tails[id] = done
	q.mu.Unlock()
	if prev != nil {
		<-prev
	}
	defer func() {
		close(done)
		q.mu.Lock()
		if q.tails[id] == done {
			delete(q.tails, id)
		}
		q.mu.Unlock()
	}()
	fn()
}

// Evidence 是「已清除证据的条目」集合（conversation-evidence.ts）：当前窗口也遵守清除标记，
// 迟到的流和已展开的片段不能再次暴露旧证据。全局问答与本文问答共用一份，清除时通知双方重新脱敏。
type Evidence struct {
	mu        sync.Mutex
	ids       map[string]bool
	order     []string
	listeners []func()
}

// Clear 登记一批已清除证据的条目，并通知监听者（监听者在调用方 goroutine 上同步执行）。
func (e *Evidence) Clear(ids []string) {
	e.mu.Lock()
	if e.ids == nil {
		e.ids = map[string]bool{}
	}
	for _, id := range ids {
		if !e.ids[id] {
			e.ids[id] = true
			e.order = append(e.order, id)
		}
	}
	listeners := append([]func(){}, e.listeners...)
	e.mu.Unlock()
	for _, fn := range listeners {
		fn()
	}
}

// OnCleared 注册清除通知（进程生命周期内不取消，与渲染层模块级监听一致）。
func (e *Evidence) OnCleared(fn func()) {
	e.mu.Lock()
	e.listeners = append(e.listeners, fn)
	e.mu.Unlock()
}

// IDs 返回已登记的全部条目 ID（按登记顺序）。
func (e *Evidence) IDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.order...)
}

// Has 报告条目的证据是否已被清除。
func (e *Evidence) Has(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ids[id]
}

// sanitizeList 对一组会话消息做证据脱敏（sanitizeCachedMessages）：没有清除记录时原样返回；
// 其余经服务端的同一份实现往返一次，与保存时落库的内容一致。
func sanitizeList[T any](svc services.Ask, ids []string, items []T) ([]T, error) {
	if len(ids) == 0 || len(items) == 0 {
		return items, nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return items, err
	}
	out, err := svc.SanitizeConversation(string(raw), ids)
	if err != nil {
		return items, err
	}
	var cleaned []T
	if err := json.Unmarshal([]byte(out), &cleaned); err != nil {
		return items, err
	}
	return cleaned, nil
}
