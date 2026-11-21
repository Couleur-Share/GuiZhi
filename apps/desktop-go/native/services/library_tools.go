package services

import "encoding/json"

// 文章工具侧栏的数据形状（渲染层 SourceCommentsContext 与 WebSourceVersions）。
// 对应的 IPC 频道：platformCapture:listComments、platformCapture:refreshComments、web:versions、web:adopt。

// 采集评论的条数档位（渲染层评论卡片「评论采集数量」下拉的三个选项）；处理器只接受这三个。
var LibraryCommentLimits = []int{10, 20, 50}

// LibraryCommentLimitDefault 是评论采集数量的默认档位（渲染层 useState(20)）。
const LibraryCommentLimitDefault = 20

// LibrarySourceComment 是一条来源评论（packages/shared 的 SourceComment）。数据库里的 NULL 解码成零值。
type LibrarySourceComment struct {
	ID         string `json:"id"`
	ItemID     string `json:"itemId"`
	Platform   string `json:"platform"`
	ExternalID string `json:"externalId"`
	// AuthorName 为空时界面写「未知用户」。
	AuthorName string `json:"authorName"`
	Content    string `json:"content"`
	LikeCount  int64  `json:"likeCount"`
	// PublishedAt 是评论发布时间（毫秒）；nil 表示平台没给。
	PublishedAt *int64 `json:"publishedAt"`
	CapturedAt  int64  `json:"capturedAt"`
}

// LibraryWebVersion 是一次网页采集留下的原文版本（packages/shared 的 WebSourceVersion）。
type LibraryWebVersion struct {
	ID            string `json:"id"`
	ItemID        string `json:"itemId"`
	SourceURL     string `json:"sourceUrl"`
	Title         string `json:"title"`
	Markdown      string `json:"markdown"`
	ContentHash   string `json:"contentHash"`
	CapturedAt    int64  `json:"capturedAt"`
	EngineVersion string `json:"engineVersion"`
	// Complete 为假表示这次采集不完整，界面不让采用。
	Complete bool `json:"complete"`
	// Kind 是 "remote"（远程采集）或 "local"（本地快照）。
	Kind string `json:"kind"`
	// Snapshot 是公众号网页快照（经净化的对象）；原生界面只关心它在不在（快照面板随 M2-5b 迁入），所以不解码内容。
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// WebVersionLocal 与 WebVersionRemote 是 LibraryWebVersion.Kind 的两个取值。
const (
	WebVersionLocal  = "local"
	WebVersionRemote = "remote"
)

// HasSnapshot 报告这个版本带不带网页快照。
func (v LibraryWebVersion) HasSnapshot() bool {
	return len(v.Snapshot) > 0 && string(v.Snapshot) != "null"
}

// LibraryWebVersions 是 web:versions 的数据：版本列表（新的在前）与比较基线（此刻的正文、标题与它们的哈希）。
type LibraryWebVersions struct {
	Versions    []LibraryWebVersion `json:"versions"`
	Content     string              `json:"content"`
	Title       string              `json:"title"`
	ContentHash string              `json:"contentHash"`
	// SummaryStale 为真表示摘要是按旧正文写的，采用新版本后需要重新整理。
	SummaryStale bool `json:"summaryStale"`
	// PendingVersion 为真表示有尚未采用的新版本可比较。
	PendingVersion bool `json:"pendingVersion"`
}

// LibraryAdoptWebVersionInput 是采用某个原文版本的请求（AdoptWebVersionInput）：
// 期望的正文哈希与标题是比较时的基线，服务端据此做乐观锁。
type LibraryAdoptWebVersionInput struct {
	ItemID              string `json:"itemId"`
	VersionID           string `json:"versionId"`
	ExpectedContentHash string `json:"expectedContentHash"`
	ExpectedTitle       string `json:"expectedTitle"`
}
