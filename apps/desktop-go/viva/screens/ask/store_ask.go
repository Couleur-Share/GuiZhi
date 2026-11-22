package ask

import (
	"context"
	"errors"
	"slices"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 提问、重答、删除单轮与停止（ask.store.ts 的 ask / retry / removeMessage / stop）。

// stoppedMessage 把还在 running 的消息标成「已停止」（写时复制）。
func (s *Store) stoppedMessages(msgs []services.AskMessage) []services.AskMessage {
	out := slices.Clone(msgs)
	for i, m := range out {
		if m.Status == services.AskStatusRunning {
			m.Status, m.Error, m.ErrorKind, m.Stopped = services.AskStatusError, locales.AskStopped(), services.AskErrorGeneric, true
			out[i] = m
		}
	}
	return out
}

// Stop 停止进行中的回答：取消请求，把 running 的轮次标成「已停止」，并在后台保存。没有进行中的回答时什么也不做。
func (s *Store) Stop() {
	s.mu.Lock()
	run := s.run
	if run == nil {
		s.mu.Unlock()
		return
	}
	s.run = nil
	run.cancel()
	s.view.IsRunning = false
	s.view.Messages = s.stoppedMessages(s.view.Messages)
	s.rev++
	s.mu.Unlock()
	s.wake()
	s.spawn(func() { s.persistActive() })
}

// current 报告 run 这一轮是否仍然有效：没被停止、没被换成新的一轮、会话也没切走。
func (s *Store) current(run *askRun, sessionID string) bool {
	return s.run == run && run.ctx.Err() == nil && s.view.ActiveSessionID == sessionID
}

// patchMessage 改写消息 id 的内容；这一轮已失效（停止、切换会话）时丢弃。
// touchesEvidence 为 true 时按证据清除记录给来源脱敏——迟到的流不能再次暴露已清除的证据。
func (s *Store) patchMessage(run *askRun, sessionID, id string, touchesEvidence bool, mutate func(m *services.AskMessage)) {
	for {
		s.mu.Lock()
		if !s.current(run, sessionID) {
			s.mu.Unlock()
			return
		}
		at := slices.IndexFunc(s.view.Messages, func(m services.AskMessage) bool { return m.ID == id })
		if at < 0 {
			s.mu.Unlock()
			return
		}
		next := s.view.Messages[at]
		mutate(&next)
		rev := s.rev
		ids := s.ev.IDs()
		if !touchesEvidence || len(ids) == 0 {
			s.apply(at, next)
			s.mu.Unlock()
			s.wake()
			return
		}
		s.mu.Unlock()

		// 脱敏是服务调用，不在锁内做；做完若这期间状态变了就按最新内容重来。
		cleaned, err := sanitizeList(s.svc, ids, normalizeAskAll([]services.AskMessage{{Sources: next.Sources, EvidenceSources: next.EvidenceSources}}))
		if err != nil {
			s.guard.Background("ask.sanitize", err)
		} else {
			next.Sources, next.EvidenceSources = cleaned[0].Sources, cleaned[0].EvidenceSources
		}
		s.mu.Lock()
		if s.rev != rev {
			s.mu.Unlock()
			continue
		}
		if !s.current(run, sessionID) {
			s.mu.Unlock()
			return
		}
		s.apply(at, next)
		s.mu.Unlock()
		s.wake()
		return
	}
}

// apply 写入第 at 条消息（写时复制）。调用方持锁。
func (s *Store) apply(at int, m services.AskMessage) {
	msgs := slices.Clone(s.view.Messages)
	msgs[at] = m
	s.view.Messages = msgs
	s.rev++
}

// Ask 发起一轮提问并阻塞到这一轮结束（界面在后台 goroutine 里调用）。
// 没有文本模型时提示「尚未配置 AI 服务」，不把一条注定失败的提问写进历史；正在回答时忽略。
func (s *Store) Ask(question string) {
	trimmed := jsTrim(question)
	if trimmed == "" || s.View().IsRunning {
		return
	}
	configured, err := s.svc.QAConfigured(s.ctx())
	if err != nil {
		s.guard.Report(locales.AskSend(), err)
		return
	}
	if !configured {
		s.guard.Report(locales.AskSend(), errors.New(locales.AskNotConfigured()))
		return
	}

	id := s.newID("ask")
	runCtx, cancel := context.WithCancel(s.ctx())
	run := &askRun{cancel: cancel, ctx: runCtx}
	defer cancel()

	s.mu.Lock()
	if s.view.IsRunning || s.view.ArticleSession != nil {
		s.mu.Unlock()
		return
	}
	// 多轮上下文：已完成的问答作为历史传入
	var history []services.AskTurn
	for _, m := range s.view.Messages {
		if m.Status == services.AskStatusDone {
			history = append(history, services.AskTurn{Question: m.Question, Answer: m.Answer})
		}
	}
	message := services.AskMessage{ID: id, Question: trimmed, Sources: []services.AskSource{}, Steps: []string{}, Status: services.AskStatusRunning}
	s.view.Messages = append(slices.Clone(s.view.Messages), message)
	s.view.IsRunning = true
	s.run = run
	sessionID := s.view.ActiveSessionID
	s.rev++
	s.mu.Unlock()
	s.wake()

	if !s.persistActive() {
		s.Stop()
		return
	}

	cb := services.AskCallbacks{
		OnStep: func(step string) {
			s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) { m.Steps = append(slices.Clone(m.Steps), step) })
		},
		// 回调给的是到目前为止的全量文本，直接覆盖即可
		OnText: func(text string) {
			s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) { m.Answer = text })
		},
		OnEvidence: func(sources []services.AskSource) {
			s.patchMessage(run, sessionID, id, true, func(m *services.AskMessage) {
				m.EvidenceSources = mergeEvidence(m.EvidenceSources, sources)
				m.Sources = slices.Clone(sources)
			})
			s.spawn(func() { s.persistActive() })
		},
		OnWarning: func(warning string) {
			s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) { m.Warnings = appendUnique(m.Warnings, warning) })
		},
	}
	answer, err := s.svc.AskKnowledge(runCtx, services.AskRequest{Question: trimmed, History: history}, cb)
	switch {
	case err == nil:
		s.patchMessage(run, sessionID, id, true, func(m *services.AskMessage) {
			m.Status, m.Answer, m.Sources = services.AskStatusDone, answer.Text, slices.Clone(answer.Sources)
			m.Model, m.UsedFallback, m.Truncated = answer.Model, answer.UsedFallback, answer.Truncated
		})
	case runCtx.Err() != nil || errors.Is(err, context.Canceled):
		s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) {
			m.Status, m.Error, m.ErrorKind, m.Stopped = services.AskStatusError, locales.AskStopped(), services.AskErrorGeneric, true
		})
	case errors.Is(err, services.ErrAINotConfigured):
		s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) {
			m.Status, m.ErrorKind = services.AskStatusError, services.AskErrorNotConfigured
		})
	case errors.Is(err, services.ErrAskNoSource):
		s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) {
			m.Status, m.ErrorKind, m.Answer = services.AskStatusError, services.AskErrorNoSource, ""
		})
	default:
		s.guard.Background("ask.answer", err)
		s.patchMessage(run, sessionID, id, false, func(m *services.AskMessage) {
			m.Status, m.Error, m.ErrorKind = services.AskStatusError, err.Error(), services.AskErrorGeneric
		})
	}

	s.mu.Lock()
	still := s.run == run && s.view.ActiveSessionID == sessionID
	if still {
		s.run = nil
		s.view.IsRunning = false
	}
	s.mu.Unlock()
	if still {
		s.wake()
		s.persistActive()
	}
}

