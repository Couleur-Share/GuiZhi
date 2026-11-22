package services

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func testConfig() AIConfig {
	return AIConfig{
		Providers: []AIProvider{{ID: "p1", Name: "Main", Provider: "openai", APIProtocol: AIProtocolOpenAI, APIKey: "sk-secret-1", APIURL: "https://api.openai.com", Enabled: true}},
		Models: []AIModel{
			{ID: "m1", ProviderID: "p1", Provider: "openai", APIProtocol: AIProtocolOpenAI, APIKey: "sk-secret-1", APIURL: "https://api.openai.com", Model: "gpt-4o", IsDefault: true, Enabled: true,
				Capabilities: AICapabilities{Chat: true, Vision: true}},
			{ID: "m2", ProviderID: "p1", Provider: "openai", APIProtocol: AIProtocolOpenAI, APIKey: "sk-secret-1", APIURL: "https://api.openai.com", Model: "text-embedding-3-small", Enabled: true,
				Capabilities: AICapabilities{Embedding: true}},
		},
		Routes: map[string]string{},
	}
}

func TestAIRedactionNeverPrintsKey(t *testing.T) {
	cfg := testConfig()
	cfg.AILegacy = AILegacy{Provider: "openai", APIKey: "sk-legacy-secret", APIURL: "https://x", Model: "m"}
	for name, value := range map[string]any{
		"provider": cfg.Providers[0], "model": cfg.Models[0], "legacy": cfg.AILegacy, "config": cfg,
		"quick": QuickSetupProvider{APIKey: "sk-secret-1"},
	} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			out := fmt.Sprintf(format, value)
			if strings.Contains(out, "secret") {
				t.Errorf("%s 用 %s 打印泄露了密钥：%s", name, format, out)
			}
		}
	}
	if out := fmt.Sprintf("%v", []AIModel{cfg.Models[0]}); strings.Contains(out, "secret") {
		t.Errorf("切片里的模型泄露了密钥：%s", out)
	}
}

func TestResolveRouteRules(t *testing.T) {
	cfg := testConfig()
	if m, ok := cfg.ResolveRoute(AIRouteMainText); !ok || m.ID != "m1" {
		t.Fatalf("mainText 应回退到默认模型 m1，实际 %v %v", m.ID, ok)
	}
	if m, ok := cfg.ResolveRoute(AIRouteEmbedding); !ok || m.ID != "m2" {
		t.Fatalf("embedding 应取向量模型 m2，实际 %v %v", m.ID, ok)
	}
	if _, ok := cfg.ResolveRoute(AIRouteAudioText); ok {
		t.Fatal("audioText 没有显式设置时不应回退")
	}
	if _, ok := cfg.ResolveRoute(AIRouteImageGen); ok {
		t.Fatal("imageGen 没有候选时应为空")
	}
	// 转写路由：对话模型是合法候选，显式设置后生效。
	cfg = cfg.SetRoute(AIRouteAudioText, "m1")
	if m, ok := cfg.ResolveRoute(AIRouteAudioText); !ok || m.ID != "m1" {
		t.Fatalf("显式设置 audioText 后应生效，实际 %v %v", m.ID, ok)
	}
	// 供应商停用后，其模型不再是候选。
	cfg = cfg.UpdateProvider("p1", func(p *AIProvider) { p.Enabled = false })
	if _, ok := cfg.ResolveRoute(AIRouteMainText); ok {
		t.Fatal("供应商停用后 mainText 不应再解析出模型")
	}
}

