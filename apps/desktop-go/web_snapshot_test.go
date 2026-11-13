package main

// web:snapshot / web:versions / web:snapshot-export / web:snapshot-enqueue / web:solverCheck 的手写测试：
// 真实 SQLite（基准库副本）+ 临时图片目录 + httptest 回环服务（不访问真实网络，不起真实 SSH）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type webSnapEnv struct {
	t      *testing.T
	l      *Library
	b      *Bridge
	itemID string
}

func newWebSnapEnv(t *testing.T) *webSnapEnv {
	t.Helper()
	_, l := loadGolden(t)
	l.paths = Paths{UserData: t.TempDir()}
	l.configDir = t.TempDir()
	if err := os.MkdirAll(l.paths.ImagesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	e := &webSnapEnv{t: t, l: l, b: &Bridge{handlers: map[string]handler{}}}
	l.setPush(func(string, any) {})
	l.extractOverride = func(ctx context.Context, _ map[string]any, _ func(string)) (*extractedContent, error) {
		return nil, fmt.Errorf("测试不执行抽取")
	}
	l.registerImport(e.b)
	l.registerWebSnapshot(e.b)
	v, err := l.knowledgeCreate(context.Background(), CreateInput{Title: optStr("公众号文章"), Content: optStr("正文内容"), ItemType: optStr("note")})
	if err != nil {
		t.Fatal(err)
	}
	e.itemID = toGeneric(t, v).(map[string]any)["id"].(string)
	return e
}

func (e *webSnapEnv) call(ch string, args ...any) map[string]any {
	e.t.Helper()
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		raw[i], _ = json.Marshal(a)
	}
	out, err := e.b.handlers[ch](context.Background(), raw)
	if err != nil {
		e.t.Fatalf("%s: %v", ch, err)
	}
	return toGeneric(e.t, out).(map[string]any)
}

