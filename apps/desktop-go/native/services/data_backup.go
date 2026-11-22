package services

import (
	"context"
	"errors"
	"strings"
)

// 设置页「数据与备份」分区依赖的系统能力：数据目录与重启、备份与恢复、Markdown 导出、配置迁移。
//
// DTO 的 JSON 标签与 IPC 回复的字段一致（packages/shared/types/backup.ts、config-transfer.ts）：
// 主包适配器直接把处理器的回复解码进来。回复里的 { success:false, error } 在适配器里统一变成 error，
// 界面只看 error 与各结果的 Canceled：取消不是失败，不弹提示。
// 口令（恢复口令、导出密码）只经参数传入，不进任何 DTO 的字符串形式，也不进日志。

// RuntimePaths 是应用运行时的几个目录（app:getRuntimePaths）。
type RuntimePaths struct {
	UserDataPath string `json:"userDataPath"`
	DataDir      string `json:"dataDir"`
	DatabasePath string `json:"databasePath"`
	BackupsDir   string `json:"backupsDir"`
	LogsDir      string `json:"logsDir"`
}

// Data 是数据目录相关的系统动作：查目录、用资源管理器打开、重启应用。
type Data interface {
	// RuntimePaths 返回备份目录、日志目录等（app:getRuntimePaths）。
	RuntimePaths(ctx context.Context) (RuntimePaths, error)
	// OpenPath 用系统文件管理器打开目录；传文件路径时在文件夹里定位它（shell:openPath）。
	OpenPath(ctx context.Context, path string) error
	// Relaunch 重启应用（app:relaunch）：先返回，随后进程重启。
	Relaunch(ctx context.Context) error
}

// 备份来源类别（BackupKind）。
const (
	BackupKindManual     = "manual"
	BackupKindAuto       = "auto"
	BackupKindPreUpdate  = "pre-update"
	BackupKindPreRestore = "pre-restore"
)

// BackupSummary 是完整快照的内容摘要。
type BackupSummary struct {
	ItemCount     int      `json:"itemCount"`
	AssetCount    int      `json:"assetCount"`
	ConfigDomains []string `json:"configDomains"`
	SchemaVersion int      `json:"schemaVersion"`
	AppVersion    string   `json:"appVersion"`
}

// BackupInfo 是一份备份（旧 .db 快照或完整快照）的描述（BackupFileInfo）。
type BackupInfo struct {
	// FileName 是恢复与删除时用的标识（旧快照是文件名，完整快照是快照 ID）。
	FileName string `json:"fileName"`
	// Path 是绝对路径，仅展示。
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  int64  `json:"createdAt"` // 毫秒时间戳
	Format     string `json:"format"`
	Encrypted  bool   `json:"encrypted"`
	Validation string `json:"validation"`
	// Summary 只有完整快照有。
	Summary *BackupSummary `json:"summary,omitempty"`
}

// RepositoryStatus 是加密去重备份仓库的状态（BackupRepositoryStatus）。
type RepositoryStatus struct {
	Initialized              bool    `json:"initialized"`
	AutomaticAccessAvailable bool    `json:"automaticAccessAvailable"`
	KeyStorageBackend        *string `json:"keyStorageBackend"`
	// Warning 在系统密钥环不可用时说明原因与后果。
	Warning string `json:"warning,omitempty"`
}

// SnapshotCreated 是一次完整备份的结果：新增与复用的对象数。
type SnapshotCreated struct {
	Snapshot       BackupInfo `json:"snapshot"`
	CreatedObjects int        `json:"createdObjects"`
	ReusedObjects  int        `json:"reusedObjects"`
}

// RestorePreview 是恢复预检通过后的结果。
type RestorePreview struct {
	Snapshot BackupInfo `json:"snapshot"`
	Warnings []string   `json:"warnings"`
}

// RestorePreviewError 是预检失败：原因加上缺失与损坏的文件，界面把后两者逐行列在详情里。
type RestorePreviewError struct {
	Message      string
	MissingFiles []string
	DamagedFiles []string
}

