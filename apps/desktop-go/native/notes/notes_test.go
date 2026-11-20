package notes

import (
	"reflect"
	"strings"
	"testing"
)

// 用例取自渲染层的单测（apps/desktop/tests/unit/import 下的 forum-note、image-note、video-meta），
// 逐条保持同样的输入与断言；与主包同名实现的逐字节一致由主包里的对拍测试（notes_parity_test.go）守住。

// forumContent 是采集端 buildForumEntry 的现行输出形态。
var forumContent = strings.Join([]string{
	"> 平台：V2EX · 作者：yitwotre · 节点：问与答 · 107 条回复",
	"> 发布：2026-07-16",
	"",
	"## 讨论总结",
	"",
	"**ZeroTier**",
	"- 多人推荐，延迟 4ms",
	"",
	"## 正文",
	"",
	"家里的设备有群辉 nas。",
	"",
	"## 讨论（2 条）",
	"",
	"**1 楼 · wowo243**",
	"",
	"zerotier 试试",
	"",
	"**2 楼 · onlychen**",
	"",
	"搞个网关 sslvpn 拨回去。",
}, "\n")

func TestSplitForumSections(t *testing.T) {
	s := SplitForumSections(forumContent)
	if s.Summary != "**ZeroTier**\n- 多人推荐，延迟 4ms" || s.Body != "家里的设备有群辉 nas。" {
		t.Fatalf("总结或主楼不对：%+v", s)
	}
	if !strings.Contains(s.Replies, "**1 楼 · wowo243**") || !strings.Contains(s.Replies, "搞个网关 sslvpn 拨回去。") {
		t.Fatalf("回复段不对：%q", s.Replies)
	}
	for _, part := range []string{s.Summary, s.Body, s.Replies} {
		if strings.Contains(part, "平台：V2EX") || strings.Contains(part, "发布：2026-07-16") {
			t.Fatalf("段里不该有元数据引用块：%q", part)
		}
	}
	if strings.Contains(s.Summary, "楼 ·") || strings.Contains(s.Replies, "ZeroTier") {
		t.Fatal("「## 讨论总结」不该被当成回复小节的标题")
	}
}

func TestSplitForumSectionsKeepsContentWithoutHeadings(t *testing.T) {
	content := strings.Join([]string{
		"> 平台：V2EX · 作者：someone · 1 条回复", "> 发布：2026-07-16", "",
		"> 未配置文本模型，讨论总结未生成；原始讨论已完整入库。", "",
		"## 正文", "", "主楼内容", "", "## 讨论（1 条）", "", "**1 楼 · a**", "", "回复内容",
	}, "\n")
	s := SplitForumSections(content)
	if s.Summary != "" || !strings.Contains(s.Body, "未配置文本模型") || !strings.Contains(s.Body, "主楼内容") || !strings.Contains(s.Replies, "回复内容") {
		t.Fatalf("状态注记应归入主楼段：%+v", s)
	}
	if got := SplitForumSections("随手改成一段普通笔记"); got != (ForumSections{Body: "随手改成一段普通笔记"}) {
		t.Fatalf("没有小节标题时全部归入主楼：%+v", got)
	}
	if got := SplitForumSections("> 平台：V2EX · 作者：someone · 0 条回复\n> 发布：2026-07-16\n\n## 正文\n\n只有主楼"); got != (ForumSections{Body: "只有主楼"}) {
		t.Fatalf("无回复的帖子回复段为空：%+v", got)
	}
}

func TestParseForumReplies(t *testing.T) {
	want := []ForumReply{
		{Floor: 1, Author: "wowo243", Content: "zerotier 试试"},
		{Floor: 2, Author: "onlychen", Content: "搞个网关 sslvpn 拨回去。"},
	}
	if got := ParseForumReplies(forumContent); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	multi := strings.Join([]string{
		"> 平台：V2EX · 作者：a · 2 条回复", "> 发布：2026-07-16", "", "## 讨论（2 条）", "",
		"**1 楼 · hnbcinfo**", "", "easytier，我用了三年。", "", "中继换了个 12 块钱半年的 LXC。", "",
		"**2 楼 · me221**", "", "Netbird、Tailscale",
	}, "\n")
	got := ParseForumReplies(multi)
	if len(got) != 2 || got[0].Content != "easytier，我用了三年。\n\n中继换了个 12 块钱半年的 LXC。" || got[1].Content != "Netbird、Tailscale" {
		t.Fatalf("多行回复要完整保留：%+v", got)
	}
	if got := ParseForumReplies("> 平台：V2EX · 作者：a · 0 条回复\n\n## 正文\n\n只有主楼"); len(got) != 0 {
		t.Fatalf("没有讨论段时没有回复：%+v", got)
	}
}

func TestParseForumRepliesReadsReplyContext(t *testing.T) {
	nga := strings.Join([]string{
		"> 平台：NGA · 作者：a · 2040 条回复（入库保留楼主 1 条）", "> 发布：2026-07-16", "",
		"## 讨论（楼主 1 条 · 原帖共 2040 条）", "", "### 12 楼 · a", "",
		"> 回复 @lyzlegend：对方在问镜片怎么选", "", "楼主补充说明",
	}, "\n")
	want := []ForumReply{{Floor: 12, Author: "a", Content: "楼主补充说明", ReplyTo: &ForumReplyTo{Author: "lyzlegend", Snippet: "对方在问镜片怎么选"}}}
	if got := ParseForumReplies(nga); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got[0].ReplyTo)
	}
	withFloor := "### 12 楼 · 楼主\n\n> 回复 @lyzlegend（8 楼）：对方在问镜片怎么选\n\n楼主补充"
	if rt := ParseForumReplies(withFloor)[0].ReplyTo; !reflect.DeepEqual(rt, &ForumReplyTo{Author: "lyzlegend", Floor: 8, HasFloor: true, Snippet: "对方在问镜片怎么选"}) {
		t.Fatalf("带楼层的回复行：%+v", rt)
	}
	// 读库时洗净摘要里残留的 br 标签（旧条目不必重采）
	br := "### 6 楼 · a\n\n> 回复 @牧云吹雪：<br/> <br/> 想看看楼主整个眼镜是什么样的\n\n楼主回复正文"
	if got := ParseForumReplies(br)[0].ReplyTo.Snippet; got != "想看看楼主整个眼镜是什么样的" {
		t.Fatalf("snippet=%q", got)
	}
}

