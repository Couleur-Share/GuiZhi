package kit

import (
	"testing"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// 渲染层 SOURCE_PLATFORMS 的全部平台：新增平台漏配会在这里挡下（侧栏与表格直接按它索引）。
var sourcePlatforms = []string{
	"douyin", "bilibili", "xiaohongshu", "youtube", "v2ex", "nga", "nodeseek", "linuxdo", "appinn", "twolibra",
	"wechat", "web", "local",
}

// 对应 platform-logos.test.tsx「每个平台都配了图标」：每个平台都有名称与图标。
func TestEveryPlatformHasLabelAndIcon(t *testing.T) {
	if len(platformTable) != len(sourcePlatforms) {
		t.Fatalf("平台表有 %d 项，渲染层有 %d 个平台", len(platformTable), len(sourcePlatforms))
	}
	for _, id := range sourcePlatforms {
		meta, ok := platformTable[id]
		if !ok {
			t.Fatalf("平台 %q 没有配图标", id)
		}
		if meta.label() == "" || meta.icon.ViewBox == 0 {
			t.Errorf("平台 %q 的名称或图标为空", id)
		}
	}
}

// 对应「旧版本认不出的平台走兜底而不是抛异常」：值来自数据库，查不到时退回「未知来源」。
func TestUnknownPlatformFallsBack(t *testing.T) {
	if got := PlatformLabel("mastodon"); got != "未知来源" {
		t.Errorf("未知平台的名称 = %q，要 未知来源", got)
	}
	if lookupPlatform("mastodon").icon.ViewBox != icons.CircleHelp.ViewBox {
		t.Error("未知平台应当用 CircleHelp 图标")
	}
}

// 近黑色的品牌（抖音、V2EX）跟着主题走，不写死品牌色：写死会在深色主题里整个消失。
func TestNearBlackBrandsFollowTheme(t *testing.T) {
	for _, id := range []string{"douyin", "v2ex"} {
		if kind := platformTable[id].color.kind; kind != colorForeground {
			t.Errorf("%s 的着色应当跟前景色走（text-foreground），实际 %d", id, kind)
		}
	}
	light, dark := tokens.Light(217, 91), tokens.Dark(217, 91)
	c := platformTable["douyin"].color
	if c.resolve(light, tokens.Color{}, false) != light.Foreground || c.resolve(dark, tokens.Color{}, true) != dark.Foreground {
		t.Error("抖音应当在两个主题里各取各的前景色")
	}
}

// 品牌色、成对色与继承色的求值。
func TestBrandColorResolve(t *testing.T) {
	th := tokens.Light(217, 91)
	current := tokens.RGBA(1, 2, 3, 1)
	if got := platformTable["youtube"].color.resolve(th, current, false); got != tokens.RGBA(255, 0, 0, 1) {
		t.Errorf("YouTube 的品牌色 = %v，要 #FF0000", got)
	}
	if got := platformTable["youtube"].color.resolve(th, current, true); got != tokens.RGBA(255, 0, 0, 1) {
		t.Errorf("品牌色不随主题变，深色下 = %v", got)
	}
	wechat := platformTable["wechat"].color
	if got := wechat.resolve(th, current, false); got != tokens.RGBA(22, 163, 74, 1) {
		t.Errorf("微信公众号浅色 = %v，要 green-600", got)
	}
	if got := wechat.resolve(th, current, true); got != tokens.RGBA(74, 222, 128, 1) {
		t.Errorf("微信公众号深色 = %v，要 green-400", got)
	}
	if got := platformTable["web"].color.resolve(th, current, false); got != current {
		t.Errorf("网页（text-current）应当继承所在位置的文字色，实际 %v", got)
	}
}