func TestEndpointGroupsSyntheticAndSorted(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = append(cfg.Providers, AIProvider{ID: "p0", Provider: "anthropic", APIProtocol: AIProtocolAnthropic, APIURL: "https://api.anthropic.com", Enabled: true})
	// 没有供应商记录的模型合成分组；连接信息相同的归到同一组（与渲染层一致）。
	cfg.Models = append(cfg.Models,
		AIModel{ID: "x1", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIKey: "k", APIURL: "http://a", Model: "a", Enabled: true},
		AIModel{ID: "x2", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIKey: "k", APIURL: "http://a", Model: "b", Enabled: true},
	)
	groups := cfg.EndpointGroups()
	if len(groups) != 3 {
		t.Fatalf("应有 3 个分组（2 个记录 + 1 个合成），实际 %d", len(groups))
	}
	var order []string
	for _, g := range groups {
		order = append(order, g.Provider)
	}
	if strings.Join(order, ",") != "anthropic,custom,openai" {
		t.Fatalf("应按供应商标识排序，实际 %v", order)
	}
	for _, g := range groups {
		if g.Provider == "custom" && (g.ProviderConfigID != "" || len(g.Models) != 2) {
			t.Fatalf("合成分组没有供应商记录且含 2 个模型：%+v", g)
		}
		if g.ProviderConfigID == "p1" && len(g.Models) != 2 {
			t.Fatalf("p1 应含 2 个模型，实际 %d", len(g.Models))
		}
	}
}

func TestUpdateProviderSyncsModelsAndNormalizes(t *testing.T) {
	cfg := testConfig().UpdateProvider("p1", func(p *AIProvider) {
		p.APIKey = "  sk-new  "
		p.APIURL = " https://proxy.example/v1 "
		p.Name = "  "
	})
	if p := cfg.Providers[0]; p.APIKey != "sk-new" || p.APIURL != "https://proxy.example/v1" || p.Name != "" {
		t.Fatalf("端点字段应去空白：%v", p)
	}
	for _, m := range cfg.Models {
		if m.APIKey != "sk-new" || m.APIURL != "https://proxy.example/v1" {
			t.Fatalf("旗下模型应同步连接字段：%v", m)
		}
	}
}

func TestModelOpsDefaultsAndRoutes(t *testing.T) {
	cfg := testConfig().SetRoute(AIRouteMainText, "m1").SetRoute(AIRouteFastText, "m1")
	next := cfg.DeleteModel("m1")
	if len(next.Models) != 1 || !next.Models[0].IsDefault {
		t.Fatalf("删除默认模型后第一个剩余模型应接任默认：%v", next.Models)
	}
	if len(next.Routes) != 0 {
		t.Fatalf("指向被删模型的路由应一并清掉：%v", next.Routes)
	}
	if len(cfg.Models) != 2 || cfg.Routes[AIRouteMainText] != "m1" {
		t.Fatal("更新函数不应改动接收者")
	}

	added, id := cfg.AddModel(AIModel{Provider: "openai", APIProtocol: AIProtocolOpenAI, APIKey: "zzz", APIURL: "x", Model: "gpt-5", Enabled: true,
		ProviderID: "p1", Capabilities: AICapabilities{Chat: false, Vision: true}})
	var got AIModel
	for _, m := range added.Models {
		if m.ID == id {
			got = m
		}
	}
	if got.IsDefault || got.APIKey != "sk-secret-1" || got.Capabilities.Vision {
		t.Fatalf("新增模型应镜像供应商连接字段、非默认，且视觉依附对话：%v %+v", got, got.Capabilities)
	}

	set := cfg.SetDefaultModel("m2")
	if set.Models[0].IsDefault || !set.Models[1].IsDefault {
		t.Fatal("SetDefaultModel 应只留 m2 为默认")
	}
}

func TestDeleteProviderRemovesModelsAndRoutes(t *testing.T) {
	cfg := testConfig().SetRoute(AIRouteEmbedding, "m2")
	next := cfg.DeleteProvider("p1", []string{"m1", "m2"})
	if len(next.Providers) != 0 || len(next.Models) != 0 || len(next.Routes) != 0 {
		t.Fatalf("应连同模型与路由一起删：%v", next)
	}
	// 合成分组：providerID 为空，只删模型，不碰任何供应商记录。
	keep := cfg.DeleteProvider("", []string{"m1"})
	if len(keep.Providers) != 1 || len(keep.Models) != 1 {
		t.Fatalf("providerID 为空只删模型：%v", keep)
	}
}

