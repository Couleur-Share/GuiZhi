package library

import (
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 详情入口的可用性判断（detailcaps.go）：每条规则各有正反两面，数据取自渲染层 ContentPanel / use-media-actions / ItemDetail 的判断。

func capsItem(itemType, content, transcript, sourceURI string, deletedAt int64) services.LibraryItem {
	return services.LibraryItem{ItemType: itemType, Content: content, Transcript: transcript, SourceURI: sourceURI, DeletedAt: deletedAt}
}

// 标签页：笔记只有正文；有文字稿或能转写才有「文字稿」页（回收站里不摆）；图片有「图片」页，有识别结果才有「图中文字」页；
// 论坛按小节摆「讨论总结」「逐楼回复」。
func TestTabsFollowTheItem(t *testing.T) {
	forum := "## 讨论总结\n总结\n\n## 正文\n主楼\n\n## 讨论\n**1楼 · 甲**\n回复"
	cases := []struct {
		name string
		item services.LibraryItem
		want ItemCaps // 只比较标签页相关字段
	}{
		{"普通笔记只有正文", capsItem("note", "正文", "", "", 0), ItemCaps{}},
		{"有文字稿就有文字稿页", capsItem("audio", "x", "  稿  ", "", 0), ItemCaps{ShowTranscriptTab: true, IsMediaItem: true, Transcript: "稿"}},
		{"回收站里不摆文字稿页", capsItem("audio", "x", "稿", "", 5), ItemCaps{Trashed: true, IsMediaItem: true, Transcript: "稿"}},
		{"本地视频能转写", capsItem("video", "local-video://a.mp4\n正文", "", "", 0), ItemCaps{ShowTranscriptTab: true, IsMediaItem: true, CanTranscribe: true}},
		{"在线视频能转写", capsItem("video", "正文", "", "https://www.bilibili.com/video/BV1xx", 0), ItemCaps{ShowTranscriptTab: true, IsMediaItem: true, CanTranscribe: true, IsOnlineVideo: true}},
		{"回收站里的视频仍具备转写条件，只是不摆页", capsItem("video", "local-video://a.mp4", "", "", 5), ItemCaps{Trashed: true, IsMediaItem: true, CanTranscribe: true}},
		{"图文条目的来源是视频站也不能转写", capsItem("image", "正文", "", "https://www.bilibili.com/video/BV1xx", 0), ItemCaps{ShowImagesTab: true}},
		{"图片有识别结果", capsItem("image", "文案\n\n## 图中文字\n识别出的字", "", "", 0), ItemCaps{ShowImagesTab: true, ShowRecognizedTab: true}},
		{"论坛按小节摆页", capsItem("forum", forum, "", "", 0), ItemCaps{ShowForumSummaryTab: true, ShowRepliesTab: true}},
		{"论坛没有总结与回复", capsItem("forum", "## 正文\n只有主楼", "", "", 0), ItemCaps{}},
		{"已有本地视频引用就不算在线视频", capsItem("video", "local-video://a.mp4", "", "https://www.bilibili.com/video/BV1xx", 0), ItemCaps{ShowTranscriptTab: true, IsMediaItem: true, CanTranscribe: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveCaps(c.item)
			pick := func(x ItemCaps) ItemCaps {
				return ItemCaps{
					Trashed: x.Trashed, IsMediaItem: x.IsMediaItem, Transcript: x.Transcript,
					ShowTranscriptTab: x.ShowTranscriptTab, ShowImagesTab: x.ShowImagesTab, ShowRecognizedTab: x.ShowRecognizedTab,
					ShowForumSummaryTab: x.ShowForumSummaryTab, ShowRepliesTab: x.ShowRepliesTab,
					CanTranscribe: x.CanTranscribe, IsOnlineVideo: x.IsOnlineVideo,
				}
			}
			if pick(got) != pick(c.want) {
				t.Fatalf("得到 %+v，期望 %+v", pick(got), pick(c.want))
			}
		})
	}
}

// 总结：论坛要有逐楼回复、音视频要有文字稿才能生成；已有总结小节则按钮变「重新生成」；文案按类型分。
func TestSummaryAvailabilityAndLabel(t *testing.T) {
	cases := []struct {
		name      string
		item      services.LibraryItem
		available bool
		has       bool
		label     string
	}{
		{"视频有文字稿没总结", capsItem("video", "正文", "稿", "", 0), true, false, locales.LibraryMediaSummaryGenerateVideo()},
		{"视频没文字稿", capsItem("video", "正文", " \n", "", 0), false, false, locales.LibraryMediaSummaryGenerateVideo()},
		{"视频已有总结", capsItem("video", "正文\n## 视频总结\n要点", "稿", "", 0), true, true, locales.LibraryMediaSummaryRegenerateVideo()},
		{"音频已有总结", capsItem("audio", "## 音频总结\n要点", "稿", "", 0), true, true, locales.LibraryMediaSummaryRegenerateAudio()},
		{"音频没总结", capsItem("audio", "正文", "稿", "", 0), true, false, locales.LibraryMediaSummaryGenerateAudio()},
		{"音频里的视频总结标题也算已有", capsItem("audio", "## 视频总结\n要点", "稿", "", 0), true, true, locales.LibraryMediaSummaryRegenerateAudio()},
		{"论坛有回复没总结", capsItem("forum", "## 正文\n主楼\n\n## 讨论\n**1楼 · 甲**\n回复", "", "", 0), true, false, locales.LibraryForumSummaryGenerate()},
		{"论坛已有总结", capsItem("forum", "## 讨论总结\n总结\n\n## 讨论\n**1楼 · 甲**\n回复", "", "", 0), true, true, locales.LibraryForumSummaryRegenerate()},
		{"论坛没有回复", capsItem("forum", "## 正文\n主楼", "稿", "", 0), false, false, locales.LibraryForumSummaryGenerate()},
		{"笔记也按视频文案", capsItem("note", "正文", "", "", 0), false, false, locales.LibraryMediaSummaryGenerateVideo()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveCaps(c.item)
			if got.SummaryAvailable != c.available || got.HasSummary != c.has || got.SummaryLabel != c.label {
				t.Fatalf("得到 可用=%v 已有=%v 文案=%q，期望 %v %v %q", got.SummaryAvailable, got.HasSummary, got.SummaryLabel, c.available, c.has, c.label)
			}
		})
	}
}

