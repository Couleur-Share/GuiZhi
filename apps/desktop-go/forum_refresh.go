package main

// 对应 media.ipc.ts 的 regenerateForumSummary / refreshForumDiscussion：
// 重新生成讨论总结、刷新全楼层。支持公开分页接口的站点（LINUX DO / 小众软件 / 2Libra）读取最新完整帖子。

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

func isRefreshableForumTarget(t *forumTarget) bool {
	return t != nil && (t.Platform == "linuxdo" || t.Platform == "appinn" || t.Platform == "twolibra")
}

func refreshableForumLabel(platform string) string {
	switch platform {
	case "linuxdo":
		return "LINUX DO"
	case "appinn":
		return "小众软件论坛"
	}
	return "2Libra"
}

// fetchLatestForumThread 读取最新完整帖子。LINUX DO 遇到 Cloudflare 的浏览器会话降级依赖渲染宿主，暂未接入。
func (l *Library) fetchLatestForumThread(ctx context.Context, t *forumTarget) (*forumThread, error) {
	return l.fetchThreadByPlatform(ctx, *t, l.forumPost)
}

func forumRepliesToEntries(replies []forumReply) []forumReplyEntry {
	out := make([]forumReplyEntry, len(replies))
	for i, r := range replies {
		out[i] = forumReplyEntry{Floor: r.Floor, Author: r.Author, Content: r.Content, ReplyTo: r.ReplyTo}
	}
	return out
}

var reForumReplyCountLine = regexp.MustCompile(`(?m)^(>` + fsp + `*平台：[^\n]*?·` + fsp + `*)\d+` + fsp + `*条回复([ \t]*(?:（|$))`)

// updateForumReplyCount 只改元数据行里的回复数（原正则用前瞻，Go 以捕获组还原后缀）。
func updateForumReplyCount(content string, replyCount int) string {
	loc := reForumReplyCountLine.FindStringSubmatchIndex(content)
	if loc == nil {
		return content
	}
	prefix, suffix := content[loc[2]:loc[3]], content[loc[4]:loc[5]]
	return content[:loc[0]] + prefix + strconv.Itoa(replyCount) + " 条回复" + suffix + content[loc[1]:]
}

func (l *Library) regenerateForumSummary(ctx context.Context, item map[string]any) map[string]any {
	content := strField(item, "content")
	baseContent := content
	body := splitForumNoteSections(content).Body
	replies := parseForumReplies(content)
	var target *forumTarget
	if uri := strField(item, "sourceUri"); uri != "" {
		target = detectForumPlatform(uri)
	}
	if isRefreshableForumTarget(target) {
		thread, err := l.fetchLatestForumThread(ctx, target)
		if err != nil {
			return mediaFail(fmt.Sprintf("无法获取完整的%s讨论：%s", refreshableForumLabel(target.Platform), err.Error()))
		}
		body = thread.Content
		replies = forumRepliesToEntries(thread.Replies)
		baseContent = updateForumReplyCount(replaceForumRepliesSection(content, replies), thread.ReplyCount)
	}
	if len(replies) == 0 {
		return mediaFail("该条目没有可用于总结的讨论内容")
	}
	cfg := l.forumSummaryConfig(l.forumPost)
	if cfg == nil {
		return mediaResult(false, "notConfigured", true, "error", "未配置可用的文本模型")
	}
	material := make([]forumSummaryReply, len(replies))
	for i, r := range replies {
		material[i] = forumSummaryReply{Floor: r.Floor, Author: r.Author, Content: r.Content}
	}
	title := strField(item, "title")
	// 只喂主楼，别把上一版总结和回复原文重复塞进提示词
	res, err := l.forumSummarize(ctx, l.forumPost, forumSummaryInput{Title: title, Content: body, Replies: material}, *cfg)
	if err != nil {
		return mediaFail(err.Error())
	}
	if res == nil {
		return mediaFail("模型未返回有效的讨论总结")
	}
	next := upsertForumSummarySection(baseContent, res.Summary)
	in := UpdateInput{Content: optStr(next)}
	// 标题仍是那个说不清内容的原标题时，顺手换成模型拟的
	if res.HasTitle && res.Title != "" && res.Title != title {
		in.Content = optStr(appendOriginalTitleNote(next, title))
		in.Title = optStr(res.Title)
	}
	updated, err := l.knowledgeUpdate(ctx, strField(item, "id"), in)
	if err != nil {
		return mediaFail(err.Error())
	}
	fmt.Printf("[import] 讨论总结重新生成完成（item=%s，%d 字）\n", strField(item, "id"), utf16Len(res.Summary))
	out := mediaResult(true)
	if updated != nil {
		out["item"] = updated
	}
	return out
}

// refreshForumDiscussion 抓取支持站点的最新全楼层，替换讨论段并把旧总结标成过期。
func (l *Library) refreshForumDiscussion(ctx context.Context, item map[string]any) map[string]any {
	var target *forumTarget
	if uri := strField(item, "sourceUri"); uri != "" {
		target = detectForumPlatform(uri)
	}
	if strField(item, "itemType") != "forum" || !isRefreshableForumTarget(target) {
		return mediaFail("仅 LINUX DO、小众软件论坛与 2Libra 条目支持刷新讨论")
	}
	label := refreshableForumLabel(target.Platform)
	thread, err := l.fetchLatestForumThread(ctx, target)
	if err != nil {
		return mediaFail(fmt.Sprintf("刷新%s讨论失败：%s", label, err.Error()))
	}
	next := updateForumReplyCount(replaceForumRepliesSection(strField(item, "content"), forumRepliesToEntries(thread.Replies)), thread.ReplyCount)
	updated, err := l.knowledgeUpdate(ctx, strField(item, "id"), UpdateInput{Content: optStr(next)})
	if err != nil {
		return mediaFail(fmt.Sprintf("刷新%s讨论失败：%s", label, err.Error()))
	}
	if updated == nil {
		return mediaFail("刷新后的讨论写入失败")
	}
	fmt.Printf("[import] %s讨论刷新完成（item=%s，%d 层）\n", label, strField(item, "id"), len(thread.Replies))
	return mediaResult(true, "item", updated)
}