func TestCapabilityToggles(t *testing.T) {
	c := DefaultAICapabilities()
	emb := c.ToggleEmbedding(true)
	if emb.Chat || !emb.Embedding || emb.Vision {
		t.Fatalf("勾选向量应成为专用模型：%+v", emb)
	}
	if v := emb.ToggleVision(true); !v.Chat || !v.Vision || v.Embedding {
		t.Fatalf("勾选视觉应回到对话并退出向量：%+v", v)
	}
	if off := emb.ToggleEmbedding(false); !off.Chat || off.Embedding {
		t.Fatalf("取消向量应回到对话：%+v", off)
	}
	r := AICapabilities{Chat: true, Reasoning: true}.ToggleAudioTranscription(true)
	if r.Reasoning || r.Chat || !r.AudioTranscription {
		t.Fatalf("勾选转写应关闭推理与对话：%+v", r)
	}
}

func TestInferModelCapabilities(t *testing.T) {
	cases := []struct {
		id   string
		want AICapabilities
	}{
		{"text-embedding-3-small", AICapabilities{Embedding: true}},
		{"whisper-1", AICapabilities{AudioTranscription: true}},
		{"dall-e-3", AICapabilities{ImageGeneration: true}},
		{"bge-reranker-v2", AICapabilities{Rerank: true}},
		{"gpt-4o", AICapabilities{Chat: true, Vision: true}},
		{"deepseek-r1", AICapabilities{Chat: true, Reasoning: true}},
		{"sonar-pro", AICapabilities{Chat: true, WebSearch: true}},
		{"llama-3", AICapabilities{Chat: true}},
	}
	for _, c := range cases {
		if got := InferModelCapabilities(c.id); got != c.want {
			t.Errorf("%s：期望 %+v，实际 %+v", c.id, c.want, got)
		}
	}
}

func TestModelCategory(t *testing.T) {
	cases := []struct{ provider, id, owner, want string }{
		{"anthropic", "whatever", "", "Claude"},
		{"custom", "claude-3-opus", "", "Claude"},
		{"", "mystery", "Google", "Gemini"},
		{"", "text-embedding-x", "", "Embedding"},
		{"", "zzz", "", "Other"},
	}
	for _, c := range cases {
		if got := ModelCategory(c.provider, c.id, c.owner); got != c.want {
			t.Errorf("%+v：期望 %s，实际 %s", c, c.want, got)
		}
	}
	if ProviderIconCategory("qwen") != "Qwen" || ProviderIconCategory("nobody") != "Other" {
		t.Error("ProviderIconCategory 取值不对")
	}
}

