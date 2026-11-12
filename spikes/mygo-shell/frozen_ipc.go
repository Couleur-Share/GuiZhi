package main

// 冻结功能的频道处理（见 docs/product-scope.md：研究、配图、发现视图、阅读配图、手机收集、阅读同步）。
// 这些功能不迁移，但渲染进程仍可能在页面加载时调用其中的读取类频道；没有处理器会让调用报
// 「频道未实现」，在页面上表现成错误态。这里统一处理：
//   - 配图风格的读写属于「配置迁移」的一部分，数据已有 Go 实现，直接开放（设置页可用）。
//   - 读取列表类频道返回空列表（功能在 Go 壳里不可用，数据仍保留在库里，不删除）。
//   - 其余冻结频道明确报「功能已冻结」，而不是静默成功。
// Frozen features: keep their read-only list channels quiet and fail the rest with a clear reason.

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
)

const errFeatureFrozen = "该功能已冻结（见 docs/product-scope.md），Go 壳不提供"

var frozenChannelPrefixes = []string{
	"research:", "illustration:", "discovery:", "discoveryView:", "discoveryCandidate:",
	"readingGraphics:", "mobile-capture:", "reading-sync:",
}

// frozenEmptyList 是冻结功能里「读取列表」的频道，返回空数组以免页面进入错误态。
var frozenEmptyList = map[string]bool{
	"research:list": true, "research:baselines": true, "discoveryView:list": true,
}

func isFrozenChannel(ch string) bool {
	for _, p := range frozenChannelPrefixes {
		if strings.HasPrefix(ch, p) {
			return true
		}
	}
	return false
}

// registerIllustrationStyles：配图风格预设的读写（其余配图功能冻结）。
func (l *Library) registerIllustrationStyles(b *Bridge) {
	b.handlers["illustration:styles"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.readIllustrationStyles(), nil
	}
	b.handlers["illustration:builtInStyles"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return illustrationBuiltins(), nil
	}
	b.handlers["illustration:saveStyles"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var raw any
		if len(a) > 0 {
			if parsed, err := parseOrdered(a[0]); err == nil {
				if root, ok := parsed.(*omap); ok {
					raw = root.vals["styles"]
				}
			}
		}
		styles, msg := l.writeIllustrationStyles(raw)
		if msg != "" {
			return map[string]any{"success": false, "error": msg}, nil
		}
		return map[string]any{"success": true, "styles": styles}, nil
	}
	b.handlers["illustration:revealStylesFile"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		l.readIllustrationStyles() // 文件不存在时先播种
		// 定位而不是打开：.json 多半没有默认关联程序。explorer 即使成功也常返回退出码 1，只看能否启动。
		if err := exec.Command("explorer.exe", "/select,"+l.illustrationStylesPath()).Start(); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true}, nil
	}
}

// registerFrozenStubs 必须在所有真实注册之后调用：只给「尚无处理器」的冻结频道补桩。
func (b *Bridge) registerFrozenStubs() {
	stub := func(ch string) handler {
		if frozenEmptyList[ch] {
			return func(context.Context, []json.RawMessage) (any, error) { return []any{}, nil }
		}
		return func(context.Context, []json.RawMessage) (any, error) { return nil, errors.New(errFeatureFrozen) }
	}
	for _, ch := range rendererInvokeChannels {
		if b.handlers[ch] == nil && isFrozenChannel(ch) {
			b.handlers[ch] = stub(ch)
		}
	}
	for _, ch := range rendererSendChannels {
		if b.sends[ch] == nil && isFrozenChannel(ch) {
			b.sends[ch] = func([]json.RawMessage) {}
		}
	}
}

// unimplementedChannels 返回渲染进程能调用、却没有处理器的频道（冻结频道补桩之后应为空）。
func (b *Bridge) unimplementedChannels() []string {
	var out []string
	for _, ch := range rendererInvokeChannels {
		if b.handlers[ch] == nil {
			out = append(out, ch)
		}
	}
	for _, ch := range rendererSendChannels {
		if b.sends[ch] == nil {
			out = append(out, "(send) "+ch)
		}
	}
	sort.Strings(out)
	return out
}
