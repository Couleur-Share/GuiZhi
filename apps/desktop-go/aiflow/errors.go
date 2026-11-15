package aiflow

// 错误类型与 AI 响应错误的可读化（对应 services/ai-errors.ts）。
//
// provider 与中转站的错误体形态各异：结构化 JSON、纯文本、网关返回的 HTML 错误页都可能出现，
// 这里统一收敛成一句人能看懂的话。文案逐字沿用 TS，界面与日志里的用户看到的不会变。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// AbortError 对应 DOMException("已取消", "AbortError")：上层据此识别为「已停止」，不当作失败。
type AbortError struct{}

func (AbortError) Error() string { return "已取消" }

// Is 让 errors.Is(err, context.Canceled) 对 AbortError 成立。
func (AbortError) Is(target error) bool { return target == context.Canceled }

// ErrAborted 是 AbortError 的单例。
var ErrAborted error = AbortError{}

// IsAborted 判断错误是否表示用户取消。
func IsAborted(err error) bool {
	if err == nil {
		return false
	}
	var ae AbortError
	return errors.As(err, &ae) || errors.Is(err, context.Canceled)
}

// NotConfiguredError 对应 AiNotConfiguredError：AI 未配置（区别于调用失败，界面引导去设置页）。
type NotConfiguredError struct{}

func (*NotConfiguredError) Error() string { return "AI_NOT_CONFIGURED" }

// ErrNotConfigured 是 NotConfiguredError 的单例。
var ErrNotConfigured error = &NotConfiguredError{}

// IsNotConfigured 判断错误是否是「AI 未配置」。
func IsNotConfigured(err error) bool {
	var nc *NotConfiguredError
	return errors.As(err, &nc)
}

// responseHeader 对应 getResponseHeader：按名称大小写不敏感取值。
func responseHeader(headers map[string]string, name string) string {
	lower := strings.ToLower(name)
	for k, v := range headers {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return ""
}

func isHTMLErrorPayload(text string, headers map[string]string) bool {
	contentType := strings.ToLower(responseHeader(headers, "content-type"))
	trimmed := jsLower(jsTrimStart(text))
	return strings.Contains(contentType, "text/html") ||
		strings.HasPrefix(trimmed, "<!doctype html") ||
		strings.HasPrefix(trimmed, "<html")
}

var reHTMLTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func extractHTMLTitle(text string) string {
	m := reHTMLTitle.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return jsTrim(jsCollapseSpaces(m[1], " "))
}

// ParseStructuredErrorMessage 对应 parseStructuredErrorMessage；解析不出返回 ok=false。
func ParseStructuredErrorMessage(text string) (string, bool) {
	root, err := parseJSON(text)
	if err != nil {
		return "", false
	}
	errorJSON, ok := root.(map[string]any)
	if !ok {
		return "", false // null 在 TS 里读属性抛错被吞，其余原始类型没有这些字段
	}
	errField := errorJSON["error"]
	errObj, _ := errField.(map[string]any)
	pick := func(m map[string]any, key string) any {
		if m == nil {
			return nil
		}
		return m[key]
	}
	var message any
	for _, candidate := range []any{
		pick(errObj, "message"), pick(errObj, "status"), pick(errObj, "type"),
		errorJSON["message"], errorJSON["detail"],
	} {
		if jsTruthy(candidate) {
			message = candidate
			break
		}
	}
	if message == nil {
		if s, isString := errField.(string); isString && s != "" {
			message = s
		}
	}
	if !jsTruthy(message) {
		return "", false
	}
	text = jsToString(message)
	if code := pick(errObj, "code"); jsTruthy(code) {
		return fmt.Sprintf("%s (code: %s)", text, jsToString(code)), true
	}
	if typ := pick(errObj, "type"); jsTruthy(typ) && !jsStrictEqual(typ, message) {
		return fmt.Sprintf("[%s] %s", jsToString(typ), text), true
	}
	return text, true
}

// jsStrictEqual 对应 ===（只覆盖 JSON 解码后的标量类型；对象按不相等处理）。
func jsStrictEqual(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case nil:
		return b == nil
	}
	return false
}

// FormatErrorOptions 对应 getFormattedErrorMessageFromResponse 的 options。
type FormatErrorOptions struct {
	Operation     string
	Fallback      string
	MaxTextLength int
}

// FormatHTTPError 对应 getFormattedErrorMessageFromResponse：把一个失败响应收敛成一句可读文案。
func FormatHTTPError(resp TransportResponse, opts FormatErrorOptions) string {
	errorText := resp.Body
	if resp.Error != nil {
		errorText = *resp.Error // `response.error ?? text()`：空串也算有值
	}
	operation := opts.Operation
	if operation == "" {
		operation = "API request"
	}
	fallback := opts.Fallback
	if fallback == "" {
		fallback = fmt.Sprintf("API 请求失败 (%d)", resp.Status)
	}
	if resp.Status == 504 {
		return fmt.Sprintf("%s gateway timed out (%d). The provider or proxy did not finish before its own timeout.", operation, resp.Status)
	}
	if structured, ok := ParseStructuredErrorMessage(errorText); ok {
		return structured
	}
	if errorText != "" && isHTMLErrorPayload(errorText, resp.Headers) {
		if title := extractHTMLTitle(errorText); title != "" {
			return fallback + ": " + title
		}
		return fallback
	}
	if errorText != "" {
		limit := opts.MaxTextLength
		if limit == 0 {
			limit = 200
		}
		return u16Head(errorText, limit)
	}
	return fallback
}
