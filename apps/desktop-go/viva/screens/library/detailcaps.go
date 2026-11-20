package library

import (
	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 详情上各个入口此刻能不能用（渲染层 ContentPanel 的标签页判断、use-media-actions 的转写 / 总结 / 刷新讨论判断、
// SourceCommentsContext 的 supported、ItemDetail 的提问入口）。纯函数：只读条目，不读 Store。
// 判断里的 trim 一律是 JS 的（jsTrim：去 U+FEFF、不去 U+0085），与渲染层对同一条正文的结论一致。

// ItemCaps 是一条详情的能力（派生值，随条目重新算）。
type ItemCaps struct {
	// Trashed 条目在回收站里（渲染层 deletedAt != null）：只读，且不摆转写入口。
	Trashed bool
	// 面板的标签页（渲染层 availableTabs；正文永远有）。
	ShowTranscriptTab, ShowImagesTab, ShowRecognizedTab, ShowForumSummaryTab, ShowRepliesTab bool
	// IsMediaItem 是音频或视频条目。
	IsMediaItem bool
	// Transcript 是去掉首尾空白的文字稿。
	Transcript string
	// CanTranscribe 具备转写条件：本地视频资产，或能重新下载音轨的在线视频；
	// 图文条目的来源即便是抖音也没有音轨，不能据此提供转写入口（渲染层 isOnlineVideo 排除 image）。
	CanTranscribe, IsOnlineVideo bool
	// SummaryAvailable 能生成总结：论坛要有逐楼回复，音视频要有文字稿；HasSummary 已有总结小节；
	// SummaryLabel 是按钮文字（已有则是「重新生成……」）。
	SummaryAvailable, HasSummary bool
	SummaryLabel                 string
	// RefreshAvailable 论坛条目的来源是支持刷新讨论的站点（Linux.do、小众软件论坛、2libra）。
	RefreshAvailable bool
	// CommentsSupported 能采集来源评论：只开放抖音与小红书的 https 链接，回收站里的不开放。
	CommentsSupported bool
	// IsWechat 来源是公众号文章（可在网页快照与原文之间切换）。
	IsWechat bool
	// IsBlankNote 空白笔记（没删除、没有内容）：没有可阅读的内容，打开时直接进编辑态（渲染层 ContentPanel）。
	IsBlankNote bool
	// AskAvailable 头部摆不摆「提问」入口：空白笔记没有可提问的正文，不摆（渲染层 ItemDetail 的 onToggleAsk；
	// 与 IsBlankNote 不同，这里不看回收站）。
	AskAvailable bool
}

// deriveCaps 算出一条的能力。
func deriveCaps(item services.LibraryItem) ItemCaps {
	trashed := item.DeletedAt != 0
	c := ItemCaps{
		Trashed:     trashed,
		IsMediaItem: item.ItemType == services.LibraryTypeAudio || item.ItemType == services.LibraryTypeVideo,
		Transcript:  jsTrim(item.Transcript),
	}

	_, hasLocalVideo := notes.LocalAssetRef(item.Content, notes.LocalVideo)
	c.IsOnlineVideo = item.ItemType != services.LibraryTypeImage && !hasLocalVideo &&
		services.DetectVideoPlatform(jsTrim(item.SourceURI)) != ""
	c.CanTranscribe = hasLocalVideo || c.IsOnlineVideo
	c.ShowTranscriptTab = !trashed && (c.CanTranscribe || c.Transcript != "")

	if item.ItemType == services.LibraryTypeImage {
		c.ShowImagesTab = true
		c.ShowRecognizedTab = notes.SplitImageSections(item.Content).Recognized != ""
	}

	isForum := item.ItemType == services.LibraryTypeForum
	var forum notes.ForumSections
	if isForum {
		forum = notes.SplitForumSections(item.Content)
		c.ShowForumSummaryTab = forum.Summary != ""
		c.ShowRepliesTab = forum.Replies != ""
	}
	if isForum {
		c.HasSummary = forum.Summary != ""
		c.SummaryAvailable = forum.Replies != ""
	} else {
		c.HasSummary = notes.HasMediaSummarySection(item.Content)
		c.SummaryAvailable = c.Transcript != ""
	}
	c.SummaryLabel = summaryLabel(isForum, item.ItemType == services.LibraryTypeAudio, c.HasSummary)

	if isForum && item.SourceURI != "" {
		switch services.DetectForumPlatform(item.SourceURI) {
		case "linuxdo", "appinn", "twolibra":
			c.RefreshAvailable = true
		}
	}
	c.CommentsSupported = !trashed && services.DetectSourceCommentsPlatform(item.SourceURI) != ""
	if p, ok := services.ResolveSourcePlatform("url", item.SourceURI); ok && p == "wechat" {
		c.IsWechat = true
	}
	blank := item.ItemType == services.LibraryTypeNote && jsTrim(item.Content) == ""
	c.IsBlankNote = blank && !trashed
	c.AskAvailable = !blank
	return c
}

// summaryLabel 是总结按钮的文字（渲染层 useMediaSummaryAction 的 label）。
func summaryLabel(isForum, isAudio, hasSummary bool) string {
	switch {
	case isForum && hasSummary:
		return locales.LibraryForumSummaryRegenerate()
	case isForum:
		return locales.LibraryForumSummaryGenerate()
	case hasSummary && isAudio:
		return locales.LibraryMediaSummaryRegenerateAudio()
	case hasSummary:
		return locales.LibraryMediaSummaryRegenerateVideo()
	case isAudio:
		return locales.LibraryMediaSummaryGenerateAudio()
	}
	return locales.LibraryMediaSummaryGenerateVideo()
}
