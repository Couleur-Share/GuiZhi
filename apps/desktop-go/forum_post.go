package main

// 对应 services/import/forum-post.ts：论坛帖子的条目组装（元数据引用块 + 讨论总结 + 主楼正文 + 回复）。
// 三个小节标题同时是详情页分段锚点，与 forum_note.go 保持同步。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var forumPlatformLabels = map[string]string{
	"v2ex": "V2EX", "nga": "NGA", "linuxdo": "LINUX DO", "nodeseek": "NodeSeek", "appinn": "小众软件", "twolibra": "2Libra",
}

const forumSummaryErrorMaxLength = 120 // 失败原因写进正文时的截断长度

// forumPostDeps 汇总可注入依赖；零值即生产行为。
type forumPostDeps struct {
	fetchThread            func(ctx context.Context, t forumTarget) (*forumThread, error)
	fetchNodeseek          func(ctx context.Context, topicID string) (*forumThread, error)
	fetchAuthenticatedJSON func(ctx context.Context, url string) (any, error)
	fetch                  forumDeps // 抓取层注入（测试用）
	nga                    ngaDeps   // NGA 抓取层注入（测试用）
	getSummaryConfig       func() *aiClientConfig
	summarize              func(ctx context.Context, in forumSummaryInput, cfg aiClientConfig) (*forumSummaryResult, error)
	onStage                func(stage string)
}

func (l *Library) fetchThreadByPlatform(ctx context.Context, t forumTarget, deps forumPostDeps) (*forumThread, error) {
	fd := deps.fetch
	if fd.fetchAuthenticatedJSON == nil {
		fd.fetchAuthenticatedJSON = deps.fetchAuthenticatedJSON
	}
	switch t.Platform {
	case "nodeseek":
		if deps.fetchNodeseek == nil {
			return nil, errors.New("NodeSeek 浏览器采集服务未就绪")
		}
		return deps.fetchNodeseek(ctx, t.TopicID)
	case "v2ex":
		return l.fetchV2exThread(ctx, t.TopicID, fd)
	case "nga":
		return l.fetchNgaThread(ctx, t.TopicID, deps.nga)
	case "linuxdo":
		return l.fetchDiscourseThread(ctx, t.TopicID, linuxdoSite, fd)
	case "appinn":
		fd.fetchAuthenticatedJSON = nil // 小众软件没有登录降级
		return l.fetchDiscourseThread(ctx, t.TopicID, appinnSite, fd)
	case "twolibra":
		return l.fetchTwolibraThread(ctx, t.TopicID, fd)
	}
	return nil, fmt.Errorf("暂不支持的论坛: %s", t.Platform)
}

// formatForumDate 本地时区的 YYYY-MM-DD。
func formatForumDate(ts int64) string { return time.UnixMilli(ts).Local().Format("2006-01-02") }

func forumRetentionNote(t *forumThread) string {
	if t.ReplyRetention == "op-only" {
		return "条目讨论区仅保留楼主回复，完整楼层见原帖链接。"
	}
	if t.WarningReason != "" {
		return "讨论存在缺失：" + t.WarningReason + "。"
	}
	return "原始讨论已完整入库。"
}

// forumSummaryMaterial 交给模型的讨论素材：NGA 用采样页，其余用入库回复。
func forumSummaryMaterial(t *forumThread) []forumSummaryReply {
	src := t.Replies
	if t.SummaryReplies != nil {
		src = t.SummaryReplies
	}
	out := make([]forumSummaryReply, len(src))
	for i, r := range src {
		out[i] = forumSummaryReply{Floor: r.Floor, Author: r.Author, Content: r.Content}
	}
	return out
}

// buildForumMetaBlock 元数据引用块；首字段沿用「平台：」，两行之间只能是换行不能空行。
func buildForumMetaBlock(t *forumThread) string {
	facts := []string{"平台：" + forumPlatformLabels[t.Platform]}
	if t.Author != "" {
		facts = append(facts, "作者："+t.Author)
	}
	if t.Node != "" {
		facts = append(facts, "节点："+t.Node)
	}
	if t.ReplyRetention == "op-only" {
		facts = append(facts, fmt.Sprintf("%d 条回复（入库保留楼主 %d 条）", t.ReplyCount, len(t.Replies)))
	} else {
		facts = append(facts, fmt.Sprintf("%d 条回复", t.ReplyCount))
	}
	return "> " + strings.Join(facts, " · ") + "\n> 发布：" + formatForumDate(t.CreatedAt)
}

