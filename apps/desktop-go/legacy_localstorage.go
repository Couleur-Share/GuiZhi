package main

// 旧 Electron 版 localStorage 的一次性导入。
//
// 背景：渲染进程把设置（zustand 持久化的 guizhi-settings）、界面偏好、首次引导标记等存在 localStorage。
// Electron 版放在 <userData>\Local Storage\leveldb（Chromium 的 leveldb，origin 为 file://），
// Go 壳的 WebView2 另有自己的存储与 origin，覆盖安装后这些都会回到默认。
// 做法：启动时（尚未导入）只读地复制并读出旧库，把键值经预加载脚本在页面脚本运行前写入新的 localStorage；
// 页面确认后由主进程落一个标记文件，之后不再读取。旧数据目录不会被改动或删除（可随时回退到 Electron 版）。
//
// One-time import of the old Electron localStorage: read the Chromium leveldb from a copy, hand the entries to the
// renderer through the preload script before app code runs, and record a marker once the page confirms.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

const (
	// legacyLSMarkerKey 写在新 localStorage 里，和「写入新键」同属一次同步脚本，是页面一侧的幂等标记。
	legacyLSMarkerKey = "guizhi-legacy-ls-imported"
	// legacyLSConfirmChannel 是页面写入完成后回报主进程的频道。
	legacyLSConfirmChannel = "legacyLocalStorage:imported"
	// 旧版设置键：用它判断哪个 origin 是归知的数据。
	legacyLSSettingsKey = "guizhi-settings"
	legacyLSProdOrigin  = "file://" // 正式版 Electron 用 loadFile 加载渲染进程
)

type legacyLocalStorage struct {
	Origin  string
	Entries map[string]string
}

// readLegacyLocalStorage 读取 <userData>\Local Storage\leveldb。没有旧数据返回 (nil, nil)；
// 读不了（损坏等）返回错误，调用方应记录后继续启动。
func readLegacyLocalStorage(userData string) (*legacyLocalStorage, error) {
	src := filepath.Join(userData, "Local Storage", "leveldb")
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return nil, nil
	}
	// 旧版 Electron 可能还在运行并持有 LOCK；复制到临时目录再开（不复制 LOCK），既不干扰旧版也不改动旧数据。
	tmp, err := os.MkdirTemp("", "guizhi-legacy-ls-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyDirFlat(src, tmp, "LOCK"); err != nil {
		return nil, fmt.Errorf("复制旧 Local Storage 失败：%w", err)
	}
	db, err := leveldb.OpenFile(tmp, &opt.Options{ReadOnly: true, ErrorIfMissing: true})
	if err != nil {
		return nil, fmt.Errorf("打开旧 Local Storage 失败：%w", err)
	}
	defer db.Close()

	byOrigin := map[string]map[string]string{}
	it := db.NewIterator(util.BytesPrefix([]byte("_")), nil)
	for it.Next() {
		origin, name, ok := parseLegacyLSKey(it.Key())
		if !ok {
			continue
		}
		val, ok := decodeLegacyLSString(it.Value())
		if !ok {
			continue
		}
		if byOrigin[origin] == nil {
			byOrigin[origin] = map[string]string{}
		}
		byOrigin[origin][name] = val
	}
	it.Release()
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("遍历旧 Local Storage 失败：%w", err)
	}
	return pickLegacyOrigin(byOrigin), nil
}

// pickLegacyOrigin：优先 file://（正式版）；它没有归知设置时，改取任何有归知设置的 origin（例如开发服务器）；
// file:// 有数据但哪里都没有设置时仍取 file://；其余无关来源一律不导入。
func pickLegacyOrigin(byOrigin map[string]map[string]string) *legacyLocalStorage {
	if m, ok := byOrigin[legacyLSProdOrigin]; ok {
		if _, has := m[legacyLSSettingsKey]; has {
			return &legacyLocalStorage{Origin: legacyLSProdOrigin, Entries: m}
		}
	}
	origins := make([]string, 0, len(byOrigin))
	for o := range byOrigin {
		origins = append(origins, o)
	}
	sort.Strings(origins)
	for _, o := range origins {
		if _, has := byOrigin[o][legacyLSSettingsKey]; has {
			return &legacyLocalStorage{Origin: o, Entries: byOrigin[o]}
		}
	}
	if m, ok := byOrigin[legacyLSProdOrigin]; ok && len(m) > 0 {
		return &legacyLocalStorage{Origin: legacyLSProdOrigin, Entries: m}
	}
	return nil
}

// parseLegacyLSKey 拆 Chromium 的 localStorage 键：'_' + origin + 0x00 + <编码标记 + 键名>。
func parseLegacyLSKey(k []byte) (origin, name string, ok bool) {
	if len(k) < 3 || k[0] != '_' {
		return "", "", false
	}
	i := strings.IndexByte(string(k[1:]), 0)
	if i < 0 {
		return "", "", false
	}
	origin = string(k[1 : 1+i])
	name, ok = decodeLegacyLSString(k[2+i:])
	return origin, name, ok
}

