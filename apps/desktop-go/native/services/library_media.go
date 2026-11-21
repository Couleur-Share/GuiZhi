package services

// M2-6d：媒体动作（转写、区分说话人、AI 排版、音视频与论坛总结、刷新论坛讨论）及其进度事件的数据形状。
// 对应的 IPC 频道：media:transcribe、media:formatTranscript、media:summarize、media:refreshForumDiscussion，
// 以及主进程推送的 media:transcribeProgress、media:formatProgress。

// 转写进度的阶段（media:transcribeProgress 的 stage）：转写 → 排版 → 总结，三步都以分钟计。
const (
	LibraryTranscribeStageTranscribing = "transcribing"
	LibraryTranscribeStageFormatting   = "formatting"
	LibraryTranscribeStageSummarizing  = "summarizing"
)

// LibraryMediaActionResult 是一次媒体动作的回复（MediaTranscribeResult），四个动作共用。
//
// 成功当且仅当 Error 为空且 NotConfigured 为假：此时 Item 是写回后的完整条目，界面据此刷新。
// 失败时 Item 为 nil，Error 是可展示、可复制的原因；NotConfigured 为真表示缺少所需的模型配置（界面应引导去设置）。
// 只做了一部分也算成功（结果已落库）：Warning 非空时界面要提示「部分完成」而不是报完整成功。
// 调用本身失败（处理器报错、解码失败）作为 error 返回，不放进这里。
type LibraryMediaActionResult struct {
	Item          *LibraryItem
	Warning       string
	NotConfigured bool
	Error         string
}

// OK 报告动作是否成功落库。
func (r LibraryMediaActionResult) OK() bool {
	return r.Error == "" && !r.NotConfigured && r.Item != nil
}

// LibraryTranscribeProgress 是转写进行中每秒一次的进度（TranscribeProgress）。
// 没有百分比是有意的：转写接口不回报进度，只有已用时长；StalledMs 仅本地引擎转写阶段有，表示距上次心跳多久。
type LibraryTranscribeProgress struct {
	ItemID    string `json:"itemId"`
	Stage     string `json:"stage"`
	ElapsedMs int64  `json:"elapsedMs"`
	StalledMs *int64 `json:"stalledMs,omitempty"`
}

// LibraryTranscriptFormatProgress 是文字稿排版的逐块进度（TranscriptFormatProgress）：已完成块数与总块数。
type LibraryTranscriptFormatProgress struct {
	ItemID  string `json:"itemId"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}