func buildForumRepliesSection(t *forumThread) []string {
	if len(t.Replies) == 0 {
		return nil
	}
	heading := fmt.Sprintf("%s（%d 条）", forumRepliesHeading, len(t.Replies))
	if t.ReplyRetention == "op-only" {
		heading = fmt.Sprintf("%s（楼主 %d 条 · 原帖共 %d 条）", forumRepliesHeading, len(t.Replies), t.ReplyCount)
	}
	parts := []string{heading}
	for _, r := range t.Replies {
		parts = append(parts, formatForumReplyBlock(r.Floor, r.Author, r.Content, r.ReplyTo))
	}
	return parts
}

type forumSummarySection struct {
	Parts    []string
	Title    string
	HasTitle bool
}

// buildForumSummarySection 未配置模型或生成失败都不阻断采集，改为在正文里如实交代。
func (l *Library) buildForumSummarySection(ctx context.Context, t *forumThread, deps forumPostDeps) (forumSummarySection, error) {
	material := forumSummaryMaterial(t)
	if len(material) == 0 {
		return forumSummarySection{}, nil
	}
	note := forumRetentionNote(t)
	cfg := l.forumSummaryConfig(deps)
	if cfg == nil {
		return forumSummarySection{Parts: []string{"> 未配置文本模型，讨论总结未生成；" + note}}, nil
	}
	if deps.onStage != nil {
		deps.onStage("summarizing")
	}
	in := forumSummaryInput{Title: t.Title, Content: t.Content, Replies: material}
	res, err := l.forumSummarize(ctx, deps, in, *cfg)
	if err != nil {
		if ctx.Err() != nil {
			return forumSummarySection{}, errors.New(forumCanceledMsg)
		}
		logAppErrorf("import", "论坛讨论总结失败", "%v", err)
		reason := utf16Slice(err.Error(), forumSummaryErrorMaxLength)
		return forumSummarySection{Parts: []string{"> 讨论总结生成失败：" + reason + "。" + note}}, nil
	}
	if res == nil {
		return forumSummarySection{}, nil
	}
	return forumSummarySection{Parts: []string{forumSummaryHeading, res.Summary}, Title: res.Title, HasTitle: res.HasTitle}, nil
}

// extractForumPost 抓取论坛帖子并组装为知识条目。
// 抓取失败返回带 DegradedReason 的空壳，由队列标记任务失败（不入库）；取消则返回 error。
func (l *Library) extractForumPost(ctx context.Context, target forumTarget, deps forumPostDeps) (*extractedContent, error) {
	// 抓取主楼与回复要串多个请求，先把阶段报出去，否则界面只显示笼统的「抓取中」
	if deps.onStage != nil {
		deps.onStage("forum-replies")
	}
	var thread *forumThread
	var err error
	if deps.fetchThread != nil {
		thread, err = deps.fetchThread(ctx, target)
	} else {
		thread, err = l.fetchThreadByPlatform(ctx, target, deps)
	}
	if err != nil {
		if err.Error() == forumCanceledMsg {
			return nil, err
		}
		return &extractedContent{ItemType: "forum", DegradedReason: "论坛帖子抓取失败：" + err.Error()}, nil
	}

	section, err := l.buildForumSummarySection(ctx, thread, deps)
	if err != nil {
		return nil, err
	}
	parts := append([]string{buildForumMetaBlock(thread)}, section.Parts...)
	if thread.Content != "" {
		parts = append(parts, forumBodyHeading, thread.Content)
	}
	parts = append(parts, buildForumRepliesSection(thread)...)

	title := thread.Title
	content := strings.Join(parts, "\n\n")
	// 原标题说不清内容时才换成 AI 拟的，原标题记进元数据引用块
	if section.HasTitle && section.Title != "" && section.Title != title {
		content = appendOriginalTitleNote(content, title)
		title = section.Title
	}
	uri := thread.WebpageURL
	return &extractedContent{
		Title: title, Content: content, ItemType: "forum", SourceURI: &uri, WarningReason: thread.WarningReason,
	}, nil
}

// forumSummaryConfig 总结用模型配置：测试注入优先，否则按 mainText → fastText 解析；未配置返回 nil。
func (l *Library) forumSummaryConfig(deps forumPostDeps) *aiClientConfig {
	if deps.getSummaryConfig != nil {
		return deps.getSummaryConfig()
	}
	return l.resolveMediaSummaryConfig()
}

// forumSummarize 生成讨论总结：测试注入优先，否则走真实模型调用。
func (l *Library) forumSummarize(ctx context.Context, deps forumPostDeps, in forumSummaryInput, cfg aiClientConfig) (*forumSummaryResult, error) {
	if deps.summarize != nil {
		return deps.summarize(ctx, in, cfg)
	}
	return l.generateForumSummary(ctx, in, cfg, nil)
}
