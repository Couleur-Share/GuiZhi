package main

// goja 宿主公共函数：渲染引擎（themed_engine.go）与生成管线（themed_pipeline.go）共用。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	whatwg "github.com/nlnwa/whatwg-url/url"
)

func jsAtob(s string) (string, error) {
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
}

func jsBtoa(s string) (string, error) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return "", fmt.Errorf("btoa: 字符超出 Latin-1")
		}
		b = append(b, byte(r))
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func jsHash(algorithm, data, encoding string) (string, error) {
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
}

// jsCompileCheck 返回 ""（通过）或 "原因\n行号"。goja 与 V8 的错误措辞不同，行号口径一致（从 1 起）。
func jsCompileCheck(code, filename string) string {
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
}

// jsURLParse 对应 JS 的 new URL：失败返回空串，成功返回各组成部分的 JSON。
func jsURLParse(input, base string, hasBase bool) string {
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
}
