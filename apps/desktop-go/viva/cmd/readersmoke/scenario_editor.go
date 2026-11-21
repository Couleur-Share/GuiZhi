//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"

	"guizhi-mygo-shell/viva/reader"
)

// ---- editor：编辑态（阅读岛里复用的 CodeMirror，台账 C-012）----
//
// 真 WebView2，只用合成的 DOM 事件与宿主消息：没有真键盘、真输入法与真鼠标（台账 R-15）。
// 覆盖：懒加载的分块真的取到并挂好、焦点请求、输入到宿主的草稿、回声不重发、撤销留在编辑器里、
// 转交的快捷键收窄、宿主换内容、空草稿的占位文字、大文档的往返、离开编辑态之后不再有改动事件、两种主题的底色。

// editorFixture 是编辑态的草稿：标题、段落、列表与代码块（几行就够，不必滚动）。
const editorFixture = "# 并发模式笔记\n\n第一段：goroutine 与 channel。\n\n- 项一\n- 项二\n\n```go\nfunc main() {}\n```\n"

// editorQuery 取页面里编辑器的内容区（后面接着写脚本的其余部分）。
const editorQuery = `const c=document.querySelector('[data-testid="markdown-editor"] .cm-content');`

// editorLinesJS 把编辑器的全文读成一个字符串（每个 .cm-line 一行；空文档里占位文字也在 .cm-line 里，先去掉）。
const editorLinesJS = `const lines=c?[...c.querySelectorAll('.cm-line')].map(l=>{const k=l.cloneNode(true);k.querySelectorAll('.cm-placeholder').forEach(p=>p.remove());return k.textContent}):[];`

func editorView(draft string) reader.View {
	return reader.View{Kind: reader.ViewEditor, Content: draft, SourceKind: "body"}
}