func (e *RestorePreviewError) Error() string { return e.Message }

// MarkdownExport 是 Markdown 导出的结果；Canceled 为真表示用户没有选目录。
type MarkdownExport struct {
	Canceled bool
	Count    int
	Dir      string
}

// PortableExport 是便携备份导出的结果。
type PortableExport struct {
	Canceled bool
	FilePath string
}

// PortableImport 是便携备份导入的结果。
type PortableImport struct {
	Canceled bool
	// AlreadyExisted 表示本机仓库里已有这份快照，没有重复写入。
	AlreadyExisted bool
	// ReplacedRepository 表示本机原来没有快照的仓库被换成了便携备份的仓库，恢复口令随之变为便携备份的口令。
	ReplacedRepository bool
	Snapshot           *BackupInfo
}

// Backup 是备份、恢复与导出。所有失败返回带原因的 error（原因来自处理器的 error 字段，用户可读）。
type Backup interface {
	// List 返回旧 .db 快照（backup:list），新的在前。
	List(ctx context.Context) ([]BackupInfo, error)
	// Create 立即创建一份手动旧快照（backup:create，VACUUM INTO，含未合并的 WAL）。
	Create(ctx context.Context) (BackupInfo, error)
	// Restore 用旧快照替换当前数据（backup:restore）：成功后应用随即重启。
	// 换库失败时处理器仍会安排重启并回滚，此时返回错误，界面如实提示。
	Restore(ctx context.Context, fileName string) error
	// Delete 删除一份旧快照（backup:delete）。
	Delete(ctx context.Context, fileName string) error
	// ExportMarkdown 选目录并导出全部条目为 Markdown（export:markdown）；用户取消时 Canceled 为真。
	ExportMarkdown(ctx context.Context) (MarkdownExport, error)

	// RepositoryStatus 返回完整备份仓库的状态（backupRepository:status）。
	RepositoryStatus(ctx context.Context) (RepositoryStatus, error)
	// ListSnapshots 返回完整快照（backupRepository:listSnapshots）。
	ListSnapshots(ctx context.Context) ([]BackupInfo, error)
	// InitRepository 用恢复口令启用完整备份（backupRepository:init）。
	InitRepository(ctx context.Context, password string) error
	// ChangePassword 修改恢复口令（backupRepository:changePassword）；媒体对象不需要重写。
	ChangePassword(ctx context.Context, current, next string) error
	// CreateSnapshot 立即做一次完整备份（backupRepository:createSnapshot）；recoveryPassword 可为空（密钥环可用时）。
	CreateSnapshot(ctx context.Context, recoveryPassword string) (SnapshotCreated, error)
	// PreviewRestore 恢复预检（backupRepository:preview）；失败返回 *RestorePreviewError。
	PreviewRestore(ctx context.Context, snapshotID, recoveryPassword string) (RestorePreview, error)
	// RestoreSnapshot 用完整快照替换当前数据（backupRepository:restore）：成功后应用随即重启。
	RestoreSnapshot(ctx context.Context, snapshotID, recoveryPassword string) error
	// DeleteSnapshot 删除一份完整快照并回收不再被引用的对象（backupRepository:deleteSnapshot）。
	DeleteSnapshot(ctx context.Context, snapshotID string) error
	// ExportPortable 选位置导出便携备份（backupRepository:exportPortable）；用户取消时 Canceled 为真。
	ExportPortable(ctx context.Context, snapshotID, recoveryPassword string) (PortableExport, error)
	// ImportPortable 选文件导入便携备份（backupRepository:importPortable）；用户取消时 Canceled 为真。
	ImportPortable(ctx context.Context, recoveryPassword string) (PortableImport, error)
}

// ConfigExportOptions 是配置导出的选项（ConfigExportDialog 的输出）。
type ConfigExportOptions struct {
	// IncludeSecrets 为真时 API Key 与代理口令用 Password 加密后写入；为假时它们被清空。
	IncludeSecrets bool
	Password       string

	IncludeUILayout  bool
	IncludeShortcuts bool
	IncludeMCPScope  bool
}

