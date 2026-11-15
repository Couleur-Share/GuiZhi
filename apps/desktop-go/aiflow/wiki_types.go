package aiflow

// Wiki 编译相关的数据类型（对应 packages/shared/types/wiki.ts 与 wiki-compiler.ts）。
// JSON 标签与 TS 字段名一致：golden 对拍时直接按 JSON 比较，也是主包适配层与旧 IPC 载荷之间的约定。

// WikiCompilerVersion 对应 WIKI_COMPILER_VERSION：编译器版本，参与模型身份串，变了就不能续跑旧任务。
const WikiCompilerVersion = "fulltext-v2"

// WikiPageKind 对应 WikiPageKind：topic / entity / concept。
type WikiPageKind string

const (
	WikiKindTopic   WikiPageKind = "topic"
	WikiKindEntity  WikiPageKind = "entity"
	WikiKindConcept WikiPageKind = "concept"
)

// WikiCatalogEntry 对应 WikiCatalogEntry：目录条目（链接解析与编译提示用的轻量投影）。
type WikiCatalogEntry struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	NormalizedTitle string       `json:"normalizedTitle"`
	Kind            WikiPageKind `json:"kind"`
	Summary         string       `json:"summary"`
	AliasesJSON     *string      `json:"aliasesJson"`
	ManualEditedAt  *int64       `json:"manualEditedAt"`
	UpdatedAt       int64        `json:"updatedAt"`
}

// WikiSearchHit 对应 WikiSearchHit：页面全文检索命中（问答检索用）。
type WikiSearchHit struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Kind    WikiPageKind `json:"kind"`
	Summary string       `json:"summary"`
}

// WikiSourceRef 对应 WikiSourceRef：页面的一个来源条目引用。
type WikiSourceRef struct {
	ReviewStatus  string   `json:"reviewStatus,omitempty"` // 空串表示未设置
	ReviewReasons []string `json:"reviewReasons,omitempty"`
	ItemID        string   `json:"itemId"`
	Title         string   `json:"title"`
}

// WikiPage 对应 WikiPage，只含问答用到的字段（正文与更新时间）；其余字段在适配层丢弃。
type WikiPage struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	UpdatedAt int64  `json:"updatedAt"`
}

// WikiPageDetail 对应 WikiPageDetail 里问答用到的部分：页面本体与来源条目（反向链接不需要）。
type WikiPageDetail struct {
	Page    WikiPage        `json:"page"`
	Sources []WikiSourceRef `json:"sources"`
}

// WikiPageDraft 对应 WikiPageDraft：净化后的页面草稿。
type WikiPageDraft struct {
	Title           string       `json:"title"`
	NormalizedTitle string       `json:"normalizedTitle"`
	Kind            WikiPageKind `json:"kind"`
	Summary         string       `json:"summary"`
	Body            string       `json:"body"`
	AliasesJSON     *string      `json:"aliasesJson"`
}

// WikiContribution 对应 WikiContributions 的元素：草稿加上按白名单清洗后的出链目标。
type WikiContribution struct {
	Title           string       `json:"title"`
	NormalizedTitle string       `json:"normalizedTitle"`
	Kind            WikiPageKind `json:"kind"`
	Summary         string       `json:"summary"`
	Body            string       `json:"body"`
	AliasesJSON     *string      `json:"aliasesJson"`
	LinkTargets     []string     `json:"linkTargets"`
}

// WikiBlock 对应 WikiBlock：来源条目里的一个编译分块。
type WikiBlock struct {
	Key     string `json:"key"`
	Hash    string `json:"hash"`
	Field   string `json:"field"` // title | content | transcript
	Section string `json:"section"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
}

// WikiCandidate 对应 WikiCandidate：预览里的一条候选资料。State 取 ready / review / waiting / upgrade / current / excluded。
type WikiCandidate struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Blocks      int    `json:"blocks"`
	Reusable    int    `json:"reusable"`
}

// WikiRequestRange 是预览估算的请求数区间。
type WikiRequestRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// WikiCompilePreview 对应 WikiCompilePreview。
type WikiCompilePreview struct {
	Entries  []WikiCandidate  `json:"entries"`
	Counts   map[string]int   `json:"counts"`
	Requests WikiRequestRange `json:"requests"`
}

// WikiCompileJob 对应 WikiCompileJob。Status 取 running / paused / cancelled / interrupted / failed / completed。
type WikiCompileJob struct {
	ID        string `json:"id"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Error     string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// WikiCompileWork 对应 WikiCompileWork：检查点交给驱动器的下一个分块。
type WikiCompileWork struct {
	JobID       string    `json:"jobId"`
	ItemID      string    `json:"itemId"`
	Title       string    `json:"title"`
	Fingerprint string    `json:"fingerprint"`
	Block       WikiBlock `json:"block"`
}

// WikiCompileFailure 对应 WikiCompileFailure：只有条目名和「为什么没编出来」才是可据以行动的。
type WikiCompileFailure struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// WikiCompileRoundResult 对应 WikiCompileRoundResult：后台一轮编译的汇总。
type WikiCompileRoundResult struct {
	Compiled int                  `json:"compiled"`
	Pending  int                  `json:"pending"`
	Skipped  int                  `json:"skipped"`
	Failures []WikiCompileFailure `json:"failures"`
}
