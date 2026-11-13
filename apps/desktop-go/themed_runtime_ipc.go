package main

// 主题阅读运行时的 IPC 频道：generate / cancel / resume / continueOffline / regenerateAsset。
// 对应 ipc/themed-reading.ipc.ts 中的同名处理器与 parseThemedOptions。

import (
	"context"
	"encoding/json"
	"errors"
	"math"
)

// parseThemedOptions 对应 parseThemedOptions：校验渲染进程传来的排版选项并补默认值（键序与 TS 一致）。
func parseThemedOptions(v any) (*jsObj, error) {
	undef := func(x any) bool { return x == jsUndefined }
	style, styleOK := jsGet(v, "style").(string)
	generate, genOK := jsGet(v, "generateImages").(bool)
	maxImages, maxOK := jsGet(v, "maxImages").(float64)
	if !vTruthy(v) || !styleOK || utf16Len(style) > 2000 || !genOK || !maxOK || maxImages != math.Trunc(maxImages) || maxImages < 0 || maxImages > 5 {
		return nil, errors.New("主题排版选项无效（图片上限为 0–5 张）")
	}
	research := jsGet(v, "research")
	action := jsGet(v, "action")
	_, researchBool := research.(bool)
	a, _ := action.(string)
	if (!undef(research) && !researchBool) || (!undef(action) && a != "create" && a != "revise" && a != "redesign" && a != "refresh") {
		return nil, errors.New("重构选项无效")
	}
	enhanced := jsGet(v, "enhancedInteraction")
	depth := jsGet(v, "researchDepth")
	_, enhancedBool := enhanced.(bool)
	d, _ := depth.(string)
	if (!undef(enhanced) && !enhancedBool) || (!undef(depth) && d != "standard" && d != "deep") {
		return nil, errors.New("增强交互或查证深度无效")
	}
	out := newObj()
	if undef(enhanced) {
		out.set("enhancedInteraction", true)
	} else {
		out.set("enhancedInteraction", enhanced)
	}
	if undef(depth) {
		out.set("researchDepth", "standard")
	} else {
		out.set("researchDepth", depth)
	}
	if undef(research) {
		out.set("research", false)
	} else {
		out.set("research", research)
	}
	if undef(action) {
		out.set("action", "create")
	} else {
		out.set("action", action)
	}
	out.set("style", jsTrim(style))
	out.set("generateImages", generate)
	out.set("maxImages", maxImages)
	out.set("fromCurrent", jsGet(v, "fromCurrent") == true)
	return out, nil
}

func (l *Library) registerThemedRuntime(b *Bridge) {
	rt := l.newThemedRuntime()
	// 接入其他功能族已预留的挂钩：恢复备份 / 切换数据目录前的活动检查，条目进回收站 / 彻底删除时取消任务。
	l.hasActiveThemedReading = func() bool { return rt.hasActive(context.Background()) }
	l.retireIdleThemedReading = func() { _ = rt.retireIdle(context.Background()) }
	l.cancelThemedReading = rt.cancelForItems
	wrap := func(action string, fn func(ctx context.Context, a []json.RawMessage) (any, error)) func(context.Context, []json.RawMessage) (any, error) {
		return func(ctx context.Context, a []json.RawMessage) (any, error) {
			out, err := fn(ctx, a)
			if err != nil {
				return themedFail(l, action, err), nil
			}
			return out, nil
		}
	}
	taskResult := func(task *jsObj, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "task": task}, nil
	}
	b.handlers["themedReading:generate"] = wrap("generate", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		req, err := parseThemedRequest(raw)
		if err != nil {
			return nil, err
		}
		opts, err := parseThemedOptions(jsGet(raw, "options"))
		if err != nil {
			return nil, err
		}
		return taskResult(rt.generate(ctx, themedGenerateInput{req.itemID, req.kind, opts}, "", false))
	})
	b.handlers["themedReading:cancel"] = wrap("cancel", func(ctx context.Context, a []json.RawMessage) (any, error) {
		return taskResult(rt.cancel(ctx, jsToString(jsArg(a, 0))))
	})
	b.handlers["themedReading:resume"] = wrap("resume", func(ctx context.Context, a []json.RawMessage) (any, error) {
		return taskResult(rt.resume(ctx, jsToString(jsArg(a, 0))))
	})
	b.handlers["themedReading:continueOffline"] = wrap("continueOffline", func(ctx context.Context, a []json.RawMessage) (any, error) {
		return taskResult(rt.continueOffline(ctx, jsToString(jsArg(a, 0))))
	})
	b.handlers["themedReading:regenerateAsset"] = wrap("asset", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		req, err := parseThemedRequest(raw)
		if err != nil {
			return nil, err
		}
		page, err := rt.pages.get(ctx, req.itemID, req.kind, "")
		if err != nil {
			return nil, err
		}
		assetID, ok := jsGet(raw, "assetId").(string)
		if page == nil || !ok {
			return nil, errors.New("主题页或图片不存在")
		}
		opts := newObj()
		spreadInto(opts, page.get("options"))
		opts.set("fromCurrent", true)
		opts.set("generateImages", false)
		return taskResult(rt.generate(ctx, themedGenerateInput{req.itemID, req.kind, opts}, assetID, false))
	})
}
