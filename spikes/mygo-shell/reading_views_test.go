package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadingViewCommandValidation(t *testing.T) {
	ok := []string{
		`{"type":"appearance","theme":"dark","fontSize":16}`,
		`{"type":"appearance","theme":"light","fontSize":12,"fontFamily":"serif"}`,
		`{"type":"find","query":"","index":0,"requestId":"a-1_b"}`,
		`{"type":"find","query":"x","index":-1}`,
		`{"type":"anchor","id":"sec-1"}`,
		`{"type":"scroll","top":0}`,
		`{"type":"scroll","top":2000000}`,
		`{"type":"interaction","enabled":false}`,
	}
	bad := map[string]string{
		`null`: "阅读命令无效", `{"type":"x"}`: "阅读命令无效", `"find"`: "阅读命令无效",
		`{"type":"appearance","theme":"blue","fontSize":16}`:                "阅读外观参数无效",
		`{"type":"appearance","theme":"dark","fontSize":11}`:                "阅读外观参数无效",
		`{"type":"appearance","theme":"dark","fontSize":33}`:                "阅读外观参数无效",
		`{"type":"appearance","theme":"dark","fontSize":"16"}`:              "阅读外观参数无效",
		`{"type":"appearance","theme":"dark","fontSize":16,"fontFamily":1}`: "阅读外观参数无效",
		`{"type":"find","query":1,"index":0}`:                               "阅读查找参数无效",
		`{"type":"find","query":"x","index":1.5}`:                           "阅读查找参数无效",
		`{"type":"find","query":"x","index":0,"requestId":"a b"}`:           "阅读查找参数无效",
		`{"type":"find","query":"x","index":0,"requestId":null}`:            "阅读查找参数无效",
		`{"type":"anchor","id":"1abc"}`:                                     "阅读章节无效",
		`{"type":"anchor"}`:                                                 "阅读章节无效",
		`{"type":"scroll","top":-1}`:                                        "阅读位置无效",
		`{"type":"scroll","top":2000001}`:                                   "阅读位置无效",
		`{"type":"interaction","enabled":"true"}`:                           "阅读交互开关无效",
	}
	for _, s := range ok {
		v, _ := jsParse(s)
		if err := validateReadingViewCommand(v); err != nil {
			t.Errorf("%s 应通过：%v", s, err)
		}
	}
	for s, want := range bad {
		v, _ := jsParse(s)
		if err := validateReadingViewCommand(v); err == nil || err.Error() != want {
			t.Errorf("%s 期望 %q，得到 %v", s, want, err)
		}
	}
}

func TestReadingViewBoundsValidation(t *testing.T) {
	for s, good := range map[string]bool{
		`{"x":0,"y":0,"width":100,"height":50,"visible":true}`:   true,
		`{"x":-5,"y":3,"width":0,"height":0,"visible":false}`:    true,
		`{"x":0,"y":0,"width":20001,"height":50,"visible":true}`: false,
		`{"x":0,"y":0,"width":-1,"height":50,"visible":true}`:    false,
		`{"x":"0","y":0,"width":1,"height":1,"visible":true}`:    false,
		`{"x":0,"y":0,"width":1,"height":1}`:                     false,
		`null`:                                                   false,
	} {
		v, _ := jsParse(s)
		if err := validateReadingViewBounds(v); (err == nil) != good {
			t.Errorf("%s 期望通过=%v，得到 %v", s, good, err)
		}
	}
}

type viewEvents struct {
	mu  sync.Mutex
	got []string
}

func (v *viewEvents) add(ch string, p any) {
	s, _ := jsStringify(p)
	v.mu.Lock()
	v.got = append(v.got, ch+" "+s)
	v.mu.Unlock()
}
func (v *viewEvents) list() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.got...)
}

func newReadingViewTestHost(t *testing.T) (*readingViewHost, *readingView, *viewEvents) {
	t.Helper()
	l := &Library{}
	evs := &viewEvents{}
	l.push = evs.add
	h := l.readingViews()
	page, _ := jsParse(`{"assets":[{"id":"img1","status":"ready"},{"id":"img2","status":"failed"}],"design":{"scripts":[{"id":"calc"}]}}`)
	e := &readingView{id: "v1", page: page.(*jsObj), enabled: true, stop: make(chan struct{})}
	h.views[e.id] = e
	return h, e, evs
}