func TestNormalizeForumSnippet(t *testing.T) {
	if got := NormalizeForumSnippet("<br/><br/>大佬写的太好了<br/>下一段", 200); got != "大佬写的太好了 下一段" {
		t.Fatalf("got %q", got)
	}
	// 按 UTF-16 码元截断：一个 emoji 占两个码元
	if got := NormalizeForumSnippet("ab😀cd", 4); got != "ab😀" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveReplyTargetFloor(t *testing.T) {
	replies := []ForumReply{
		{Floor: 1, Author: "楼主"}, {Floor: 8, Author: "lyzlegend"}, {Floor: 12, Author: "楼主"},
		{Floor: 20, Author: "同名"}, {Floor: 21, Author: "同名"},
	}
	cases := []struct {
		to   ForumReplyTo
		want int
		ok   bool
	}{
		{ForumReplyTo{Author: "x", Floor: 8, HasFloor: true}, 8, true},
		{ForumReplyTo{Author: "x", Floor: 999, HasFloor: true}, 0, false},
		{ForumReplyTo{Author: "lyzlegend"}, 8, true},
		{ForumReplyTo{Author: "同名"}, 0, false},
		{ForumReplyTo{Author: "不存在"}, 0, false},
		{ForumReplyTo{Author: "  "}, 0, false},
	}
	for _, c := range cases {
		if got, ok := ResolveReplyTargetFloor(replies, c.to); got != c.want || ok != c.ok {
			t.Errorf("%+v: got (%d, %v)，要 (%d, %v)", c.to, got, ok, c.want, c.ok)
		}
	}
}

func TestFilterForumReplies(t *testing.T) {
	sample := []ForumReply{
		{Floor: 6, Author: "楼主", Content: "整副眼镜的侧视图在附件", ReplyTo: &ForumReplyTo{Author: "牧云吹雪", Snippet: "想看看楼主整个眼镜是什么样的"}},
		{Floor: 14, Author: "楼主", Content: "数码型不推荐一般人随便配", ReplyTo: &ForumReplyTo{Author: "FreezeLegend", Snippet: "渐进片相关的问题"}},
	}
	if got := FilterForumReplies(sample, "  "); len(got) != 2 {
		t.Fatal("空关键词返回全部")
	}
	for query, want := range map[string]int{"14 楼": 1, "14": 1, "牧云": 1, "渐进片": 1, "FREEZELEGEND": 1, "楼主": 2, "不存在": 0} {
		if got := FilterForumReplies(sample, query); len(got) != want {
			t.Errorf("%q：命中 %d 条，要 %d 条", query, len(got), want)
		}
	}
}

func TestSplitImageSections(t *testing.T) {
	content := strings.Join([]string{
		"> 平台：小红书 · 作者：a", "",
		"![图1](local-image://a/1.png) ![图2](local-image://a/2.png)", "",
		"这是文案", "",
		"## 图中文字", "", "识别出的文字",
	}, "\n")
	got := SplitImageSections(content)
	if got.Caption != "> 平台：小红书 · 作者：a\n\n这是文案" {
		t.Fatalf("文案里不该有只含图片的段落：%q", got.Caption)
	}
	if got.Recognized != "## 图中文字\n\n识别出的文字" {
		t.Fatalf("图中文字含小节标题：%q", got.Recognized)
	}
	if got := SplitImageSections("只有文案"); got != (ImageSections{Caption: "只有文案"}) {
		t.Fatalf("没有图中文字：%+v", got)
	}
}

func TestParseVideoMeta(t *testing.T) {
	content := strings.Join([]string{
		"> 平台：B站 · 作者：某人 · 时长：12:52",
		"> 原标题：原来的标题",
		"> 简介：一句话简介 · 里面有圆点",
		"> 文字稿来源：平台 AI 字幕",
		"",
		"",
		"正文第一行",
	}, "\n")
	meta := ParseVideoMeta(content)
	want := &VideoMeta{
		Platform: "B站", Author: "某人", Duration: "12:52", OriginalTitle: "原来的标题",
		Description: "一句话简介 · 里面有圆点", TranscriptSource: "平台 AI 字幕", Body: "正文第一行",
	}
	if !reflect.DeepEqual(meta, want) {
		t.Fatalf("got %+v", meta)
	}
	if ParseVideoMeta("> 普通引用\n\n正文") != nil || ParseVideoMeta("") != nil {
		t.Fatal("首行不是 `> 平台：…` 时不是元数据块")
	}
	if body, ok := VideoMetaBody("> 平台:X\n正文"); !ok || body != "正文" {
		t.Fatalf("半角冒号也认：%q %v", body, ok)
	}
	if _, ok := VideoMetaBody("没有元数据"); ok {
		t.Fatal("没有元数据块时 ok 为假")
	}
}
