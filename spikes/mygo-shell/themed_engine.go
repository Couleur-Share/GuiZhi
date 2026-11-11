package main

// 主题阅读渲染层：在 goja 里运行 themed-reading 的 TS 原文件打包产物（见 spikes/mygo-js/build-themed.mts）。
// 范围只含纯函数——正文分块（unified / remark / rehype）、净化（sanitize-html / css-tree / linkedom）、
// v3 页面规范化与校验、文档拼装与导出文档；文件读写、数据库和模型请求都在 Go 里。
// 选择内嵌 JS 而不手写移植的理由与快照净化引擎相同：这些模块是安全边界，且依赖链很深（remark-cjk-friendly、
// rehype-sanitize 的默认 schema、sanitize-html 的 mXSS 修补），手写移植无法保证逐字节一致，也会滞后上游。
// 内置库脚本（echarts / mermaid / animation，数 MB）不嵌入可执行文件：由 reading_libraries.json 按需读取。

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	whatwg "github.com/nlnwa/whatwg-url/url"
)

//go:embed themed_engine.js
var themedEngineJS string

const themedEngineTimeout = 120 * time.Second

type themedEngine struct {
	mu  sync.Mutex
	rt  *goja.Runtime
	gzt *goja.Object

	libOnce sync.Once
	libs    map[string]json.RawMessage
	libErr  error
}

var (
	themedEngineOnce sync.Once
	themedEngineInst *themedEngine
	themedEngineErr  error
)

func getThemedEngine() (*themedEngine, error) {
	themedEngineOnce.Do(func() { themedEngineInst, themedEngineErr = newThemedEngine() })
	return themedEngineInst, themedEngineErr
}

// readingLibrariesPath 按「环境变量 → 可执行文件旁 → 源码目录」查找内置库资源。
func readingLibrariesPath() string {
	if p := os.Getenv("GUIZHI_READING_LIBS"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "reading_libraries.json"); fileExists(p) {
			return p
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		return filepath.Join(filepath.Dir(file), "reading_libraries.json")
	}
	return "reading_libraries.json"
}

