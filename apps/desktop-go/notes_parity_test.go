package main

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
)

// 原生界面用的 native/notes 与 native/services.MergeDraftTags 是主包同名实现的独立副本（原生包不能依赖 package main）。
// 这里在同一批随机语料上让两份实现逐项对拍：任何一份改了而另一份没跟，这个测试就红。
// 语料由固定种子生成（可复现），覆盖元数据引用块、各种小节标题与楼层头的写法、回复上下文、<br>、
// JS 与 Go 空白集不同的字符（U+00A0、U+3000、U+FEFF、U+0085）与 emoji。

var parityLines = []string{
	"> 平台：V2EX · 作者：x · 3 条回复", "> 平台:B站 · 作者：某人 · 时长：12:52", "> 发布：2026-07-16", "> 原标题：abc", "> 简介：甲 · 乙", "> 文字稿来源：音频识别", "> 普通引用",
	"## 讨论总结", "## 正文", "## 讨论（2 条）", "## 讨论（楼主 1 条 · 原帖共 2040 条）", "##  讨论", "## 讨论总结 ", "## 其他", "## 图中文字", "##正文",
	"**1 楼 · alice**", "**12楼·Bob**", "### 3 楼 · carol", "###4楼·dave", "### 5 楼 · ", "### 0 楼 · zero", "**7 楼 · alice**", "### 8 楼 · Alice",
	"> 回复 @alice：对方在问", "> 回复 @bob（3 楼）：<br/>摘要<br>文本", "> 回复 @x（99楼）：", "> 回复  @y：z", "> 回复 @alice（4 楼）：<p>段</p>",
	"", "", "", "正文行 ABC", "另一行 abc DEF", "![图](a.png)", "![图1](a.png) ![图2](b.png)", "\u00a0\u3000空白开头\u3000", "行内 <br/> 标签", "😀 emoji 行", "\ufeff带 BOM 的行", "\u0085NEL 行", "行尾回车\r",
}

func parityDoc(r *rand.Rand) string {
	n := 1 + r.Intn(40)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = parityLines[r.Intn(len(parityLines))]
	}
	sep := "\n"
	if r.Intn(10) == 0 {
		sep = "\n\n"
	}
	return strings.Join(lines, sep)
}

// mainReplies 把主包的回复转成 notes 的形状，两边用同一个类型比较。
func mainReplies(in []forumReplyEntry) []notes.ForumReply {
	var out []notes.ForumReply
	for _, e := range in {
		r := notes.ForumReply{Floor: e.Floor, Author: e.Author, Content: e.Content}
		if e.ReplyTo != nil {
			rt := &notes.ForumReplyTo{Author: e.ReplyTo.Author, Snippet: e.ReplyTo.Snippet}
			if e.ReplyTo.Floor != nil {
				rt.Floor, rt.HasFloor = *e.ReplyTo.Floor, true
			}
			r.ReplyTo = rt
		}
		out = append(out, r)
	}
	return out
}

func mainReplyTo(rt notes.ForumReplyTo) forumReplyTo {
	out := forumReplyTo{Author: rt.Author, Snippet: rt.Snippet, HasFloor: rt.HasFloor}
	if rt.HasFloor {
		f := rt.Floor
		out.Floor = &f
	}
	return out
}

