package services

import (
	"testing"
	"time"
)

// 向导落盘：只有新模型里的第一个成为默认，原有模型一律取消默认；整份配置里恰好一个默认模型。
func TestApplyQuickSetupOnlyFirstNewModelIsDefault(t *testing.T) {
	in := QuickSetupInput{
		Provider: QuickSetupProvider{Name: "Proxy", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIURL: "https://proxy.example/v1", APIKey: "sk-proxy"},
		Models: []QuickSetupModel{
			{Name: "a", Model: "a", Capabilities: AICapabilities{Chat: true}},
			{Name: "b", Model: "b", Capabilities: AICapabilities{Chat: true}},
			{Name: "e", Model: "e", Capabilities: AICapabilities{Embedding: true}},
		},
		Routes: map[string]int{AIRouteMainText: 0, AIRouteEmbedding: 2},
	}
	out := testConfig().ApplyQuickSetup(in, time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	if len(out.Models) != 5 {
		t.Fatalf("应有 2 个原有加 3 个新模型：%d", len(out.Models))
	}
	var defaults []string
	for _, m := range out.Models {
		if m.IsDefault {
			defaults = append(defaults, m.Model)
		}
	}
	if len(defaults) != 1 || defaults[0] != "a" {
		t.Fatalf("默认模型应只有新模型里的第一个 a：%v", defaults)
	}
}

// 向导交来的路由下标越界（或为负）时忽略那一条，不 panic、不动配置里已有的同名路由。
func TestApplyQuickSetupIgnoresOutOfRangeRouteIndex(t *testing.T) {
	in := QuickSetupInput{
		Provider: QuickSetupProvider{Name: "Proxy", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIURL: "https://proxy.example/v1", APIKey: "sk-proxy"},
		Models: []QuickSetupModel{
			{Name: "a", Model: "a", Capabilities: AICapabilities{Chat: true}},
			{Name: "e", Model: "e", Capabilities: AICapabilities{Embedding: true}},
		},
		Routes: map[string]int{AIRouteMainText: 9, AIRouteFastText: -1, AIRouteEmbedding: 1},
	}
	base := testConfig().SetRoute(AIRouteMainText, "m1")
	out := base.ApplyQuickSetup(in, time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	if got := out.Routes[AIRouteMainText]; got != "m1" {
		t.Errorf("下标越界的路由应保持原值 m1：%q", got)
	}
	if got, ok := out.Routes[AIRouteFastText]; ok {
		t.Errorf("负下标不应写入路由：%q", got)
	}
	if got := out.Routes[AIRouteEmbedding]; got != out.Models[len(out.Models)-1].ID {
		t.Errorf("有效下标应指向对应的新模型：%q", got)
	}
}

// 按模型名推荐不区分大小写：中转站常返回大写或混写的模型名。
func TestRecommendModelIsCaseInsensitive(t *testing.T) {
	cases := []struct {
		role QuickSetupRole
		ids  []string
		want string
	}{
		{RoleText, []string{"zzz", "GPT-4O-Mini"}, "GPT-4O-Mini"},
		{RoleText, []string{"zzz", "Claude-3-Opus"}, "Claude-3-Opus"},
		{RoleEmbedding, []string{"zzz", "BGE-M3"}, "BGE-M3"},
		{RoleAudio, []string{"zzz", "Whisper-Large"}, "Whisper-Large"},
		{RoleImageGen, []string{"zzz", "DALL-E-3"}, "DALL-E-3"},
	}
	for _, c := range cases {
		if got := RecommendModel(c.ids, c.role); got != c.want {
			t.Errorf("%s %v：期望 %s，实际 %s", c.role, c.ids, c.want, got)
		}
	}
}

// 文生图路由与转写路由一样不回退：没有显式设置时即使有可用的文生图模型也不能解析出来，
// 否则界面显示「已配置」而实际调用报未配置；显式设置之后才生效。
func TestResolveRouteImageGenNeverFallsBack(t *testing.T) {
	cfg := testConfig()
	cfg.Models = append(cfg.Models, AIModel{ID: "img1", ProviderID: "p1", Provider: "openai", APIProtocol: AIProtocolOpenAI, APIKey: "sk-secret-1",
		APIURL: "https://api.openai.com", Model: "dall-e-3", Enabled: true, Capabilities: AICapabilities{ImageGeneration: true}})
	if got := cfg.RouteCandidates(AIRouteImageGen); len(got) != 1 || got[0].ID != "img1" {
		t.Fatalf("文生图候选应是 img1：%v", got)
	}
	if m, ok := cfg.ResolveRoute(AIRouteImageGen); ok {
		t.Fatalf("没有显式设置不应回退到候选：%v", m.ID)
	}
	cfg = cfg.SetRoute(AIRouteImageGen, "img1")
	if m, ok := cfg.ResolveRoute(AIRouteImageGen); !ok || m.ID != "img1" {
		t.Fatalf("显式设置后应生效：%v %v", m.ID, ok)
	}
}

// 删除的供应商名下有默认模型、而别的供应商还有模型时：第一个剩余模型接任默认，且只有它一个。
func TestDeleteProviderHandsDefaultToFirstRemainingModel(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = append(cfg.Providers, AIProvider{ID: "p2", Name: "Other", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIKey: "k2", APIURL: "https://b.example", Enabled: true})
	cfg.Models = append(cfg.Models,
		AIModel{ID: "m3", ProviderID: "p2", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIKey: "k2", APIURL: "https://b.example", Model: "x", Enabled: true, Capabilities: AICapabilities{Chat: true}},
		AIModel{ID: "m4", ProviderID: "p2", Provider: "custom", APIProtocol: AIProtocolOpenAI, APIKey: "k2", APIURL: "https://b.example", Model: "y", Enabled: true, Capabilities: AICapabilities{Chat: true}},
	)
	next := cfg.DeleteProvider("p1", []string{"m1", "m2"})
	if len(next.Providers) != 1 || next.Providers[0].ID != "p2" || len(next.Models) != 2 {
		t.Fatalf("应只剩 p2 与它的两个模型：%v %v", next.Providers, next.Models)
	}
	if !next.Models[0].IsDefault || next.Models[1].IsDefault {
		t.Errorf("第一个剩余模型应接任默认：%v %v", next.Models[0].IsDefault, next.Models[1].IsDefault)
	}
	// 被删的不是默认模型的供应商：默认保持不变。
	keep := cfg.DeleteProvider("p2", []string{"m3", "m4"})
	if len(keep.Models) != 2 || !keep.Models[0].IsDefault || keep.Models[1].IsDefault {
		t.Errorf("删非默认供应商不应动默认：%v", keep.Models)
	}
}

// 掩码先去首尾空白再判断长度：带空白的短密钥不能因为空白凑够长度而露出前后几位。
func TestMaskAPIKeyTrimsWhitespaceBeforeMeasuring(t *testing.T) {
	if got := MaskAPIKey("  sk-1234  "); got != "******" {
		t.Errorf("去空白后只有 7 个字符，应全遮蔽：%q", got)
	}
	if got := MaskAPIKey("\tsk-live-9f2c-SECRET-7d41\n"); got != "sk-****7d41" {
		t.Errorf("长密钥应只露出首 3 末 4：%q", got)
	}
}