// decodeLegacyLSString 解 Chromium 的字符串：首字节 0 = UTF-16LE，1 = Latin1（每字节一个码点）。
// 孤立代理项按 U+FFFD 处理（Go 字符串无法表示；实际设置里几乎不会出现）。
func decodeLegacyLSString(b []byte) (string, bool) {
	if len(b) == 0 {
		return "", false
	}
	switch b[0] {
	case 1:
		rs := make([]rune, len(b)-1)
		for i, c := range b[1:] {
			rs[i] = rune(c)
		}
		return string(rs), true
	case 0:
		body := b[1:]
		if len(body)%2 != 0 {
			return "", false
		}
		u := make([]uint16, len(body)/2)
		for i := range u {
			u[i] = uint16(body[2*i]) | uint16(body[2*i+1])<<8
		}
		return string(utf16.Decode(u)), true
	}
	return "", false
}

// copyDirFlat 复制 src 下的普通文件（不递归），跳过 skip 里列出的名字。
func copyDirFlat(src, dst string, skip ...string) error {
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
next:
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		for _, s := range skip {
			if e.Name() == s {
				continue next
			}
		}
		in, err := os.Open(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(dst, e.Name()))
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// legacyLocalStorageScript 生成放在 preload 之前的注入脚本。它在每个文档创建时运行（含子框架），
// 所以第一件事是确认自己在渲染进程主页面的 origin 上。只写入「当前不存在」的键，并先写 localStorage 内的
// 标记键，保证只导入一次；回报主进程的调用在 DOM 就绪后进行（preload 阶段 mygo 桥可能还没就绪）。
func legacyLocalStorageScript(ls *legacyLocalStorage, pageOrigin string) string {
	payload, _ := json.Marshal(ls.Entries)
	literal, _ := json.Marshal(string(payload)) // 作为 JS 字符串字面量嵌入，再 JSON.parse
	origin, _ := json.Marshal(pageOrigin)
	marker, _ := json.Marshal(legacyLSMarkerKey)
	channel, _ := json.Marshal(legacyLSConfirmChannel)
	return `;(function () {
  try {
    if (location.origin !== ` + string(origin) + `) return;
    var data = JSON.parse(` + string(literal) + `);
    var mark = ` + string(marker) + `;
    if (localStorage.getItem(mark) === null) {
      for (var k in data) {
        if (Object.prototype.hasOwnProperty.call(data, k) && localStorage.getItem(k) === null) localStorage.setItem(k, data[k]);
      }
      localStorage.setItem(mark, String(Date.now()));
    }
    var confirm = function () {
      try { window.mygo.call("Bridge.Invoke", ` + string(channel) + `, "[]").catch(function () {}); } catch (e) {}
    };
    if (document.readyState === "loading") window.addEventListener("DOMContentLoaded", confirm); else confirm();
  } catch (e) { console.error("[legacy-localstorage] 导入失败", e); }
})();
`
}

func legacyImportMarkerPath(userData string) string {
	return filepath.Join(userData, ".machine", "legacy-localstorage-imported")
}

// legacyImportPending：没有标记文件就还需要导入。标记在机器本地的 .machine 下，不随数据目录迁移或备份。
func legacyImportPending(userData string) bool {
	_, err := os.Stat(legacyImportMarkerPath(userData))
	return os.IsNotExist(err)
}

func markLegacyImported(userData string) error {
	p := legacyImportMarkerPath(userData)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("imported\n"), 0o644)
}

// legacyLocalStoragePreload 返回要拼在 preload 前面的脚本；无需导入或读取失败时返回空串（失败只记录，不阻止启动）。
func legacyLocalStoragePreload(userData string, logf func(format string, args ...any)) string {
	if !legacyImportPending(userData) {
		return ""
	}
	ls, err := readLegacyLocalStorage(userData)
	if err != nil {
		logf("[legacy-ls] 读取旧 Local Storage 失败，跳过导入：%v", err)
		return ""
	}
	if ls == nil {
		// 没有旧数据（全新安装）：直接落标记，避免每次启动都探测一遍。
		if err := markLegacyImported(userData); err != nil {
			logf("[legacy-ls] 写入标记失败：%v", err)
		}
		return ""
	}
	logf("[legacy-ls] 从旧 Electron 版导入 %d 个 localStorage 键（origin=%s）", len(ls.Entries), ls.Origin)
	return legacyLocalStorageScript(ls, rendererOrigin)
}

// registerLegacyLocalStorage 登记页面确认频道：写入标记文件，之后不再读取旧库。
func (b *Bridge) registerLegacyLocalStorage(userData string, logf func(format string, args ...any)) {
	b.handlers[legacyLSConfirmChannel] = func(_ context.Context, _ []json.RawMessage) (any, error) {
		if legacyImportPending(userData) {
			if err := markLegacyImported(userData); err != nil {
				logf("[legacy-ls] 写入标记失败：%v", err)
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
		}
		return map[string]any{"ok": true}, nil
	}
}