func (e *webSnapEnv) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.l.db.Query(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

// image 写入一张快照图片并返回 (文件名, sha256)；文件名需满足 wechat-<64 hex>.png。
func (e *webSnapEnv) image(content string) (string, string) {
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	name := "wechat-" + hash + ".png"
	if err := os.WriteFile(filepath.Join(e.l.paths.ImagesDir(), name), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return name, hash
}

func snapshotFor(name, hash, html, css string) map[string]any {
	return map[string]any{
		"formatVersion": 1, "policyVersion": 1, "adapterVersion": "wechat/1",
		"html": html, "css": css, "hash": "h", "account": "账号<b>", "author": "作者", "publishedAt": 1700000000000,
		"assets":   []any{map[string]any{"fileName": name, "sourceUrl": "https://mmbiz.qpic.cn/x", "sha256": hash, "bytes": 3}},
		"failures": []any{}, "warnings": []any{},
	}
}

// version 插入一条来源版本并按需设为基线。
func (e *webSnapEnv) version(id string, capturedAt int64, snapshot map[string]any, baseline bool) {
	e.t.Helper()
	payload := map[string]any{
		"id": id, "itemId": e.itemID, "sourceUrl": "https://mp.weixin.qq.com/s/abc", "title": "标题" + id, "markdown": "md",
		"contentHash": webContentHash("md"), "capturedAt": capturedAt, "engineVersion": "wechat/1", "complete": true, "kind": "remote",
	}
	if snapshot != nil {
		payload["snapshot"] = snapshot
	}
	raw, _ := json.Marshal(payload)
	e.exec("INSERT INTO web_source_versions VALUES (?,?,?,?)", id, e.itemID, string(raw), capturedAt)
	if baseline {
		e.exec("INSERT INTO web_source_baselines VALUES (?,?,?,?,?,?,?) ON CONFLICT(item_id) DO UPDATE SET version_id=excluded.version_id,content_hash=excluded.content_hash",
			e.itemID, id, webContentHash("正文内容"), "公众号文章", 1, webContentHash("md"), 0)
	}
}

func TestWebSnapshotIDValidation(t *testing.T) {
	e := newWebSnapEnv(t)
	long := strings.Repeat("x", 101)
	for _, ch := range []string{"web:snapshot", "web:versions", "web:snapshot-export"} {
		for _, bad := range []any{"", long, 5, nil} {
			r := e.call(ch, bad, "v1")
			if r["ok"] != false || r["error"] != "记录 ID 无效" {
				t.Errorf("%s(%v) = %v", ch, bad, r)
			}
		}
	}
	if r := e.call("web:snapshot", e.itemID, 5); r["error"] != "记录 ID 无效" {
		t.Errorf("版本 ID 类型不对应被拒绝：%v", r)
	}
	if r := e.call("web:snapshot", "nope"); r["error"] != "条目不存在" {
		t.Errorf("不存在的条目：%v", r)
	}
	if r := e.call("web:versions", "nope"); r["error"] != "条目不存在" {
		t.Errorf("不存在的条目：%v", r)
	}
}

func TestWebSnapshotRead(t *testing.T) {
	e := newWebSnapEnv(t)
	// 没有任何版本 → version:null，edited 取决于是否有基线
	r := e.call("web:snapshot", e.itemID)
	data := r["data"].(map[string]any)
	if r["ok"] != true || data["version"] != nil || data["edited"] != false || data["pending"] != false {
		t.Fatalf("无版本：%v", r)
	}
	name, hash := e.image("abc")
	dirty := `<p onclick="x()" style="color:red;position:fixed">正文<script>alert(1)</script></p><img src="local-image://` + name + `"><img src="https://evil/x.png">`
	e.version("v1", 1000, snapshotFor(name, hash, dirty, `@import url(x);p{color:red;background:url(https://evil)}`), true)
	r = e.call("web:snapshot", e.itemID)
	data = r["data"].(map[string]any)
	version := data["version"].(map[string]any)
	if r["ok"] != true || version["id"] != "v1" {
		t.Fatalf("读取快照：%v", r)
	}
	snap := version["snapshot"].(map[string]any)
	if strings.Contains(snap["html"].(string), "script") || strings.Contains(snap["html"].(string), "onclick") || snap["css"] != "p{color:red}" {
		t.Errorf("快照应被再次净化：%v", snap)
	}
	doc := data["document"].(string)
	if !strings.Contains(doc, "local-image://"+name) || !strings.Contains(doc, `data-instance="`+data["instanceId"].(string)+`"`) || !strings.Contains(doc, "<script>") {
		t.Errorf("文档应带实例标识与桥接脚本：%.300s", doc)
	}
	if data["edited"] != false || data["pending"] != false {
		t.Errorf("edited/pending：%v", data)
	}
	// 用户改了正文 → edited
	e.exec("UPDATE knowledge_items SET content=? WHERE id=?", "改过的正文", e.itemID)
	if d := e.call("web:snapshot", e.itemID)["data"].(map[string]any); d["edited"] != true {
		t.Errorf("正文变化后 edited 应为 true：%v", d)
	}
	// 更新的版本带快照 → pending
	e.version("v2", 2000, snapshotFor(name, hash, "<p>新</p>", ""), false)
	if d := e.call("web:snapshot", e.itemID)["data"].(map[string]any); d["pending"] != true {
		t.Errorf("存在更新快照时 pending 应为 true：%v", d)
	}
	// 指定版本；不存在的版本
	d := e.call("web:snapshot", e.itemID, "v2")["data"].(map[string]any)
	if d["version"].(map[string]any)["id"] != "v2" || d["pending"] != false {
		t.Errorf("指定版本：%v", d)
	}
	if r := e.call("web:snapshot", e.itemID, "zzz"); r["error"] != "原文版本不存在" {
		t.Errorf("不存在的版本：%v", r)
	}
}

func TestWebSnapshotCorruption(t *testing.T) {
	e := newWebSnapEnv(t)
	name, hash := e.image("abc")
	// 图片被篡改 → 校验失败（以 error 形式返回，version:null）
	e.version("v1", 1000, snapshotFor(name, hash, "<p>x</p>", ""), true)
	if err := os.WriteFile(filepath.Join(e.l.paths.ImagesDir(), name), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := e.call("web:snapshot", e.itemID)["data"].(map[string]any)
	if d["version"] != nil || d["error"] != "原文图片校验失败，请重新补采" {
		t.Errorf("篡改：%v", d)
	}
	// 图片缺失
	if err := os.Remove(filepath.Join(e.l.paths.ImagesDir(), name)); err != nil {
		t.Fatal(err)
	}
	d = e.call("web:snapshot", e.itemID)["data"].(map[string]any)
	if d["version"] != nil || !strings.Contains(d["error"].(string), "原文图片缺失") {
		t.Errorf("缺失：%v", d)
	}
	// 快照格式无效
	bad := snapshotFor(name, hash, "<p>x</p>", "")
	bad["formatVersion"] = 2
	e.exec("DELETE FROM web_source_baselines")
	e.exec("DELETE FROM web_source_versions")
	e.version("v9", 3000, bad, true)
	d = e.call("web:snapshot", e.itemID)["data"].(map[string]any)
	if d["version"] != nil || d["error"] != "原文快照格式无效" {
		t.Errorf("格式无效：%v", d)
	}
}

func TestWebVersions(t *testing.T) {
	e := newWebSnapEnv(t)
	name, hash := e.image("abc")
	e.version("v1", 1000, snapshotFor(name, hash, `<p onclick="x">a</p>`, ""), true)
	bad := snapshotFor(name, hash, "<p>b</p>", "")
	bad["formatVersion"] = 9
	e.version("v2", 2000, bad, false)
	e.version("v3", 3000, nil, false)
	d := e.call("web:versions", e.itemID)["data"].(map[string]any)
	versions := d["versions"].([]any)
	if len(versions) != 3 || versions[0].(map[string]any)["id"] != "v3" {
		t.Fatalf("版本按 capturedAt 降序：%v", versions)
	}
	if _, has := versions[1].(map[string]any)["snapshot"]; has {
		t.Errorf("无效快照应被移除：%v", versions[1])
	}
	if s := versions[2].(map[string]any)["snapshot"].(map[string]any); strings.Contains(s["html"].(string), "onclick") {
		t.Errorf("有效快照应被净化：%v", s)
	}
	if d["content"] != "正文内容" || d["title"] != "公众号文章" || d["contentHash"] != webContentHash("正文内容") || d["summaryStale"] != false || d["pendingVersion"] != true {
		t.Errorf("其余字段：%v", d)
	}
}

func TestWebSnapshotExport(t *testing.T) {
	e := newWebSnapEnv(t)
	name, hash := e.image("abc")
	snap := snapshotFor(name, hash, `<p>正文</p><img src="local-image://`+name+`">`, "p{color:red}")
	snap["warnings"] = []any{"某图片未保存"}
	e.version("v1", 1700000000123, snap, true)
	// 没有对话框 / 取消
	if r := e.call("web:snapshot-export", e.itemID, "v1")["data"].(map[string]any); r["canceled"] != true {
		t.Errorf("无对话框应视为取消：%v", r)
	}
	parent := t.TempDir()
	e.l.pickDirectory = func(title string) (string, error) {
		if title != "选择 HTML 导出目录" {
			t.Errorf("对话框标题：%q", title)
		}
		return parent, nil
	}
	r := e.call("web:snapshot-export", e.itemID, "v1")
	data := r["data"].(map[string]any)
	if r["ok"] != true || data["incomplete"] != true {
		t.Fatalf("导出：%v", r)
	}
	dir := data["path"].(string)
	html, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(html)
	for _, want := range []string{"assets/" + name, "<header><h1>标题v1</h1><p>账号&lt;b&gt; 作者</p><p>采集于 2023-11-14T22:13:20.123Z</p>", `href="https://mp.weixin.qq.com/s/abc" rel="noreferrer">打开原文`} {
		if !strings.Contains(doc, want) {
			t.Errorf("导出文档缺少 %q：%.400s", want, doc)
		}
	}
	if strings.Contains(doc, "local-image://") || strings.Contains(doc, "<script>") {
		t.Errorf("导出文档不应含本地协议或脚本")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "assets", name)); string(got) != "abc" {
		t.Errorf("资源未复制：%q", got)
	}
	entries, _ := os.ReadDir(parent)
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".tmp") {
			t.Errorf("暂存目录应已改名：%s", en.Name())
		}
	}
	// 取消选择
	e.l.pickDirectory = func(string) (string, error) { return "", nil }
	if r := e.call("web:snapshot-export", e.itemID, "v1")["data"].(map[string]any); r["canceled"] != true {
		t.Errorf("取消：%v", r)
	}
	// 无快照的版本
	e.version("v2", 1, nil, false)
	if r := e.call("web:snapshot-export", e.itemID, "v2"); r["error"] != "此版本没有原文排版" {
		t.Errorf("无快照：%v", r)
	}
	if r := e.call("web:snapshot-export", e.itemID, "zzz"); r["error"] != "原文版本不存在" {
		t.Errorf("版本不存在：%v", r)
	}
}

