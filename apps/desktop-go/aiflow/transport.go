package aiflow

// AI 请求的传输层接口（对应渲染层的 window.api.ai：request / requestStream）。
//
// 生产实现在主包（native_aiflow.go）里直接复用壳的 AI 代理 aiProxy——SSRF 守卫、网络代理设置、
// 默认超时与取消语义都与渲染层走 ai:httpRequest 时一致；这里只定义 aiflow 需要的窄接口，
// 测试用脚本化的假实现。取消用 context：ctx 被取消时实现应尽快返回，aiflow 再统一报 AbortError。

import (
	"context"
	"encoding/json"
)

// TransportRequest 对应 AITransportRequest。
type TransportRequest struct {
	Method string // GET | POST
	URL    string
	// Headers 的值都是字符串；代理对 null 值的头不发送，aiflow 不产生这种头。
	Headers map[string]string
	// Body 为空串表示没有请求体（GET）。
	Body string
	// TimeoutMs 为 0 表示未指定，代理兜底 30 秒。
	TimeoutMs float64
}

// TransportResponse 对应 AITransportResponse；传输层失败（超时、取消、被守卫拒绝）不抛错，
// 而是 OK=false、Status=0 并在 Error 里给出原因——与代理的返回一致。
type TransportResponse struct {
	OK         bool              `json:"ok"`
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Body       string            `json:"body"`
	Headers    map[string]string `json:"headers"`
	Error      *string           `json:"error,omitempty"`
}

// StreamHandler 接收流式响应：OnChunk 逐块给出已解码的文本，OnError 报告流读取阶段的失败。
type StreamHandler struct {
	OnChunk func(chunk string)
	OnError func(message string)
}

// Transport 是 aiflow 发请求的唯一出口。
type Transport interface {
	Request(ctx context.Context, req TransportRequest) (TransportResponse, error)
	RequestStream(ctx context.Context, req TransportRequest, h StreamHandler) (TransportResponse, error)
}

// cancellable 对应 withCancellation：请求前后各检查一次取消，已取消统一按 AbortError 抛出。
// 取消发生在飞行中时，代理返回的是 error 响应而不是抛错，所以要在返回后再看一眼 ctx。
func cancellable[T any](ctx context.Context, run func() (T, error)) (T, error) {
	var zero T
	if ctx.Err() != nil {
		return zero, ErrAborted
	}
	v, err := run()
	if err != nil {
		return zero, err
	}
	if ctx.Err() != nil {
		return zero, ErrAborted
	}
	return v, nil
}

// parseJSON 对应 JSON.parse：把文本解码成 any（对象为 map[string]any，数组为 []any，数字为 float64）。
func parseJSON(text string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return nil, err
	}
	return v, nil
}