func TestNotesPackageMatchesMainImplementation(t *testing.T) {
	r := rand.New(rand.NewSource(20261120))
	queries := []string{"", "  ", "楼", "alice", "ALICE", "3", "3 楼", "摘要", "😀", "\u3000abc\u00a0"}
	for i := 0; i < 4000; i++ {
		doc := parityDoc(r)
		fail := func(what string, want, got any) {
			t.Helper()
			t.Fatalf("第 %d 篇语料的 %s 不一致\n文档：%q\n主包：%+v\nnotes：%+v", i, what, doc, want, got)
		}

		want := splitForumNoteSections(doc)
		got := notes.SplitForumSections(doc)
		if want.Summary != got.Summary || want.Body != got.Body || want.Replies != got.Replies {
			fail("SplitForumSections", want, got)
		}
		wantReplies, gotReplies := mainReplies(parseForumReplies(doc)), notes.ParseForumReplies(doc)
		if !reflect.DeepEqual(wantReplies, gotReplies) {
			fail("ParseForumReplies", wantReplies, gotReplies)
		}
		if w, g := mainReplies(parseForumReplySection(doc)), notes.ParseForumReplySection(doc); !reflect.DeepEqual(w, g) {
			fail("ParseForumReplySection", w, g)
		}
		for _, q := range queries {
			w := mainReplies(filterForumReplies(mainRepliesBack(gotReplies), q))
			g := notes.FilterForumReplies(gotReplies, q)
			if !reflect.DeepEqual(w, g) {
				fail("FilterForumReplies "+q, w, g)
			}
		}
		for _, rep := range gotReplies {
			if rep.ReplyTo == nil {
				continue
			}
			wf, wok := resolveReplyTargetFloor(mainRepliesBack(gotReplies), mainReplyTo(*rep.ReplyTo))
			gf, gok := notes.ResolveReplyTargetFloor(gotReplies, *rep.ReplyTo)
			if wok != gok || (wok && wf != gf) {
				fail("ResolveReplyTargetFloor", fmt.Sprint(wf, wok), fmt.Sprint(gf, gok))
			}
		}

		wi, gi := splitImageNoteSections(doc), notes.SplitImageSections(doc)
		if wi.Caption != gi.Caption || wi.Recognized != gi.Recognized {
			fail("SplitImageSections", wi, gi)
		}

		wm, gm := parseVideoMetaBlock(doc), notes.ParseVideoMeta(doc)
		if (wm == nil) != (gm == nil) {
			fail("ParseVideoMeta 有无", wm, gm)
		}
		if wm != nil && (wm.platform != gm.Platform || wm.author != gm.Author || wm.duration != gm.Duration || wm.originalTitle != gm.OriginalTitle ||
			wm.description != gm.Description || wm.transcriptSource != gm.TranscriptSource || wm.body != gm.Body) {
			fail("ParseVideoMeta", wm, gm)
		}

		line := parityLines[r.Intn(len(parityLines))] + parityLines[r.Intn(len(parityLines))]
		for _, n := range []int{0, 1, 3, 200} {
			if w, g := normalizeForumSnippet(line, n), notes.NormalizeForumSnippet(line, n); w != g {
				fail(fmt.Sprintf("NormalizeForumSnippet(%d)", n), w, g)
			}
		}
	}
}

// mainRepliesBack 把 notes 的回复还原成主包的类型，喂给主包的过滤与解析函数。
func mainRepliesBack(in []notes.ForumReply) []forumReplyEntry {
	var out []forumReplyEntry
	for _, e := range in {
		r := forumReplyEntry{Floor: e.Floor, Author: e.Author, Content: e.Content}
		if e.ReplyTo != nil {
			rt := mainReplyTo(*e.ReplyTo)
			r.ReplyTo = &rt
		}
		out = append(out, r)
	}
	return out
}

func TestMergeDraftTagsMatchesMainImplementation(t *testing.T) {
	r := rand.New(rand.NewSource(20261121))
	pool := []string{"a", "A", " a ", "b", "B\u00a0", "İ", "i\u0307", "Σ", "ς", "σ", "\ufeffz", "z", "\u3000", "", "标签", "标签 ", "Straße", "STRASSE", "x\u0085"}
	pick := func() []string {
		n := r.Intn(6)
		out := make([]string, n)
		for i := range out {
			out[i] = pool[r.Intn(len(pool))]
		}
		return out
	}
	for i := 0; i < 20000; i++ {
		base, edited, current := pick(), pick(), pick()
		want := mergeDraftTags(base, edited, current)
		got := services.MergeDraftTags(base, edited, current)
		if !slices.Equal(want, got) {
			t.Fatalf("第 %d 组不一致：base=%q edited=%q current=%q\n主包：%q\nservices：%q", i, base, edited, current, want, got)
		}
	}
}
