package main

// 对应 packages/db/src/ai-usage.ts 与 ipc/ai.ipc.ts 里的 ai:usageRecord / ai:usageSummary / ai:usageClear。
// 日期按本机时区的自然日聚合（与 TS 的 Date.getFullYear/getMonth/getDate 一致）。

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

const aiUsageRetentionDays = 90

// toLocalDay 对应 toLocalDay(timestamp)：timestamp 为毫秒；JS 的 Date 会把小数截断。
func toLocalDay(ms float64) string {
	return time.UnixMilli(int64(math.Trunc(ms))).In(time.Local).Format("2006-01-02")
}

func (l *Library) aiUsageRecord(ctx context.Context, scenario, model string, prompt, completion float64, failed bool) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	nowF := float64(nowMs())
	trunc := func(f float64) int64 { return int64(math.Max(0, math.Trunc(f))) }
	failedN := int64(0)
	if failed {
		failedN = 1
	}
	_, err := l.db.Transaction(ctx, []sqlite.Statement{
		{
			SQL: `INSERT INTO ai_usage_daily
         (day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens, updated_at)
       VALUES (?, ?, ?, 1, ?, ?, ?, ?)
       ON CONFLICT(day, scenario, model) DO UPDATE SET
         calls = calls + 1,
         failed_calls = failed_calls + excluded.failed_calls,
         prompt_tokens = prompt_tokens + excluded.prompt_tokens,
         completion_tokens = completion_tokens + excluded.completion_tokens,
         updated_at = excluded.updated_at`,
			Args: []any{toLocalDay(nowF), scenario, model, failedN, trunc(prompt), trunc(completion), int64(nowF)},
		},
		{
			SQL:  "DELETE FROM ai_usage_daily WHERE day < ?",
			Args: []any{toLocalDay(nowF - aiUsageRetentionDays*24*60*60*1000)},
		},
	})
	return err
}

type usageTotals struct {
	calls, failed, prompt, completion int64
}

func (u *usageTotals) add(calls, failed, prompt, completion int64) {
	u.calls += calls
	u.failed += failed
	u.prompt += prompt
	u.completion += completion
}

type usageGroup struct {
	key string
	usageTotals
}

func (l *Library) aiUsageSummary(ctx context.Context, days float64) (any, error) {
	since := toLocalDay(float64(nowMs()) - math.Max(0, days-1)*24*60*60*1000)
	rows, err := l.db.Query(ctx, `SELECT day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens
       FROM ai_usage_daily WHERE day >= ? ORDER BY day DESC`, since)
	if err != nil {
		return nil, err
	}
	var totals usageTotals
	var scenarios, models []*usageGroup
	find := func(list *[]*usageGroup, key string) *usageGroup {
		for _, g := range *list {
			if g.key == key {
				return g
			}
		}
		g := &usageGroup{key: key}
		*list = append(*list, g)
		return g
	}
	for _, r := range rows.Values {
		calls, failed, prompt, completion := asInt(r[3]), asInt(r[4]), asInt(r[5]), asInt(r[6])
		totals.add(calls, failed, prompt, completion)
		find(&scenarios, r[1].(string)).add(calls, failed, prompt, completion)
		model, _ := r[2].(string)
		if model == "" {
			model = "(unknown)"
		}
		find(&models, model).add(calls, failed, prompt, completion)
	}
	byCalls := func(list []*usageGroup) {
		sort.SliceStable(list, func(i, j int) bool { return list[i].calls > list[j].calls })
	}
	byCalls(scenarios)
	byCalls(models)
	out := func(list []*usageGroup, field string) []any {
		res := make([]any, 0, len(list))
		for _, g := range list {
			res = append(res, map[string]any{
				field: g.key, "calls": g.calls, "failedCalls": g.failed, "promptTokens": g.prompt, "completionTokens": g.completion,
			})
		}
		return res
	}
	return map[string]any{
		"days": days, "calls": totals.calls, "failedCalls": totals.failed, "promptTokens": totals.prompt, "completionTokens": totals.completion,
		"byScenario": out(scenarios, "scenario"), "byModel": out(models, "model"),
	}, nil
}

func (l *Library) registerAIUsage(b *Bridge) {
	b.handlers["ai:usageRecord"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		entry, _ := jsParse(string(argAt(a, 0)))
		scenario, ok1 := jsGet(entry, "scenario").(string)
		model, ok2 := jsGet(entry, "model").(string)
		if !vTruthy(entry) || !ok1 || !ok2 {
			return nil, nil
		}
		// Number(x) || 0：NaN 与 0 都按 0
		num := func(key string) float64 {
			f := jsToNumber(jsGet(entry, key))
			if math.IsNaN(f) {
				return 0
			}
			return f
		}
		failed := jsStrictEqual(jsGet(entry, "failed"), true)
		return nil, l.aiUsageRecord(ctx, scenario, model, num("promptTokens"), num("completionTokens"), failed)
	}
	b.handlers["ai:usageSummary"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		v, _ := jsParse(string(argAt(a, 0)))
		n := jsToNumber(v)
		if math.IsNaN(n) || n == 0 {
			n = 30 // Number(days) || 30
		}
		return l.aiUsageSummary(ctx, math.Min(math.Max(1, n), 90))
	}
	b.handlers["ai:usageClear"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		_, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "DELETE FROM ai_usage_daily"}})
		return nil, err
	}
}