// String 不含口令：避免 %v 把导出密码带进日志。
func (o ConfigExportOptions) String() string {
	return "ConfigExportOptions{secrets=" + boolText(o.IncludeSecrets) + "}"
}

// GoString 让 %#v 同样不泄露口令。
func (o ConfigExportOptions) GoString() string { return o.String() }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ConfigExportResult 是配置导出的结果。
type ConfigExportResult struct {
	Canceled bool
	FilePath string
}

// ConfigMCPScope 是预览里的 MCP 范围摘要。
type ConfigMCPScope struct {
	Mode               string `json:"mode"` // all | selected
	CollectionCount    int    `json:"collectionCount"`
	AllowUncategorized bool   `json:"allowUncategorized"`
}

// ConfigPreview 是应用前给用户看的文件摘要（ConfigTransferPreview）。
type ConfigPreview struct {
	ExportedAt       string `json:"exportedAt"`
	AppVersion       string `json:"appVersion"`
	Encrypted        bool   `json:"encrypted"`
	ProviderCount    int    `json:"providerCount"`
	ModelCount       int    `json:"modelCount"`
	RouteCount       int    `json:"routeCount"`
	StyleCount       int    `json:"styleCount"`
	ShortcutCount    int    `json:"shortcutCount"`
	UILayoutKeyCount int    `json:"uiLayoutKeyCount"`
	// MCPScope 为 nil 表示文件没带 MCP 范围，导入后本机现有范围保持不变。
	MCPScope *ConfigMCPScope `json:"mcpScope,omitempty"`
}

// ConfigRead 是选文件并读出预览的结果；Canceled 为真表示用户没有选文件。
type ConfigRead struct {
	Canceled bool
	FilePath string
	Preview  ConfigPreview
}

// ConfigApplySelection 是导入时实际应用哪些配置域。
type ConfigApplySelection struct {
	Settings  bool
	UILayout  bool
	Shortcuts bool
	MCPScope  bool
}

// ConfigApplyResult 是导入完成后的说明。
type ConfigApplyResult struct {
	// Warnings 逐条说明哪些部分没能完整还原。
	Warnings []string
	// SnapshotDir 是导入前留下的配置快照目录；空表示没有保存成功。
	SnapshotDir string
}

// ConfigWrongPasswordError 表示导入密码不对：用户改一下就能过，界面留在对话框里就地提示，不当作失败 toast。
type ConfigWrongPasswordError struct{ Message string }

func (e *ConfigWrongPasswordError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return "wrong config transfer password"
	}
	return e.Message
}

// ErrConfigNotWired 表示宿主没有接上配置迁移（nil 服务时界面的失败原因）。
var ErrConfigNotWired = errors.New("config transfer is not available in this build")

// ConfigTransfer 是配置迁移：导出全部软件设置、读取并预览、应用导入。
//
// 导入分两步（Read → Apply）是有意的：它会覆盖本机设置，用户得先看清文件里有什么。
// Apply 只接受最近一次 Read 选中的路径。Apply 成功时设置、快捷键、MCP 范围与界面布局都已写入，
// 界面随后提示并重启（Data.Relaunch）。
type ConfigTransfer interface {
	// Export 采集当前配置，选位置并写文件（config:export）；用户取消时 Canceled 为真。
	Export(ctx context.Context, opts ConfigExportOptions) (ConfigExportResult, error)
	// Read 选文件并解析出预览（config:read）；用户取消时 Canceled 为真，文件读不出来返回带原因的错误。
	Read(ctx context.Context) (ConfigRead, error)
	// Apply 应用导入（config:apply）。密码不对返回 *ConfigWrongPasswordError；其余失败返回带原因的错误。
	Apply(ctx context.Context, filePath, password string, sel ConfigApplySelection) (ConfigApplyResult, error)
}
