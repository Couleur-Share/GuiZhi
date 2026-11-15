package aiflow

// 证据快照（对应 packages/shared/utils/evidence.ts 的 evidenceText / captureEvidence / qualityNotice
// 与 types/evidence.ts）：回答时实际提供给模型的片段，落库前链接已脱敏。缺少此结构的老消息不得使用现正文冒充证据。
//
// URL 脱敏本身在 evidence.go 的 EvidenceURL。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// evidenceLinkRe 对应 /https?:\/\/[^\s<>"）)\]]+/g：JS 的 \s 比 RE2 的 \s 宽（含 U+00A0、全角空格等），要显式列出。
var evidenceLinkRe = regexp.MustCompile(`https?://[^` + jsSpaceClass + `<>"）)\]]+`)

// EvidenceText 对应 evidenceText：文本里的链接逐个脱敏，不可用的换成「[链接不可用]」。
func EvidenceText(text string) string {
	return evidenceLinkRe.ReplaceAllStringFunc(text, func(link string) string {
		if cleaned, ok := EvidenceURL(link); ok {
			return cleaned
		}
		return "[链接不可用]"
	})
}

// EvidenceSnapshot 对应 EvidenceSnapshot。
type EvidenceSnapshot struct {
	Version       int      `json:"version"` // 恒为 1
	Kind          string   `json:"kind"`    // item | wiki | article | web
	SourceID      *string  `json:"sourceId,omitempty"`
	SourceItemIDs []string `json:"sourceItemIds,omitempty"` // nil 表示未设置；空切片序列化为 []，见 MarshalJSON
	Title         string   `json:"title"`
	Text          string   `json:"text"`
	Fingerprint   string   `json:"fingerprint"`
	SourceVersion *string  `json:"sourceVersion,omitempty"`
	CapturedAt    int64    `json:"capturedAt"`
	ReviewStatus  string   `json:"reviewStatus"` // clear | needs_review
	ReviewReasons []string `json:"reviewReasons"`
	URL           *string  `json:"url,omitempty"`
	Cleared       *bool    `json:"cleared,omitempty"`
}

// MarshalJSON 让「未设置」与「空数组」可区分：sourceItemIds 为 nil 时省略，空切片时写 []。
func (e EvidenceSnapshot) MarshalJSON() ([]byte, error) {
	type plain EvidenceSnapshot // 去掉方法，避免递归
	aux := struct {
		plain
		SourceItemIDs *[]string `json:"sourceItemIds,omitempty"`
		ReviewReasons []string  `json:"reviewReasons"`
	}{plain: plain(e), ReviewReasons: e.ReviewReasons}
	if aux.ReviewReasons == nil {
		aux.ReviewReasons = []string{}
	}
	if e.SourceItemIDs != nil {
		aux.SourceItemIDs = &e.SourceItemIDs
	}
	return json.Marshal(aux)
}

func (e EvidenceSnapshot) clone() EvidenceSnapshot {
	out := e
	out.SourceID = copyPtr(e.SourceID)
	out.SourceVersion = copyPtr(e.SourceVersion)
	out.URL = copyPtr(e.URL)
	out.Cleared = copyPtr(e.Cleared)
	if e.SourceItemIDs != nil {
		out.SourceItemIDs = append([]string{}, e.SourceItemIDs...)
	}
	if e.ReviewReasons != nil {
		out.ReviewReasons = append([]string{}, e.ReviewReasons...)
	}
	return out
}

// EvidenceInput 是 captureEvidence 的输入：指针与 nil 切片表示未提供（undefined）。
type EvidenceInput struct {
	Kind          string
	SourceID      *string
	SourceItemIDs []string
	Title         string
	Text          string
	Fingerprint   *string
	SourceVersion *string
	CapturedAt    *int64
	URL           *string
	ReviewStatus  *string
	ReviewReasons []string
}

// CaptureEvidence 对应 captureEvidence：链接脱敏、补指纹（正文的 SHA-256）、补采集时间与复核状态。
// now 返回毫秒时间戳，为 nil 时用系统时钟。
func CaptureEvidence(in EvidenceInput, now func() int64) EvidenceSnapshot {
	text := EvidenceText(in.Text)

	var fingerprint string
	if in.Fingerprint != nil {
		fingerprint = *in.Fingerprint // `??`：空串也原样保留
	} else {
		sum := sha256.Sum256([]byte(text))
		fingerprint = hex.EncodeToString(sum[:])
	}

	var capturedAt int64
	switch {
	case in.CapturedAt != nil:
		capturedAt = *in.CapturedAt
	case now != nil:
		capturedAt = now()
	default:
		capturedAt = time.Now().UnixMilli()
	}

	var link *string
	if in.URL != nil {
		if cleaned, ok := EvidenceURL(*in.URL); ok {
			link = &cleaned
		}
	}

	status := "clear"
	if in.ReviewStatus != nil {
		status = *in.ReviewStatus
	}
	reasons := in.ReviewReasons
	if reasons == nil {
		reasons = []string{}
	}

	return EvidenceSnapshot{
		Version:       1,
		Kind:          in.Kind,
		SourceID:      copyPtr(in.SourceID),
		SourceItemIDs: in.SourceItemIDs,
		Title:         in.Title,
		Text:          text,
		Fingerprint:   fingerprint,
		SourceVersion: copyPtr(in.SourceVersion),
		CapturedAt:    capturedAt,
		ReviewStatus:  status,
		ReviewReasons: reasons,
		URL:           link,
	}
}

// QualityNotice 对应 qualityNotice：资料待复核时给模型的限制说明；其余状态返回空串。
func QualityNotice(reviewStatus string, reviewReasons []string) string {
	if reviewStatus != "needs_review" {
		return ""
	}
	return "资料待复核，可能存在内容缺失：" + orDefault(strings.Join(reviewReasons, "；"), "尚未确认完整性") +
		"。请明确标注限制，不据此推断未提供的内容。"
}
