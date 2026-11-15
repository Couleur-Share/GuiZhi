package kit

import (
	"fmt"

	"guizhi-mygo-shell/native/locales"
)

// Logger 是诊断日志（logs/error.log）的写入口。截断与脱敏（用户目录替换成 ~ 等）由实现负责，
// 调用方可以放心把错误原文交给它；主包的适配器接到现有的 log:appError 同一条路径。
type Logger interface {
	AppError(scope, action, message string)
}

// logScope 是原生界面写进日志的来源字段（渲染层写的是 "renderer"）。
const logScope = "native"

// Guard 是变更类操作的失败通道，对应渲染层的 runGuardedMutation。
//
// 存在的理由：变更方法（批量打标签、移到回收站、入队…）常被「触发就不管」地调用，
// 一旦失败只是丢进日志，界面毫无反应，用户以为改成功了。Run 把失败统一变成
// 「<动作>失败」的错误提示（原文折叠在「查看详情」里）并记日志，调用方只看返回的布尔值。
type Guard struct {
	Notify *Notifier // nil 时只记日志
	Log    Logger    // nil 时只弹提示
}

// Run 执行一次变更操作：成功返回 true；失败记日志、弹出带原因的错误提示并返回 false，绝不向外抛——
// 包括 fn 里的 panic。action 是动作名（已本地化，如「批量更新」），进入提示标题与日志。
//
// 返回值给「成功后还要弹提示」的调用方用（比如移到回收站后的撤销提示）：
// 其余场合可以忽略，错误提示照样会出来。任意 goroutine 可调用；要在后台执行用 Go。
func (g Guard) Run(action string, fn func() error) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			g.fail(action, fmt.Errorf("panic: %v", r))
			ok = false
		}
	}()
	if err := fn(); err != nil {
		g.fail(action, err)
		return false
	}
	return true
}

// Go 在新的 goroutine 里 Run；done 非 nil 时带着结果回调（在那个 goroutine 里，改界面状态要自己同步）。
func (g Guard) Go(action string, fn func() error, done func(ok bool)) {
	go func() {
		ok := g.Run(action, fn)
		if done != nil {
			done(ok)
		}
	}()
}

// Background 记下一次后台失败（定时刷新、事件订阅里的查询等）：只进日志，不打扰用户——
// 用户没有做任何操作，弹错误只会吓到人；真要让用户知道的失败走 Run 或界面的错误态。
func (g Guard) Background(action string, err error) {
	if err == nil || g.Log == nil {
		return
	}
	g.Log.AppError(logScope, action, err.Error())
}

func (g Guard) fail(action string, err error) {
	msg := err.Error()
	if g.Log != nil {
		g.Log.AppError(logScope, action, msg)
	}
	if g.Notify != nil {
		g.Notify.Error(locales.CommonOperationFailed(action), msg)
	}
}