// Retry 重新回答某一轮：丢弃它及其之后的消息，用同一个问题重跑（之后的轮次是基于这一轮的回答问出来的，留着会让多轮上下文对不上）。
func (s *Store) Retry(messageID string) {
	s.mu.Lock()
	if s.view.IsRunning {
		s.mu.Unlock()
		return
	}
	at := slices.IndexFunc(s.view.Messages, func(m services.AskMessage) bool { return m.ID == messageID })
	if at < 0 {
		s.mu.Unlock()
		return
	}
	question := s.view.Messages[at].Question
	s.view.Messages = slices.Clone(s.view.Messages[:at])
	s.rev++
	s.mu.Unlock()
	s.wake()
	s.Ask(question)
}

// RemoveMessage 删除单轮问答（问错了、答歪了，不必清空整个会话）：删光之后连会话一起删；保存失败就恢复原样，
// 不让界面显示的比库里存的少。
func (s *Store) RemoveMessage(messageID string) {
	s.mu.Lock()
	if s.view.IsRunning {
		s.mu.Unlock()
		return
	}
	previous := s.view.Messages
	remaining := slices.DeleteFunc(slices.Clone(previous), func(m services.AskMessage) bool { return m.ID == messageID })
	if len(remaining) == len(previous) { // 没有这一轮：什么也不用做
		s.mu.Unlock()
		return
	}
	if len(remaining) == 0 {
		id := s.view.ActiveSessionID
		s.mu.Unlock()
		if id != "" {
			s.DeleteSession(id)
		}
		return
	}
	s.view.Messages = remaining
	s.rev++
	rev := s.rev
	s.mu.Unlock()
	s.wake()
	if s.persistActive() {
		return
	}
	s.mu.Lock()
	if s.rev == rev {
		s.view.Messages = previous
		s.rev++
	}
	s.mu.Unlock()
	s.wake()
}