// library 取内置库脚本 / 组件样式（惰性读取；读不到时抛出与 Electron 版同样的中文原因）。
func (e *themedEngine) library(name string) (string, error) {
	e.libOnce.Do(func() {
		raw, err := os.ReadFile(readingLibrariesPath())
		if err != nil {
			e.libErr = fmt.Errorf("阅读组件资源读取失败：reading-libraries；%v", err)
			return
		}
		var all struct {
			Components json.RawMessage `json:"components"`
			Runtime    map[string]string
		}
		if err := json.Unmarshal(raw, &all); err != nil {
			e.libErr = fmt.Errorf("阅读组件资源读取失败：reading-libraries；%v", err)
			return
		}
		e.libs = map[string]json.RawMessage{"components": all.Components}
		for k, v := range all.Runtime {
			b, _ := json.Marshal(v)
			e.libs[k] = b
		}
	})
	if e.libErr != nil {
		return "", e.libErr
	}
	raw, ok := e.libs[name]
	if !ok {
		return "", fmt.Errorf("阅读组件资源读取失败：reading-libraries/%s", name)
	}
	if name == "components" {
		return string(raw), nil
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s, nil
}

func newThemedEngine() (*themedEngine, error) {
	rt := goja.New()
	e := &themedEngine{rt: rt}
	must := func(name string, v any) {
		if err := rt.Set(name, v); err != nil {
			panic(err)
		}
	}
	// 打包产物里 node 内置模块保留为 require 调用；渲染路径用不到它们，返回空对象即可。
	// linkedom 会尝试加载可选依赖 canvas（失败则退回无画布实现），所以对它要像 Node 那样抛 MODULE_NOT_FOUND。
	must("require", func(call goja.FunctionCall) goja.Value {
		if name := call.Argument(0).String(); name == "canvas" {
			panic(rt.NewGoError(fmt.Errorf("Cannot find module '%s'", name)))
		}
		return rt.NewObject()
	})
	must("atob", func(s string) (string, error) {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(s)
		}
		if err != nil {
			return "", fmt.Errorf("atob: 无效的 base64")
		}
		out := make([]rune, len(b))
		for i, c := range b {
			out[i] = rune(c)
		}
		return string(out), nil
	})
	must("btoa", func(s string) (string, error) {
		b := make([]byte, 0, len(s))
		for _, r := range s {
			if r > 0xff {
				return "", fmt.Errorf("btoa: 字符超出 Latin-1")
			}
			b = append(b, byte(r))
		}
		return base64.StdEncoding.EncodeToString(b), nil
	})
	must("__gzHash", func(algorithm, data, encoding string) (string, error) {
		if algorithm != "sha256" {
			return "", fmt.Errorf("不支持的摘要算法：%s", algorithm)
		}
		sum := sha256.Sum256([]byte(data))
		switch encoding {
		case "hex":
			return hex.EncodeToString(sum[:]), nil
		case "base64":
			return base64.StdEncoding.EncodeToString(sum[:]), nil
		}
		return "", fmt.Errorf("不支持的摘要编码：%s", encoding)
	})
	// 语法检查：返回 ""（通过）或 "原因\n行号"。goja 与 V8 的错误措辞不同，行号口径一致（从 1 起）。
	must("__gzCompile", func(code, filename string) string {
		if _, err := goja.Compile(filename, code, true); err != nil {
			// goja 的错误形如 "SyntaxError: 原因 at 文件名:行:列"。
			msg, line := strings.TrimPrefix(err.Error(), "SyntaxError: "), 1
			if m := gojaSyntaxPos.FindStringSubmatch(msg); m != nil {
				msg = msg[:len(msg)-len(m[0])]
				line, _ = strconv.Atoi(m[1])
			}
			return msg + "\n" + strconv.Itoa(line)
		}
		return ""
	})
	// WHATWG URL 解析（JS 的 new URL）：失败返回空串。\n	must("__gzURLParse", func(input, base string, hasBase bool) string {\n		var u *whatwg.Url\n		var err error\n		if hasBase {\n			u, err = whatwg.ParseRef(base, input)\n		} else {\n			u, err = whatwg.Parse(input)\n		}\n		if err != nil {\n			return ""\n		}\n		out := newObj()\n		out.set("protocol", u.Protocol())\n		out.set("username", u.Username())\n		out.set("password", u.Password())\n		out.set("hostname", u.Hostname())\n		out.set("port", u.Port())\n		out.set("pathname", u.Pathname())\n		out.set("search", u.Search())\n		out.set("_hash", u.Hash())\n		s, _ := jsStringify(out)\n		return s\n	})\n	// WHATWG URL 解析（JS 的 new URL）：失败返回空串。
	must("__gzURLParse", func(input, base string, hasBase bool) string {
		var u *whatwg.Url
		var err error
		if hasBase {
			u, err = whatwg.ParseRef(base, input)
		} else {
			u, err = whatwg.Parse(input)
		}
		if err != nil {
			return ""
		}
		out := newObj()
		out.set("protocol", u.Protocol())
		out.set("username", u.Username())
		out.set("password", u.Password())
		out.set("hostname", u.Hostname())
		out.set("port", u.Port())
		out.set("pathname", u.Pathname())
		out.set("search", u.Search())
		out.set("_hash", u.Hash())
		s, _ := jsStringify(out)
		return s
	})
	must("__gzLibrary", func(name string) (string, error) { return e.library(name) })
	if _, err := rt.RunString(themedEngineJS); err != nil {
		return nil, fmt.Errorf("加载主题阅读渲染引擎失败：%w", err)
	}
	gzt, ok := rt.Get("GZT").(*goja.Object)
	if !ok {
		return nil, errors.New("主题阅读渲染引擎缺少入口对象")
	}
	e.gzt = gzt
	return e, nil
}

// call 调用 GZT 上的函数；JS 抛出的 Error 转成 Go error（保留 message）。
func (e *themedEngine) call(name string, args ...any) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn, ok := goja.AssertFunction(e.gzt.Get(name))
	if !ok {
		return "", fmt.Errorf("主题阅读渲染引擎缺少函数：%s", name)
	}
	vals := make([]goja.Value, len(args))
	for i, a := range args {
		vals[i] = e.rt.ToValue(a)
	}
	timer := time.AfterFunc(themedEngineTimeout, func() { e.rt.Interrupt("主题阅读渲染超时") })
	defer func() {
		timer.Stop()
		e.rt.ClearInterrupt()
	}()
	res, err := fn(e.gzt, vals...)
	if err != nil {
		var ex *goja.Exception
		if errors.As(err, &ex) {
			if obj, ok := ex.Value().(*goja.Object); ok {
				if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) {
					return "", errors.New(msg.String())
				}
			}
			return "", errors.New(ex.Value().String())
		}
		var it *goja.InterruptedError
		if errors.As(err, &it) {
			return "", errors.New("主题阅读渲染超时")
		}
		return "", err
	}
	if res == nil || goja.IsUndefined(res) || goja.IsNull(res) {
		return "", nil
	}
	return res.String(), nil
}

// themedCall 是 call 的便捷封装：自动取引擎。
func themedCall(name string, args ...any) (string, error) {
	e, err := getThemedEngine()
	if err != nil {
		return "", err
	}
	return e.call(name, args...)
}

// gojaSyntaxPos 匹配 goja 语法错误末尾的位置后缀。
var gojaSyntaxPos = regexp.MustCompile(`\s+at\s+\S*?:(\d+):\d+$`)
