package main

// 原文快照净化引擎：在 goja（纯 Go 的 JS 解释器）里运行 snapshot-sanitize.ts / snapshot-document.ts 的打包产物
// （sanitize-html + css-tree，见 spikes/mygo-js）。选择内嵌 JS 而不手写移植的原因：
//   - 净化器是安全边界，sanitize-html 的 mXSS 修补与 css-tree 的 Raw 回退等行为很细，手写移植会滞后上游且无法保证逐字节一致；
//   - 打包入口直接引用 TS 原文件，升级依赖后重新打包即可，差分基线同时保证与 Electron 版一致。
// goja Runtime 不是并发安全的：用互斥锁串行化，调用有超时（interrupt）。

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
)

//go:embed websnap_sanitize.js
var websnapSanitizeJS string

// websnapTimeout 单次净化的上限；超大或病态输入超时后中断，返回错误而不是卡住。
const websnapTimeout = 60 * time.Second

type websnapEngine struct {
	mu sync.Mutex
	rt *goja.Runtime
	gz *goja.Object
}

var (
	websnapOnce sync.Once
	websnapInst *websnapEngine
	websnapErr  error
)

// getWebsnapEngine 懒加载全局引擎（首次约数十毫秒，之后复用）。
func getWebsnapEngine() (*websnapEngine, error) {
	websnapOnce.Do(func() { websnapInst, websnapErr = newWebsnapEngine() })
	return websnapInst, websnapErr
}

func newWebsnapEngine() (*websnapEngine, error) {
	rt := goja.New()
	// 打包产物里 node 内置模块（fs/path/url…）保留为 require 调用；净化路径不会真正使用它们，返回空对象即可。
	if err := rt.Set("require", func(goja.FunctionCall) goja.Value { return rt.NewObject() }); err != nil {
		return nil, err
	}
	// htmlparser2 的实体表以 base64 内嵌，经 atob 解码；btoa 同理补齐（Latin-1 二进制字符串语义）。
	if err := rt.Set("atob", func(s string) (string, error) {
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
	}); err != nil {
		return nil, err
	}
	if err := rt.Set("btoa", func(s string) (string, error) {
		b := make([]byte, 0, len(s))
		for _, r := range s {
			if r > 0xff {
				return "", fmt.Errorf("btoa: 字符超出 Latin-1")
			}
			b = append(b, byte(r))
		}
		return base64.StdEncoding.EncodeToString(b), nil
	}); err != nil {
		return nil, err
	}
	if err := rt.Set("__gzHash", func(algorithm, data, encoding string) (string, error) {
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
	}); err != nil {
		return nil, err
	}
	if _, err := rt.RunString(websnapSanitizeJS); err != nil {
		return nil, fmt.Errorf("加载快照净化引擎失败：%w", err)
	}
	gz, ok := rt.Get("GZ").(*goja.Object)
	if !ok {
		return nil, errors.New("快照净化引擎缺少入口对象")
	}
	return &websnapEngine{rt: rt, gz: gz}, nil
}

// call 调用 GZ 上的函数并转成字符串；JS 抛出的 Error 转成 Go error（保留 message）。
func (e *websnapEngine) call(name string, args ...any) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn, ok := goja.AssertFunction(e.gz.Get(name))
	if !ok {
		return "", fmt.Errorf("快照净化引擎缺少函数：%s", name)
	}
	vals := make([]goja.Value, len(args))
	for i, a := range args {
		vals[i] = e.rt.ToValue(a)
	}
	timer := time.AfterFunc(websnapTimeout, func() { e.rt.Interrupt("快照净化超时") })
	defer func() {
		timer.Stop()
		e.rt.ClearInterrupt()
	}()
	res, err := fn(e.gz, vals...)
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
			return "", errors.New("快照净化超时")
		}
		return "", err
	}
	if res == nil || goja.IsUndefined(res) || goja.IsNull(res) {
		return "", nil
	}
	return res.String(), nil
}

// snapshotCleanHTML 对应 cleanHtml(html, map)：table 是 原始 URL → 本快照资源 URL 的映射。
func snapshotCleanHTML(html string, table map[string]string) (string, error) {
	e, err := getWebsnapEngine()
	if err != nil {
		return "", err
	}
	if table == nil {
		table = map[string]string{}
	}
	return e.call("cleanHtml", html, table)
}

// snapshotCleanCSS 对应 cleanCss(value, map, stylesheet)。
func snapshotCleanCSS(css string, table map[string]string, stylesheet bool) (string, error) {
	e, err := getWebsnapEngine()
	if err != nil {
		return "", err
	}
	if table == nil {
		table = map[string]string{}
	}
	return e.call("cleanCss", css, table, stylesheet)
}

// snapshotSanitize 对应 sanitizeSnapshot：入参与返回都是快照 JSON；格式无效时返回带原因的错误。
func snapshotSanitize(snapshotJSON string) (string, error) {
	e, err := getWebsnapEngine()
	if err != nil {
		return "", err
	}
	return e.call("sanitizeSnapshot", snapshotJSON)
}

// snapshotHashOf 对应 snapshotHash。
func snapshotHashOf(snapshotJSON string) (string, error) {
	e, err := getWebsnapEngine()
	if err != nil {
		return "", err
	}
	return e.call("snapshotHash", snapshotJSON)
}

// snapshotDocumentOf 对应 snapshotDocument(snapshot, instanceId?)；instanceId 为空串表示不带桥接脚本。
func snapshotDocumentOf(snapshotJSON, instanceID string) (string, error) {
	e, err := getWebsnapEngine()
	if err != nil {
		return "", err
	}
	if instanceID == "" {
		return e.call("snapshotDocument", snapshotJSON)
	}
	return e.call("snapshotDocument", snapshotJSON, instanceID)
}

// snapshotEscapeText 对应 escapeSnapshotText。
func snapshotEscapeText(s string) (string, error) {
	e, err := getWebsnapEngine()
	if err != nil {
		return "", err
	}
	return e.call("escapeSnapshotText", s)
}
