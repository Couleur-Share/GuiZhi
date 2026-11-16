// Package locales 是 Viva 原生界面的文案：catalog_gen.go 由 apps/desktop-go/scripts/gen-native-i18n.mjs
// 从渲染层的 i18n 目录生成，每条消息一个类型化函数，按当前语言返回中文或英文。
//
// 语言是进程级状态（设置里的「语言」）：界面线程在语言变化时调用 SetLang，之后每次取文案都是新语言。
// 本包不依赖任何界面工具包，也没有运行期的消息解析：插值在生成时展开成字符串拼接。
//
// Package locales holds the UI strings of the Viva UI; see catalog_gen.go.
package locales

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Lang 是界面语言。
type Lang int32

const (
	// Zh 是简体中文，源语言：每条消息都有，英文缺的键在生成时报错而不是运行时回落。
	Zh Lang = iota
	// En 是英文。
	En
)

var current atomic.Int32

// SetLang 切换界面语言。
func SetLang(l Lang) { current.Store(int32(l)) }

// CurrentLang 返回当前界面语言。
func CurrentLang() Lang { return Lang(current.Load()) }

// ParseLang 把设置里的语言码（"zh"、"zh-Hans"、"zh-CN"、"en"、"en-US"……）换成 Lang；认不出的取简体中文。
func ParseLang(tag string) Lang {
	if strings.HasPrefix(strings.ToLower(tag), "en") {
		return En
	}
	return Zh
}

// Tag 返回语言对应的设置码（"zh" 或 "en"），与渲染层 language 设置的取值一致。
func (l Lang) Tag() string {
	if l == En {
		return "en"
	}
	return "zh"
}

func useEn() bool { return CurrentLang() == En }

// fmtArg 把插值参数转成字符串。
func fmtArg(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
