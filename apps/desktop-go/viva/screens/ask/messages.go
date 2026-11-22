package ask

// 会话消息的序列化与读回（ask.store.ts 的 parseStoredMessages / article-ask.store.ts 的 parseArticleMessages）。
//
// messagesJson 与渲染层共用同一格式：这里写出的会话渲染层读得了，渲染层写下的会话这里也读得了。
// 读回时的校验口径与渲染层一致——格式损坏就报错，不能把损坏的记录当成空会话（否则下一次保存会把它覆盖掉）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

var (
	errRecordCorrupt  = errors.New("问答记录格式损坏")
	errMessageCorrupt = errors.New("问答消息格式损坏")
)

// Class 是一轮失败的四种展示归类（界面据此选文案与按钮）。
type Class string

const (
	ClassNone          Class = ""               // 没有失败
	ClassNotConfigured Class = "not-configured" // 模型未配置：引导去设置
	ClassNoSource      Class = "no-source"      // 知识库没有相关资料：显示引导文案
	ClassStopped       Class = "stopped"        // 用户点了停止
	ClassGeneric       Class = "generic"        // 其余失败，带原因
)

// ClassOf 归类一轮问答的失败：status 不是 error 时返回 ClassNone。
func ClassOf(m services.AskMessage) Class {
	if m.Status != services.AskStatusError {
		return ClassNone
	}
	switch {
	case m.ErrorKind == services.AskErrorNotConfigured:
		return ClassNotConfigured
	case m.ErrorKind == services.AskErrorNoSource:
		return ClassNoSource
	case m.Stopped:
		return ClassStopped
	}
	return ClassGeneric
}

// normalizeAsk 保证写出的 JSON 里 sources 与 steps 永远是数组（nil 切片会写成 null，渲染层读回时当成缺失，不如直接写空数组）。
func normalizeAsk(m services.AskMessage) services.AskMessage {
	if m.Sources == nil {
		m.Sources = []services.AskSource{}
	}
	if m.Steps == nil {
		m.Steps = []string{}
	}
	return m
}

func normalizeAskAll(msgs []services.AskMessage) []services.AskMessage {
	out := make([]services.AskMessage, len(msgs))
	for i, m := range msgs {
		out[i] = normalizeAsk(m)
	}
	return out
}

// marshalAskMessages 序列化全局问答的消息列表。
func marshalAskMessages(msgs []services.AskMessage) (string, error) {
	raw, err := json.Marshal(normalizeAskAll(msgs))
	return string(raw), err
}

// jsonObject 把消息元素读成「字段名 → 原始 JSON」，元素不是对象时返回 false。
func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