func TestReadingViewReceive(t *testing.T) {
	h, e, evs := newReadingViewTestHost(t)
	send := func(s string) {
		v, _ := jsParse(s)
		h.receive("v1", v)
	}
	send(`{"reading":3,"type":"ready"}`)
	send(`{"type":"key","value":"find"}`)
	send(`{"type":"key","value":"x"}`)
	send(`{"type":"layout","value":{"top":1,"height":2,"headings":[{"id":"a1","text":"标题"}]}}`)
	send(`{"type":"layout","value":{"top":1,"height":2,"headings":[{"id":"1a","text":"标题"}]}}`)
	send(`{"type":"selection","value":{"text":"选中","x":1,"y":2}}`)
	send(`{"type":"selection","value":{"text":"选中","x":1e9,"y":2}}`)
	send(`{"type":"find","value":{"count":3}}`)
	send(`{"type":"find","value":{"count":-1}}`)
	send(`{"type":"image","value":{"id":"img1"}}`)
	send(`{"type":"image","value":{"id":"img2"}}`)
	send(`{"type":"nope","value":1}`)
	got := evs.list()
	want := []string{
		`themedReading:viewEvent {"viewId":"v1","type":"ready"}`,
		`themedReading:viewEvent {"viewId":"v1","type":"key","value":"find"}`,
		`themedReading:viewEvent {"viewId":"v1","type":"layout","value":{"top":1,"height":2,"headings":[{"id":"a1","text":"标题"}]}}`,
		`themedReading:viewEvent {"viewId":"v1","type":"selection","value":{"text":"选中","x":1,"y":2}}`,
		`themedReading:viewEvent {"viewId":"v1","type":"find","value":{"count":3}}`,
		`themedReading:viewEvent {"viewId":"v1","type":"image","value":{"id":"img1"}}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("事件不符：\n%s\n期望：\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// 故障：已知脚本带编号，只发一次
	send(`{"type":"fault","value":{"id":"calc","message":"boom"}}`)
	send(`{"type":"fault","value":{"id":"calc","message":"again"}}`)
	got = evs.list()
	last := got[len(got)-1]
	if !strings.Contains(last, `"type":"fault"`) || !strings.Contains(last, "交互 calc 运行失败：boom") || len(got) != len(want)+1 {
		t.Fatalf("故障事件不符：%v", got)
	}
	if e.fault == "" {
		t.Fatal("实例应已标记故障")
	}
}

func TestReadingViewHeartbeat(t *testing.T) {
	h, e, evs := newReadingViewTestHost(t)
	h.startHeartbeat(e)
	var nonce string
	deadline := time.Now().Add(3 * time.Second)
	for nonce == "" && time.Now().Before(deadline) {
		for _, s := range evs.list() {
			if strings.HasPrefix(s, chReadingViewCmd+" ") {
				var m struct {
					Command struct{ Nonce string } `json:"command"`
				}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(s, chReadingViewCmd+" ")), &m)
				nonce = m.Command.Nonce
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if nonce == "" {
		t.Fatal("未收到心跳命令")
	}
	// 错误的 nonce 不应解除等待；正确的才行
	v, _ := jsParse(`{"type":"heartbeat","value":"wrong"}`)
	h.receive("v1", v)
	h.mu.Lock()
	still := e.nonce == nonce
	h.mu.Unlock()
	if !still {
		t.Fatal("错误 nonce 不应被接受")
	}
	v, _ = jsParse(`{"type":"heartbeat","value":"` + nonce + `"}`)
	h.receive("v1", v)
	h.mu.Lock()
	cleared := e.nonce == ""
	h.mu.Unlock()
	if !cleared {
		t.Fatal("正确 nonce 应清除等待")
	}
	e.halt()
}

func TestReadingViewHeartbeatTimeout(t *testing.T) {
	old := readingHeartbeatBudget
	readingHeartbeatBudget = 600 * time.Millisecond
	defer func() { readingHeartbeatBudget = old }()
	h, e, evs := newReadingViewTestHost(t)
	h.startHeartbeat(e)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range evs.list() {
			if strings.Contains(s, `"type":"fault"`) && strings.Contains(s, "五秒未响应") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("心跳超时应触发故障")
}

// 用 TS 基线里的真实 v3 页面生成外壳文档，校验中继脚本注入与素材处理。
func TestReadingViewDocumentFromGolden(t *testing.T) {
	dir := os.Getenv("GUIZHI_THEMED_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs []forumRec `json:"recs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, r := range g.Recs {
		if r.Kind != "thmV3Inner" || checked >= 3 {
			continue
		}
		var in struct {
			Version json.RawMessage `json:"version"`
		}
		mustJSON(t, r.Input, &in)
		if !strings.Contains(string(r.Output), `"ok"`) {
			continue
		}
		pv, err := jsParse(string(in.Version))
		if err != nil {
			continue
		}
		page, ok := pv.(*jsObj)
		if !ok {
			continue
		}
		h, e, _ := newReadingViewTestHost(t)
		e.page, e.enabled = page, true
		doc, err := h.document(e)
		if err != nil {
			t.Fatalf("文档生成失败：%v", err)
		}
		if !strings.Contains(doc, `sandbox="allow-scripts"`) || !strings.Contains(doc, `gzReadingView`) || !strings.Contains(doc, `var v="v1"`) {
			t.Fatalf("外壳或中继缺失：%.300s", doc)
		}
		if strings.Index(doc, "gzReadingView") > strings.LastIndex(doc, "</body>") {
			t.Fatal("中继应注入在 </body> 之前")
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("基线里没有可用的 v3 页面")
	}
}