// 刷新讨论只对论坛条目、且来源是 Linux.do / 小众软件论坛 / 2libra 的帖子开放。
func TestRefreshAvailability(t *testing.T) {
	cases := []struct {
		name string
		item services.LibraryItem
		want bool
	}{
		{"Linux.do 帖子", capsItem("forum", "", "", "https://linux.do/t/topic/12345", 0), true},
		{"小众软件论坛帖子", capsItem("forum", "", "", "https://meta.appinn.net/t/topic/9", 0), true},
		{"2libra 帖子", capsItem("forum", "", "", "https://2libra.com/post/tech/AbC_123", 0), true},
		{"V2EX 不支持刷新", capsItem("forum", "", "", "https://www.v2ex.com/t/12345", 0), false},
		{"Linux.do 非帖子页", capsItem("forum", "", "", "https://linux.do/about", 0), false},
		{"不是论坛条目", capsItem("note", "", "", "https://linux.do/t/topic/12345", 0), false},
		{"没有来源", capsItem("forum", "", "", "", 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deriveCaps(c.item).RefreshAvailable; got != c.want {
				t.Fatalf("得到 %v，期望 %v", got, c.want)
			}
		})
	}
}

// 来源评论只对抖音与小红书的 https 链接开放，回收站里不开放；公众号来源另有快照切换。
func TestCommentsAndWechatSupport(t *testing.T) {
	cases := []struct {
		name     string
		item     services.LibraryItem
		comments bool
		wechat   bool
	}{
		{"抖音", capsItem("video", "", "", "https://www.douyin.com/video/1", 0), true, false},
		{"小红书", capsItem("note", "", "", "https://www.xiaohongshu.com/explore/1", 0), true, false},
		{"回收站里的抖音", capsItem("video", "", "", "https://www.douyin.com/video/1", 5), false, false},
		{"http 链接", capsItem("video", "", "", "http://www.douyin.com/video/1", 0), false, false},
		{"别的站", capsItem("note", "", "", "https://example.com/", 0), false, false},
		{"公众号", capsItem("note", "", "", "https://mp.weixin.qq.com/s/abc", 0), false, true},
		{"没有来源", capsItem("note", "", "", "", 0), false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveCaps(c.item)
			if got.CommentsSupported != c.comments || got.IsWechat != c.wechat {
				t.Fatalf("得到 评论=%v 公众号=%v，期望 %v %v", got.CommentsSupported, got.IsWechat, c.comments, c.wechat)
			}
		})
	}
}

// 空白笔记：没删除、内容去掉 JS 空白（含 U+FEFF）后为空才是；提问入口则只看笔记与内容，不看回收站。
func TestBlankNoteAndAskAvailability(t *testing.T) {
	cases := []struct {
		name  string
		item  services.LibraryItem
		blank bool
		ask   bool
	}{
		{"空内容", capsItem("note", "", "", "", 0), true, false},
		{"只有空白与 BOM", capsItem("note", " \n\u00a0\ufeff\u3000", "", "", 0), true, false},
		{"有内容", capsItem("note", "字", "", "", 0), false, true},
		{"回收站里的空白笔记不进编辑态，仍不摆提问", capsItem("note", "", "", "", 5), false, false},
		{"非笔记的空内容不算空白笔记", capsItem("video", "", "", "", 0), false, true},
		{"U+0085 不是 JS 空白", capsItem("note", "\u0085", "", "", 0), false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveCaps(c.item)
			if got.IsBlankNote != c.blank || got.AskAvailable != c.ask {
				t.Fatalf("得到 空白=%v 提问=%v，期望 %v %v", got.IsBlankNote, got.AskAvailable, c.blank, c.ask)
			}
		})
	}
}
