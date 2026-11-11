package main

// 对应 services/import/v2ex.ts：走官方 v1 只读接口（topics/show.json + replies/show.json），不解析 HTML。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const v2exAPIBase = "https://www.v2ex.com/api"

// v2exMillis 对应 toMillis(seconds)：秒级时间戳转毫秒；缺失或非法回退当前时间。
func v2exMillis(v any) int64 {
	if n, ok := fjNum(v); ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n > 0 {
		return int64(n * 1000)
	}
	return time.Now().UnixMilli()
}

// describeV2exError 对应 describeFetchError：把接口错误翻译成用户能据此行动的说法。
func describeV2exError(err error) string {
	msg := forumErrMsg(err)
	switch {
	case msg == forumCanceledMsg:
		return msg
	case strings.Contains(msg, "HTTP 403") || strings.Contains(msg, "HTTP 429"):
		return "V2EX 接口访问受限（每小时 600 次上限，或触发了风控），稍后重试"
	case strings.Contains(msg, "HTTP 404"):
		return "帖子不存在或已被删除"
	}
	if m := reHTTP5xxCapture.FindStringSubmatch(msg); m != nil {
		return "V2EX 服务器暂时无响应（HTTP " + m[1] + "），已自动重试仍未成功，稍后再试"
	}
	switch msg {
	case "请求超时":
		return "连接 V2EX 超时，已自动重试仍未成功，稍后再试"
	case "连接被中断":
		return "与 V2EX 的连接被中断，已自动重试仍未成功，稍后再试"
	}
	return msg
}

// v2exRequest 对应 request：发起请求，瞬时故障退避重试；错误一律翻译后再抛给上层。
func (l *Library) v2exRequest(ctx context.Context, rawURL string, deps forumDeps) (any, error) {
	get := l.forumGetJSON(deps)
	delays := deps.delays()
	var last error
	for attempt := 0; attempt <= len(delays); attempt++ {
		if attempt > 0 {
			if err := forumWait(ctx, delays[attempt-1]); err != nil {
				return nil, err
			}
		}
		v, err := get(ctx, rawURL)
		if err == nil {
			return v, nil
		}
		msg := err.Error()
		if msg == forumCanceledMsg {
			return nil, err
		}
		if !isForumTransient(msg) {
			return nil, errors.New(describeV2exError(err))
		}
		last = err
		logAppErrorf("import", "V2EX 接口请求失败", "第 %d 次请求失败（%s）", attempt+1, msg)
	}
	return nil, errors.New(describeV2exError(last))
}

// fetchV2exThread 对应 fetchV2exThread：主楼抓不到即失败，回复抓取失败只记日志。
func (l *Library) fetchV2exThread(ctx context.Context, topicID string, deps forumDeps) (*forumThread, error) {
	topics, err := l.v2exRequest(ctx, v2exAPIBase+"/topics/show.json?id="+encodeURIComponent(topicID), deps)
	if err != nil {
		return nil, err
	}
	var topic *jsObj
	if arr, ok := fjArr(topics); ok && len(arr) > 0 {
		topic = fjObj(arr[0])
	}
	if topic == nil {
		return nil, errors.New("帖子不存在、已被删除或需要登录后才能查看")
	}
	if ctx.Err() != nil {
		return nil, errors.New(forumCanceledMsg)
	}
	var replies jsArr
	fetched, err := l.v2exRequest(ctx, v2exAPIBase+"/replies/show.json?topic_id="+encodeURIComponent(topicID), deps)
	if err != nil {
		if err.Error() == forumCanceledMsg {
			return nil, err
		}
		logAppErrorf("import", "V2EX 回复抓取失败", "仅保留主楼：%v", err)
	} else if arr, ok := fjArr(fetched); ok {
		replies = arr
	}

	title := forumNormalizeText(jsGet(topic, "title"))
	if title == "" {
		title = "V2EX 帖子 " + topicID
	}
	node := fjTrim(jsOptGet(jsGet(topic, "node"), "title"))
	if node == "" {
		node = fjTrim(jsOptGet(jsGet(topic, "node"), "name"))
	}
	replyCount := len(replies)
	if n, ok := fjNum(jsGet(topic, "replies")); ok {
		replyCount = int(n)
	}
	web := fjTrim(jsGet(topic, "url"))
	if web == "" {
		web = "https://www.v2ex.com/t/" + topicID
	}
	th := &forumThread{
		Platform: "v2ex", TopicID: topicID, Title: title,
		Author:     fjTrim(jsOptGet(jsGet(topic, "member"), "username")),
		Node:       node,
		CreatedAt:  v2exMillis(jsGet(topic, "created")),
		ReplyCount: replyCount,
		Content:    forumNormalizeText(jsGet(topic, "content")),
		WebpageURL: web,
	}
	for i, r := range replies {
		content := forumNormalizeText(jsGet(r, "content"))
		if content == "" { // 纯表情/空回复没有信息量
			continue
		}
		th.Replies = append(th.Replies, forumReply{
			Floor: i + 1, Author: fjTrim(jsOptGet(jsGet(r, "member"), "username")),
			Content: content, CreatedAt: v2exMillis(jsGet(r, "created")),
		})
	}
	return th, nil
}

var _ = fmt.Sprint
