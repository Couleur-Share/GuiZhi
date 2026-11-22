package ask

import (
	"context"
	"fmt"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 服务返回 context.Canceled、但本轮的 ctx 并没有被取消（用户没点停止：比如下游代理自己超时被取消）：
// 照样按「已停止」落盘（与渲染层 isAbortError 同口径），不当成一次需要上报的失败：不弹提示、不写后台日志、错误原因是「已停止」。
func TestAskCanceledErrorWithoutStopIsMarkedStoppedNotFailed(t *testing.T) {
	for name, err := range map[string]error{
		"直接返回":  context.Canceled,
		"被包了一层": fmt.Errorf("proxy: %w", context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.ready()
			r.svc.QueueAsk(servicestest.AskScript{Err: err})
			r.store.Ask("q")

			v := r.view()
			got := lastMessage(t, v)
			if got.Status != services.AskStatusError || !got.Stopped || got.Error != locales.AskStopped() || got.ErrorKind != services.AskErrorGeneric {
				t.Fatalf("取消错误应标成已停止：%+v", got)
			}
			if v.IsRunning {
				t.Fatalf("这一轮结束后不该还在回答中：%+v", v)
			}
			if entries := r.log.entries(); len(entries) != 0 {
				t.Fatalf("取消不是失败，不该写后台日志：%v", entries)
			}
			if queued := r.toasts(); len(queued) != 0 {
				t.Fatalf("取消不是失败，不该弹提示：%+v", queued)
			}
		})
	}
}
