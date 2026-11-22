package servicestest

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
)

func TestAISettingsFakeRoundTripAndRecords(t *testing.T) {
	ctx := t.Context()
	f := &AISettingsFake{}

	cfg := services.AIConfig{Routes: map[string]string{}}
	cfg, id := cfg.AddProvider(services.AIProvider{Provider: "openai", APIKey: "sk-fake-secret", APIURL: "https://x", Enabled: true})
	if err := f.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := f.Load(ctx)
	if err != nil || len(got.Providers) != 1 || got.Providers[0].ID != id {
		t.Fatalf("应读回保存的配置：%v %v", got, err)
	}
	// 读到的是副本，改它不影响假实现里的状态。
	got.Providers[0].APIKey = "changed"
	if f.Current().Providers[0].APIKey != "sk-fake-secret" {
		t.Fatal("Load 应返回副本")
	}
	if len(f.Saves()) != 1 || f.Loads() != 1 {
		t.Fatalf("记录不对：saves=%d loads=%d", len(f.Saves()), f.Loads())
	}

	boom := errors.New("disk full")
	f.FailSave(boom)
	if err := f.Save(ctx, services.AIConfig{}); !errors.Is(err, boom) || len(f.Saves()) != 1 {
		t.Fatal("注入失败时应返回错误且不改状态")
	}
}

func TestAISettingsFakeTestFetchUsage(t *testing.T) {
	ctx := t.Context()
	f := &AISettingsFake{}

	out, err := f.TestModel(ctx, services.AIModel{ID: "m", Model: "gpt"})
	if err != nil || !out.Success || out.LatencyMs != 42 {
		t.Fatalf("默认测试应成功：%+v %v", out, err)
	}
	f.PutTestOutcome(services.AITestOutcome{Error: "401 unauthorized"})
	if out, _ := f.TestModel(ctx, services.AIModel{ID: "m"}); out.Success || out.Error == "" {
		t.Fatalf("摆好的失败结果应原样返回：%+v", out)
	}
	if len(f.Tests()) != 2 {
		t.Fatalf("应记录 2 次测试：%d", len(f.Tests()))
	}

	f.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{{ID: "a"}}})
	res, _ := f.FetchModels(ctx, "https://x", "sk-fetch-secret", "openai")
	if len(res.Models) != 1 {
		t.Fatalf("应返回摆好的模型列表：%+v", res)
	}
	call := f.Fetches()[0]
	if call.APIKey != "sk-fetch-secret" {
		t.Fatal("调用记录应保存密钥原值供断言")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if out := fmt.Sprintf(format, call); strings.Contains(out, "secret") {
			t.Fatalf("调用记录用 %s 打印泄露了密钥：%s", format, out)
		}
	}

	f.PutUsage(services.AIUsageSummary{Days: 30, Calls: 3})
	if u, _ := f.UsageSummary(ctx, 30); u.Calls != 3 || f.UsageDays()[0] != 30 {
		t.Fatalf("用量与天数记录不对：%+v", u)
	}
	if err := f.ClearUsage(ctx); err != nil || f.Clears() != 1 {
		t.Fatalf("清空应成功并计数：%v %d", err, f.Clears())
	}
	if u, _ := f.UsageSummary(ctx, 30); u.Calls != 0 {
		t.Fatal("清空后用量应为 0")
	}
}