// stringField 读字符串字段；缺失或类型不对返回 false。
func stringField(obj map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := obj[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// isJSONArray 报告字段是不是 JSON 数组。
func isJSONArray(obj map[string]json.RawMessage, key string) bool {
	raw := strings.TrimSpace(string(obj[key]))
	return strings.HasPrefix(raw, "[")
}

// decodeField 把字段解到 out；字段缺失时不动 out，解码失败当作消息损坏。
func decodeField(obj map[string]json.RawMessage, key string, out any) error {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w：%s", errMessageCorrupt, key)
	}
	return nil
}

// parseStoredMessages 读回全局问答的会话消息：上次没完成的 running 消息标记为「已中断」。
func parseStoredMessages(messagesJSON string, newID func(string) string) ([]services.AskMessage, error) {
	var elements []json.RawMessage
	if err := json.Unmarshal([]byte(messagesJSON), &elements); err != nil {
		var probe any
		if json.Unmarshal([]byte(messagesJSON), &probe) != nil {
			return nil, errors.New("问答记录格式损坏，请恢复备份或重试读取")
		}
		return nil, errRecordCorrupt
	}
	out := make([]services.AskMessage, 0, len(elements))
	for _, element := range elements {
		obj, ok := jsonObject(element)
		if !ok {
			return nil, errMessageCorrupt
		}
		question, ok := stringField(obj, "question")
		if !ok {
			return nil, errMessageCorrupt
		}
		m := services.AskMessage{Question: question, Sources: []services.AskSource{}, Steps: []string{}}
		if id, ok := stringField(obj, "id"); ok {
			m.ID = id
		} else {
			m.ID = newID("ask")
		}
		m.Answer, _ = stringField(obj, "answer")
		if isJSONArray(obj, "sources") {
			if err := decodeField(obj, "sources", &m.Sources); err != nil {
				return nil, err
			}
		}
		if isJSONArray(obj, "evidenceSources") {
			if err := decodeField(obj, "evidenceSources", &m.EvidenceSources); err != nil {
				return nil, err
			}
		}
		if isJSONArray(obj, "steps") {
			if err := decodeField(obj, "steps", &m.Steps); err != nil {
				return nil, err
			}
		}
		if isJSONArray(obj, "warnings") {
			if err := decodeField(obj, "warnings", &m.Warnings); err != nil {
				return nil, err
			}
		}
		status, _ := stringField(obj, "status")
		switch status {
		case services.AskStatusDone:
			m.Status = services.AskStatusDone
			m.Error, _ = stringField(obj, "error")
			m.ErrorKind, _ = stringField(obj, "errorKind")
		case services.AskStatusRunning:
			m.Status = services.AskStatusError
			m.Error = locales.AskSessionInterrupted()
			m.ErrorKind = services.AskErrorGeneric
		default:
			m.Status = services.AskStatusError
			m.Error, _ = stringField(obj, "error")
			m.ErrorKind, _ = stringField(obj, "errorKind")
		}
		m.Model, _ = stringField(obj, "model")
		_ = decodeField(obj, "usedFallback", &m.UsedFallback)
		_ = decodeField(obj, "truncated", &m.Truncated)
		_ = decodeField(obj, "stopped", &m.Stopped)
		if status == services.AskStatusRunning {
			m.Stopped = false
		}
		out = append(out, m)
	}
	return out, nil
}

// parseArticleMessages 读回本文问答的会话消息：running 的标记为「已中断」并清掉步骤提示。
func parseArticleMessages(messagesJSON string) ([]aiflow.ArticleMessage, error) {
	var elements []json.RawMessage
	if err := json.Unmarshal([]byte(messagesJSON), &elements); err != nil {
		return nil, errRecordCorrupt
	}
	out := make([]aiflow.ArticleMessage, 0, len(elements))
	for _, element := range elements {
		obj, ok := jsonObject(element)
		if !ok {
			return nil, errMessageCorrupt
		}
		for _, key := range []string{"id", "question", "answer"} {
			if _, ok := stringField(obj, key); !ok {
				return nil, errMessageCorrupt
			}
		}
		var m aiflow.ArticleMessage
		if err := json.Unmarshal(element, &m); err != nil {
			return nil, errMessageCorrupt
		}
		if !isJSONArray(obj, "sources") || m.Sources == nil {
			m.Sources = []aiflow.ArticleSource{}
		}
		if !isJSONArray(obj, "warnings") || m.Warnings == nil {
			m.Warnings = []string{}
		}
		if m.Status == services.AskStatusRunning {
			interrupted := locales.AskSessionInterrupted()
			m.Status = services.AskStatusError
			m.Error = &interrupted
			m.Step = nil
		}
		out = append(out, m)
	}
	return out, nil
}

// normalizeArticle 保证写出的 JSON 里 sources 与 warnings 永远是数组。
func normalizeArticle(m aiflow.ArticleMessage) aiflow.ArticleMessage {
	if m.Sources == nil {
		m.Sources = []aiflow.ArticleSource{}
	}
	if m.Warnings == nil {
		m.Warnings = []string{}
	}
	return m
}

func normalizeArticleAll(msgs []aiflow.ArticleMessage) []aiflow.ArticleMessage {
	out := make([]aiflow.ArticleMessage, len(msgs))
	for i, m := range msgs {
		out[i] = normalizeArticle(m)
	}
	return out
}

func marshalArticleMessages(msgs []aiflow.ArticleMessage) (string, error) {
	raw, err := json.Marshal(normalizeArticleAll(msgs))
	return string(raw), err
}

// buildSessionTitle 取第一个问题的前 30 个码元作标题；没有问题时用「新对话」。
func buildSessionTitle(msgs []services.AskMessage) string {
	if len(msgs) > 0 {
		if title := utf16Prefix(jsTrim(msgs[0].Question), sessionTitleMax); title != "" {
			return title
		}
	}
	return locales.AskUntitledSession()
}

// buildArticleTitle 本文会话的标题：第一个问题的前 30 个码元（渲染层 question.slice(0, 30)，不先 trim）。
func buildArticleTitle(msgs []aiflow.ArticleMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	return utf16Prefix(msgs[0].Question, sessionTitleMax)
}