func TestQuickSetupBuildAndApply(t *testing.T) {
	roles := RolesForGoals(map[QuickSetupGoal]bool{GoalBasic: true, GoalVision: true, GoalSemantic: true})
	if len(roles) != 3 || roles[0] != RoleText || roles[1] != RoleEmbedding || roles[2] != RoleVision {
		t.Fatalf("角色顺序不对：%v", roles)
	}
	in := BuildQuickSetupInput(
		QuickSetupProvider{Name: "OpenAI", Provider: "OpenAI", APIProtocol: AIProtocolOpenAI, APIURL: " https://api.openai.com/v1 ", APIKey: " sk-qs "},
		roles,
		map[QuickSetupRole]string{RoleText: "gpt-4o", RoleVision: "gpt-4o", RoleEmbedding: "text-embedding-3-small"},
		map[QuickSetupRole]bool{RoleText: true, RoleVision: false, RoleEmbedding: true},
	)
	if len(in.Models) != 2 {
		t.Fatalf("同名模型应合并：%d", len(in.Models))
	}
	if m := in.Models[0]; !m.Capabilities.Chat || !m.Capabilities.Vision || m.Verified {
		t.Fatalf("合并后能力取并集、已验证取交集：%+v", m)
	}
	if in.Routes[AIRouteMainText] != 0 || in.Routes[AIRouteVisionText] != 0 || in.Routes[AIRouteEmbedding] != 1 {
		t.Fatalf("路由下标不对：%v", in.Routes)
	}

	base := testConfig().SetRoute(AIRouteMainText, "m1")
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	out := base.ApplyQuickSetup(in, now)
	if len(out.Providers) != 2 || len(out.Models) != 4 {
		t.Fatalf("应新增 1 个供应商与 2 个模型：%d %d", len(out.Providers), len(out.Models))
	}
	if out.Providers[1].APIKey != "sk-qs" || out.Providers[1].APIURL != "https://api.openai.com/v1" {
		t.Fatalf("向导里的服务应去空白：%v", out.Providers[1])
	}
	if out.Models[0].IsDefault || !out.Models[2].IsDefault {
		t.Fatal("新模型的第一个应成为默认，原有模型取消默认")
	}
	if out.Models[3].LastVerifiedAt != "2026-11-22T10:00:00.000Z" || out.Models[2].LastVerifiedAt != "" {
		t.Fatalf("已验证的模型应落 lastVerifiedAt：%q %q", out.Models[3].LastVerifiedAt, out.Models[2].LastVerifiedAt)
	}
	if out.Routes[AIRouteMainText] != out.Models[2].ID || out.Routes[AIRouteEmbedding] != out.Models[3].ID {
		t.Fatalf("路由应指向新模型：%v", out.Routes)
	}
	if got := RecommendModel([]string{"zzz", "text-embedding-3-small"}, RoleEmbedding); got != "text-embedding-3-small" {
		t.Fatalf("推荐应命中提示词：%s", got)
	}
	if got := RecommendModel([]string{"zzz", "yyy"}, RoleAudio); got != "zzz" {
		t.Fatalf("没有命中取第一个：%s", got)
	}
}

func TestDisplayNames(t *testing.T) {
	cfg := testConfig()
	label := DefaultProviderLabel
	if got := ModelDisplayName(cfg.Models[0], cfg.Providers, label); got != "gpt-4o (Main)" {
		t.Fatalf("显示名应带供应商：%s", got)
	}
	if got := ModelDisplayName(cfg.Models[0], nil, label); got != "gpt-4o" {
		t.Fatalf("没有供应商列表时不带供应商：%s", got)
	}
	if got := FormatModelWithProvider("openai-x", "OpenAI"); got != "openai-x" {
		t.Fatalf("模型名已含供应商名时不重复：%s", got)
	}
}

func TestNewAIIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		id := NewAIID("model")
		if !strings.HasPrefix(id, "model_") || seen[id] {
			t.Fatalf("ID 应带前缀且不重复：%s", id)
		}
		seen[id] = true
	}
}

func TestMaskAPIKeyOnlyShowsEnds(t *testing.T) {
	for _, short := range []string{"", "  ", "sk-1234", "12345678"} {
		if got := MaskAPIKey(short); got != "******" {
			t.Errorf("短密钥 %q 应全遮蔽：%q", short, got)
		}
	}
	key := "sk-live-9f2c-SECRET-7d41"
	got := MaskAPIKey(key)
	if got != "sk-****7d41" {
		t.Errorf("掩码 = %q", got)
	}
	if strings.Contains(got, "SECRET") {
		t.Error("掩码泄露了密钥中段")
	}
}

func TestEndpointCategoryAndBaseURL(t *testing.T) {
	if got := EndpointCategory("anthropic", nil); got != "Claude" {
		t.Errorf("已知供应商应按表归类：%q", got)
	}
	if got := EndpointCategory("custom", nil); got != "Other" {
		t.Errorf("无模型的未知供应商应归 Other：%q", got)
	}
	if got := EndpointCategory("custom", []AIModel{{Provider: "custom", Model: "gpt-4o"}}); got != "GPT" {
		t.Errorf("未知供应商应按首个模型推断：%q", got)
	}
	if got := BaseURL("https://api.example.com/v1/chat/completions"); got != "https://api.example.com/v1" {
		t.Errorf("基础地址 = %q", got)
	}
	if got := BaseURL("https://api.example.com/v1/#"); got != "https://api.example.com/v1/" {
		t.Errorf("# 结尾只去 #：%q", got)
	}
}