func TestWebSnapshotEnqueue(t *testing.T) {
	e := newWebSnapEnv(t)
	if r := e.call("web:snapshot-enqueue", []any{}); r["error"] != "单次补采 1–50 篇文章" {
		t.Errorf("空列表：%v", r)
	}
	if r := e.call("web:snapshot-enqueue", "x"); r["error"] != "单次补采 1–50 篇文章" {
		t.Errorf("非数组：%v", r)
	}
	if r := e.call("web:snapshot-enqueue", make([]any, 51)); r["error"] != "单次补采 1–50 篇文章" {
		t.Errorf("超过 50：%v", r)
	}
	if r := e.call("web:snapshot-enqueue", []any{e.itemID}); r["error"] != "此条目不是微信公众号文章" {
		t.Errorf("无来源：%v", r)
	}
	e.exec("INSERT INTO source_records (id,item_id,source_type,source_uri,access_uri,captured_at) VALUES (?,?,?,?,?,?)", "s1", e.itemID, "url", "https://example.com/a", "https://mp.weixin.qq.com/s/xyz?token=1", 5)
	r := e.call("web:snapshot-enqueue", []any{e.itemID, e.itemID})
	tasks, _ := r["data"].([]any)
	if r["ok"] != true || len(tasks) != 1 {
		t.Fatalf("入队（重复 ID 去重）：%v", r)
	}
	task := tasks[0].(map[string]any)
	if task["sourceInput"] != "https://mp.weixin.qq.com/s/xyz?token=1" || task["refreshOfItemId"] != e.itemID {
		t.Errorf("任务：%v", task)
	}
	// 访问地址为空时回退 source_uri，且必须是公众号
	e.exec("UPDATE source_records SET access_uri=NULL WHERE id='s1'")
	if r := e.call("web:snapshot-enqueue", []any{e.itemID}); r["error"] != "此条目不是微信公众号文章" {
		t.Errorf("回退到非公众号地址：%v", r)
	}
	// 已删除条目
	e.exec("UPDATE knowledge_items SET deleted_at=1 WHERE id=?", e.itemID)
	if r := e.call("web:snapshot-enqueue", []any{e.itemID}); r["error"] != "条目不存在或已删除" {
		t.Errorf("已删除：%v", r)
	}
}

func TestIsWechatURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://mp.weixin.qq.com/s/abc": true, "http://mp.weixin.qq.com/": true, "HTTPS://MP.WEIXIN.QQ.COM/x": true,
		"https://mp.weixin.qq.com.evil.com/": false, "ftp://mp.weixin.qq.com/": false, "mp.weixin.qq.com": false, "": false, "https://weixin.qq.com/": false,
	} {
		if got := isWechatURL(in); got != want {
			t.Errorf("isWechatURL(%q) = %v", in, got)
		}
	}
}

func TestWebSolverCheck(t *testing.T) {
	e := newWebSnapEnv(t)
	var lastBody string
	respond := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := make([]byte, 256)
			n, _ := r.Body.Read(buf)
			lastBody = string(buf[:n])
			if r.URL.Path != "/v1" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("请求不符：%s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}
	portOf := func(s *httptest.Server) int {
		_, p, _ := net.SplitHostPort(s.Listener.Addr().String())
		var n int
		_, _ = fmt.Sscanf(p, "%d", &n)
		return n
	}
	settings := func(port int) map[string]any {
		return map[string]any{"enabled": true, "connection": "local", "port": port, "sshHost": ""}
	}
	ok := respond(200, `{"status":"ok","sessions":[]}`)
	defer ok.Close()
	r := e.call("web:solverCheck", settings(portOf(ok)))
	if r["ok"] != true || r["data"].(map[string]any)["connected"] != true || lastBody != `{"cmd":"sessions.list"}` {
		t.Errorf("连通：%v body=%q", r, lastBody)
	}
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{200, `{"status":"error","message":"Timeout after 60s"}`, "FlareSolverr 验证超时（60 秒），请稍后重试"},
		{500, `{"status":"error","message":"boom with cookie=abc"}`, "FlareSolverr 未能完成请求，请检查服务状态或稍后重试"},
		{200, `not json`, "FlareSolverr 返回了无效响应"},
	}
	for _, c := range cases {
		s := respond(c.status, c.body)
		r := e.call("web:solverCheck", settings(portOf(s)))
		s.Close()
		if r["ok"] != false || r["error"] != c.want {
			t.Errorf("%s → %v", c.body, r)
		}
	}
	// 端口无服务
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if r := e.call("web:solverCheck", settings(closedPort)); r["error"] != "无法连接 FlareSolverr，请检查服务是否启动" {
		t.Errorf("无服务：%v", r)
	}
	// 设置无效
	if r := e.call("web:solverCheck", map[string]any{"enabled": true}); r["error"] != "FlareSolverr 设置无效，请填写有效端口和 SSH 主机别名" {
		t.Errorf("无效设置：%v", r)
	}
	if r := e.call("web:solverCheck", map[string]any{"enabled": true, "connection": "ssh", "port": 8191, "sshHost": ""}); r["error"] != "请填写已有 SSH 配置中的主机别名" {
		t.Errorf("缺少主机别名：%v", r)
	}
}
