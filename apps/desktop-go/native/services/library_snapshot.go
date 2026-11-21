package services

// M2-5b：公众号网页快照（渲染层 WebSnapshotPane）的数据形状：读取快照、导出 HTML、补采与补采进度。
// 对应的 IPC 频道：web:snapshot、web:snapshot-export、web:snapshot-enqueue、import:list、import:cancel。

// LibraryWebSnapshotAsset 是快照里一张已保存到本地的图片（packages/shared 的 WebSnapshotAsset）。
// 图片文件在用户数据目录的 images 下，以 FileName 定位；界面用 `local-image://<FileName>` 引用。
type LibraryWebSnapshotAsset struct {
	FileName  string `json:"fileName"`
	SourceURL string `json:"sourceUrl"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
}

// LibraryWebSnapshotFailure 是快照里一次没有保存成功的资源（WebSnapshot.failures 的一项）。
type LibraryWebSnapshotFailure struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// LibraryWebSnapshotMeta 是快照里原生界面用到的部分：图片清单与不完整的原因。
// 排版内容（html、css）已经拼进 LibraryWebSnapshotView.Document，这里不重复携带。
// 三个切片永不为 nil（缺省解码成空切片），界面可以直接用长度判断「有没有缺失」。
type LibraryWebSnapshotMeta struct {
	Assets   []LibraryWebSnapshotAsset   `json:"assets"`
	Failures []LibraryWebSnapshotFailure `json:"failures"`
	Warnings []string                    `json:"warnings"`
}

// Incomplete 报告快照是否有缺失的资源或警告（与导出结果的 Incomplete 同一口径）。
func (m LibraryWebSnapshotMeta) Incomplete() bool {
	return len(m.Failures) > 0 || len(m.Warnings) > 0
}

// LibraryWebSnapshotVersion 是快照所属的来源版本（WebSourceVersion 里界面要用的部分）。
type LibraryWebSnapshotVersion struct {
	ID         string `json:"id"`
	CapturedAt int64  `json:"capturedAt"`
	// Snapshot 为 nil 表示这个版本没有网页快照。
	Snapshot *LibraryWebSnapshotMeta `json:"snapshot"`
}

// LibraryWebSnapshotView 是 web:snapshot 的数据（WebSnapshotView）：一个版本的快照与阅读它所需的状态。
//
// 没有可读的快照时 Version 为 nil：此时 Error 非空表示快照损坏（图片缺失、校验失败、格式无效），
// 为空表示条目还没有保存过快照；两种情况界面都退回标准排版，但损坏要给可复制的原因。
// Document 是整页 HTML（已净化，带 InstanceID 标识与桥接脚本），只能放进沙箱 iframe 的 srcdoc。
type LibraryWebSnapshotView struct {
	// InstanceID 是这次读取的实例标识：iframe 消息按它过滤，换版本或重新读取后旧消息作废。
	InstanceID string `json:"instanceId"`
	Document   string `json:"document"`
	// Edited 为真表示条目正文在采集之后被用户改过（快照不含这些修改）。
	Edited bool `json:"edited"`
	// Pending 为真表示有比当前查看的版本更新、且尚未采用的快照版本。
	Pending bool                       `json:"pending"`
	Error   string                     `json:"error"`
	Version *LibraryWebSnapshotVersion `json:"version"`
}

// Snapshot 返回当前版本的快照信息；没有版本或版本没有快照时返回 nil。
func (v LibraryWebSnapshotView) Snapshot() *LibraryWebSnapshotMeta {
	if v.Version == nil {
		return nil
	}
	return v.Version.Snapshot
}

// LibraryWebSnapshotExportResult 是导出 HTML 的结果（web:snapshot-export）。
// Canceled 为真表示用户没有选目录（不算失败）；否则 Path 是写出的目录；
// Incomplete 为真表示导出的快照本身有缺失的资源或警告，界面要提示「部分资源有缺失」。
type LibraryWebSnapshotExportResult struct {
	Canceled   bool   `json:"canceled"`
	Path       string `json:"path"`
	Incomplete bool   `json:"incomplete"`
}

// LibraryImportTaskStatus 是轮询补采进度所需的导入任务最小字段（import:list 里的一行）。
type LibraryImportTaskStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// RefreshOfItemID 是这个任务要刷新的条目；补采任务总是有值。
	RefreshOfItemID string `json:"refreshOfItemId"`
	Error           string `json:"error"`
	Warning         string `json:"warning"`
}

// Finished 报告任务是否已经结束（不会再变化）：完成、失败、取消与重复都算。
func (s LibraryImportTaskStatus) Finished() bool {
	switch s.Status {
	case ImportStatusCompleted, ImportStatusFailed, ImportStatusCanceled, ImportStatusDuplicate:
		return true
	}
	return false
}
