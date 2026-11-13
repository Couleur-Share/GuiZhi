package main

// 对应 ipc/media.ipc.ts 的本地转写引擎频道（托管 funasr-server）：
// funasr:status / install / checkUpdate / update / uninstall，以及主进程 → 渲染进程的 funasr:installProgress 推送。

import (
	"context"
	"encoding/json"
)

const chFunasrProgress = "funasr:installProgress"

func funasrOpResult(version, warning string, err error) map[string]any {
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	res := map[string]any{"success": true}
	if version != "" {
		res["version"] = version
	}
	if warning != "" {
		res["warning"] = warning
	}
	return res
}

func (l *Library) registerFunasr(b *Bridge) {
	e := l.funasr()
	cache := newStatusCache[funasrStatus]()
	push := func(p funasrInstallProgress) { l.pushMain(chFunasrProgress, p) }
	// 安装 / 更新是几十分钟的长任务，不随单次 IPC 请求取消（与 TS 一致）
	detached := func(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

	b.handlers["funasr:status"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		// 无配置输入，缓存键固定；running 会随转写按需启动而变，靠 TTL 与手动重新检测收敛
		return cache.read("funasr", func() (funasrStatus, error) { return e.status(), nil }, jsArg(a, 0) == true)
	}
	b.handlers["funasr:checkUpdate"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return e.checkUpdate(ctx)
	}
	b.handlers["funasr:update"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		defer cache.invalidate()
		version, ok := jsArg(a, 0).(string)
		if !ok {
			return funasrOpResult("", "", errInvalidFunasrVersion), nil
		}
		v, w, err := e.update(detached(ctx), version, push)
		return funasrOpResult(v, w, err), nil
	}
	b.handlers["funasr:install"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		defer cache.invalidate()
		v, err := e.install(detached(ctx), push)
		return funasrOpResult(v, "", err), nil
	}
	b.handlers["funasr:uninstall"] = func(context.Context, []json.RawMessage) (any, error) {
		defer cache.invalidate()
		if err := e.uninstall(); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true}, nil
	}
}