// pollProbe 反复在页面里跑 body，直到 done 对结果返回真或超时；返回最后一次的结果。
func (s *smoke) pollProbe(name, body string, timeout time.Duration, done func(map[string]any) bool) (map[string]any, bool) {
	deadline := time.Now().Add(timeout)
	for {
		got, ok := s.probe(name, body, 3*time.Second)
		if ok && done(got) {
			return got, true
		}
		if time.Now().After(deadline) {
			return got, false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// editEvent 取 edit 事件里的文字（大文档不记全文，只记字节数）。
func editEvent(d any) (text string, bytes int, ok bool) {
	m, _ := d.(map[string]any)
	if m == nil {
		return "", 0, false
	}
	text, _ = m["text"].(string)
	bytes, _ = m["bytes"].(int)
	return text, bytes, true
}

func scEditor(s *smoke, r *result) {
	took, ok := s.fresh(s.request(editorView(editorFixture), reader.TabBody, true))
	r.set("openToRenderedMs", ms(took)) // 含懒加载分块（约 502 KB）的读取、解析与 CodeMirror 的首次挂载
	if !ok {
		r.check("渲染：编辑器挂好之后页面才报 rendered", false, nil)
		return
	}
	settle()

	// 1. 挂载：分块取到、可编辑、内容是宿主给的草稿、不自动聚焦、主题字号。
	dom, ok := s.probe("editor-dom", editorQuery+editorLinesJS+
		`const flag=document.querySelector('[data-article-editing]');`+
		`const chunks=performance.getEntriesByType('resource').map(e=>e.name.split('/').pop()).filter(n=>/MarkdownEditor/.test(n));`+
		`const a=document.activeElement;const cs=c?getComputedStyle(c):null;`+
		`return {has:!!document.querySelector('[data-testid="markdown-editor"]'),editable:c?c.getAttribute('contenteditable'):null,text:lines.join('\n'),`+
		`editing:flag?flag.getAttribute('data-article-editing'):null,chunks:chunks,active:a?String(a.className||a.tagName):null,`+
		`fontSize:cs?cs.fontSize:null,fontFamily:cs?cs.fontFamily:null,placeholder:!!document.querySelector('.cm-placeholder')}`, 5*time.Second)
	if !ok {
		r.check("探针回报", false, nil)
		return
	}
	r.set("dom", dom)
	r.check("页面里有编辑器，内容区可编辑", dom["has"] == true && dom["editable"] == "true", dom)
	r.check("编辑器里是宿主给的草稿（逐字相等）", dom["text"] == editorFixture, dom["text"])
	r.check("阅读区根节点标了编辑态（data-article-editing）", dom["editing"] == "true", dom["editing"])
	r.check("懒加载的 MarkdownEditor 分块经 gzreader 取到", strings.Contains(fmt.Sprint(dom["chunks"]), "MarkdownEditor"), dom["chunks"])
	r.check("挂载时不自动聚焦（渲染层也不聚焦）", !strings.Contains(fmt.Sprint(dom["active"]), "cm-content"), dom["active"])
	r.check("编辑器沿用渲染层的主题字号 14px", dom["fontSize"] == "14px", dom["fontSize"])
	r.check("有内容时没有占位文字", dom["placeholder"] == false, dom["placeholder"])

	dark, err := s.shot("editor-dark")
	if err != nil {
		r.check("截图（深色）", false, err.Error())
	} else {
		corner := s.at(dark, 8, 8)
		r.check("深色：阅读区底色是深色", luma(corner) < 0.35, rgb(corner))
	}

	// 2. 焦点请求：宿主 FocusEditor → 原生焦点交给岛 → 页面把 .cm-content 聚焦。
	mark := s.mark()
	var asked bool
	s.onUI(func() { asked = s.host.FocusEditor() })
	r.check("宿主要求聚焦编辑器：页面已就绪，返回 true", asked, nil)
	r.check("宿主把原生焦点交给了岛", len(s.eventsSince(mark, "focus")) > 0, s.eventsSince(mark, "focus"))
	focus, focused := s.pollProbe("editor-focus", `const a=document.activeElement;return {cls:a?String(a.className||a.tagName):''}`, 3*time.Second,
		func(m map[string]any) bool { return strings.Contains(fmt.Sprint(m["cls"]), "cm-content") })
	r.check("页面里 .cm-content 拿到焦点", focused, focus)

	// 3. 输入：合成的 insertText（Chromium 会派 beforeinput / input，CodeMirror 从 DOM 读到改动）。
	if _, ok := s.probe("editor-hook", `window.__msgs=[];const orig=window.viva.onmessage;`+
		`window.viva.onmessage=(t)=>{window.__msgs.push(String(t).slice(0,60));if(orig)orig(t)};return {hooked:typeof orig}`, 5*time.Second); !ok {
		r.check("装记录器", false, nil)
		return
	}
	const typed = "新增的字"
	mark = s.mark()
	typedDOM, ok := s.probe("editor-type", editorQuery+`c.focus();const ok=document.execCommand('insertText',false,'`+typed+`');`+editorLinesJS+
		`return {ok:ok,text:lines.join('\n')}`, 5*time.Second)
	if !ok {
		r.check("探针回报（输入）", false, nil)
		return
	}
	pageText, _ := typedDOM["text"].(string)
	r.check("insertText 生效：页面里的文档含输入的字", typedDOM["ok"] == true && strings.Contains(pageText, typed), typedDOM)
	e, ok := s.waitEvent(mark, "edit", 3*time.Second, func(d any) bool { t, _, _ := editEvent(d); return strings.Contains(t, typed) })
	if ok {
		got, _, _ := editEvent(e.Data)
		m, _ := e.Data.(map[string]any)
		r.check("宿主收到 edit：条目与标签对、全文等于页面里的文档", m["item"] == itemID && m["tab"] == string(reader.TabBody) && got == pageText, e.Data)
		r.check("全文只多了输入的字", len([]rune(got)) == len([]rune(editorFixture))+len([]rune(typed)), len([]rune(got)))
	} else {
		r.check("输入之后宿主收到 edit", false, nil)
	}
	time.Sleep(800 * time.Millisecond)
	after, _ := s.probe("editor-after", editorQuery+editorLinesJS+
		`return {views:window.__msgs.filter(m=>m.indexOf('"t":"view"')>=0).length,total:window.__msgs.length,text:lines.join('\n')}`, 5*time.Second)
	r.set("afterTyping", after)
	r.check("回声抑制：宿主没有把同一份草稿再发回页面", after["views"] == 0.0, after)
	afterText, _ := after["text"].(string)
	r.check("输入的字还在编辑器里（没被重置）", strings.Contains(afterText, typed), after["text"])

	// 4. 撤销留在编辑器里：Ctrl+Z 落在 .cm-content 上由 CodeMirror 处理，不转交宿主。
	mark = s.mark()
	undone, ok := s.probe("editor-undo", editorQuery+`c.dispatchEvent(new KeyboardEvent('keydown',{key:'z',code:'KeyZ',keyCode:90,ctrlKey:true,bubbles:true,cancelable:true}));`+
		editorLinesJS+`return {text:lines.join('\n')}`, 5*time.Second)
	if ok {
		r.check("Ctrl+Z 撤销了输入（文档回到草稿）", undone["text"] == editorFixture, undone["text"])
	} else {
		r.check("探针回报（撤销）", false, nil)
	}
	if e, ok := s.waitEvent(mark, "edit", 3*time.Second, func(d any) bool { t, _, _ := editEvent(d); return t == editorFixture }); ok {
		r.check("撤销之后宿主收到回到草稿的 edit", true, e.Data)
	} else {
		r.check("撤销之后宿主收到回到草稿的 edit", false, nil)
	}
	r.check("Ctrl+Z 不转交宿主（它是编辑器的撤销）", len(s.eventsSince(mark, "key")) == 0, s.eventsSince(mark, "key"))

	// 5. 转交的快捷键收窄为 Ctrl+F / Ctrl+S / Alt+Z；其余留给编辑器。
	fireKey := func(init string) (event, bool) {
		keyMark := s.mark()
		s.onUI(func() {
			if s.isl != nil {
				s.isl.View().Eval(`document.dispatchEvent(new KeyboardEvent('keydown',` + init + `))`)
			}
		})
		return s.waitEvent(keyMark, "key", 700*time.Millisecond, nil)
	}
	for _, c := range []struct{ name, init, key, mod string }{
		{"Ctrl+F", `{key:'f',ctrlKey:true,bubbles:true,cancelable:true}`, "f", "ctrl"},
		{"Ctrl+S", `{key:'s',ctrlKey:true,bubbles:true,cancelable:true}`, "s", "ctrl"},
		{"Alt+Z", `{key:'z',altKey:true,bubbles:true,cancelable:true}`, "z", "alt"},
	} {
		if e, ok := fireKey(c.init); ok {
			m, _ := e.Data.(map[string]any)
			r.check("编辑态里 "+c.name+" 转交宿主", m["key"] == c.key && m[c.mod] == true, e.Data)
		} else {
			r.check("编辑态里 "+c.name+" 转交宿主", false, nil)
		}
	}
	for _, c := range []struct{ name, init string }{
		{"Alt+←", `{key:'ArrowLeft',altKey:true,bubbles:true,cancelable:true}`},
		{"Alt+→", `{key:'ArrowRight',altKey:true,bubbles:true,cancelable:true}`},
		{"Ctrl+Z", `{key:'z',ctrlKey:true,bubbles:true,cancelable:true}`},
		{"不带修饰键的 s", `{key:'s',bubbles:true,cancelable:true}`},
	} {
		_, ok := fireKey(c.init)
		r.check("编辑态里 "+c.name+" 不转交（留给编辑器）", !ok, nil)
	}

	// 6. 宿主换内容（不是回声）：编辑器换成新文档；空草稿显示占位文字；换回草稿。
	replace := func(draft string) (map[string]any, bool) {
		s.show(s.request(editorView(draft), reader.TabBody, true), nil)
		return s.pollProbe("editor-text", editorQuery+editorLinesJS+
			`const p=document.querySelector('.cm-placeholder');return {text:lines.join('\n'),placeholder:p?p.textContent:''}`, 6*time.Second,
			func(m map[string]any) bool { return m["text"] == draft })
	}
	const other = "# 换成别的内容\n\n宿主给的新草稿。\n"
	got, ok := replace(other)
	r.check("宿主给了不同的内容：编辑器换成新文档", ok, got)
	got, ok = replace("")
	r.check("空草稿：编辑器是空的", ok, got)
	r.check("空草稿显示占位文字「编辑 Markdown 内容…」", got["placeholder"] == "编辑 Markdown 内容…", got["placeholder"])
	_, ok = replace(editorFixture)
	r.check("换回草稿", ok, nil)

	// 7. 大文档：一次塞进约 50 万字（远小于 B-070 的上限），看宿主多久收到全文、页面是否仍能响应。
	mark = s.mark()
	start := time.Now()
	ins, ok := s.probe("editor-big", editorQuery+`const dt=new DataTransfer();`+
		`const parts=[];for(let i=0;i<20000;i++)parts.push('第 '+i+' 行：并发并发并发，用来撑大文档。');const big=parts.join('\n');dt.setData('text/plain',big);`+
		`const t0=performance.now();c.dispatchEvent(new ClipboardEvent('paste',{clipboardData:dt,bubbles:true,cancelable:true}));`+
		`return {chars:big.length,pasteMs:Math.round(performance.now()-t0)}`, 15*time.Second)
	r.set("bigInsert", ins)
	if !ok || ins["error"] != nil {
		r.check("探针回报（大文档）", false, ins)
	} else {
		chars, _ := ins["chars"].(float64)
		if e, ok := s.waitEvent(mark, "edit", 20*time.Second, func(d any) bool { _, n, _ := editEvent(d); return n >= 1_000_000 }); ok {
			_, n, _ := editEvent(e.Data)
			r.set("bigInsertToHostMs", ms(time.Since(start)))
			r.set("bigInsertHostBytes", n)
			r.check("宿主收到大文档的全文（edit，字节数不小于 100 万）", float64(n) >= chars, n)
		} else {
			r.check("宿主收到大文档的全文", false, nil)
		}
		start = time.Now()
		probeDOM, ok := s.probe("editor-after-big", `return {lines:document.querySelectorAll('.cm-line').length}`, 5*time.Second)
		r.set("responsiveAfterBigMs", ms(time.Since(start)))
		r.check("大文档之后页面仍响应（探针 5 秒内回报）", ok, probeDOM)
	}
	r.check("没有宿主诊断与页面失败", len(s.eventsSince(0, "diag")) == 0 && len(s.eventsSince(0, "failed")) == 0, []any{s.eventsSince(0, "diag"), s.eventsSince(0, "failed")})

	// 8. 离开编辑态：编辑器卸载，之后不再有改动事件。
	mark = s.mark()
	s.show(s.request(originalView(), reader.TabBody, true), nil)
	gone, ok := s.pollProbe("editor-gone", `return {has:!!document.querySelector('.cm-editor'),editing:(document.querySelector('[data-article-editing]')||{getAttribute:()=>null}).getAttribute('data-article-editing')}`,
		8*time.Second, func(m map[string]any) bool { return m["has"] == false })
	r.check("回到阅读：编辑器从页面里卸载", ok, gone)
	time.Sleep(600 * time.Millisecond)
	r.check("卸载之后没有多余的 edit 事件", len(s.eventsSince(mark, "edit")) == 0, len(s.eventsSince(mark, "edit")))

	// 9. 浅色主题的底色。
	if _, ok := s.fresh(s.request(editorView(editorFixture), reader.TabBody, false)); !ok {
		r.check("浅色：渲染", false, nil)
		return
	}
	settle()
	light, err := s.shot("editor-light")
	if err != nil {
		r.check("截图（浅色）", false, err.Error())
		return
	}
	corner := s.at(light, 8, 8)
	r.check("浅色：阅读区底色是浅色", luma(corner) > 0.8, rgb(corner))
}
