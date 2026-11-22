package servicestest

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// 「数据与备份」分区三个服务的假实现：内存里存数据，记录每次调用的参数（含口令——只给测试核对「口令确实传到了」，
// 断言里要同时核对口令没有出现在提示与日志里），可按方法名注入失败、用 Gate 把调用停在「进行中」。零值可用。

// Call 是一次被记录的调用。
type Call struct {
	Method string
	Args   []string
}

// calls 是三个假实现共用的记录、失败注入与停靠点。
type calls struct {
	mu    sync.Mutex
	log   []Call
	fails map[string]error
	gates map[string]chan struct{}
}

// Fail 让 method 之后的调用返回 err 且不改数据；传 nil 取消。
func (c *calls) Fail(method string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fails == nil {
		c.fails = map[string]error{}
	}
	if err == nil {
		delete(c.fails, method)
		return
	}
	c.fails[method] = err
}

// Hold 让 method 的调用先停在入口，直到返回的函数被调用：测试用来把「进行中」状态停在界面上。
func (c *calls) Hold(method string) (release func()) {
	gate := make(chan struct{})
	c.mu.Lock()
	if c.gates == nil {
		c.gates = map[string]chan struct{}{}
	}
	c.gates[method] = gate
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

// Calls 返回全部调用（按顺序）。
func (c *calls) Calls() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Call(nil), c.log...)
}

// CallsOf 返回某个方法的调用。
func (c *calls) CallsOf(method string) []Call {
	var out []Call
	for _, call := range c.Calls() {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

// enter 记录一次调用，等停靠点放行，再返回注入的失败。
func (c *calls) enter(method string, args ...string) error {
	c.mu.Lock()
	c.log = append(c.log, Call{Method: method, Args: args})
	gate, err := c.gates[method], c.fails[method]
	c.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
}

// ── Data ──

// Data 是 services.Data 的假实现。
type Data struct {
	calls
	paths services.RuntimePaths
	set   bool
}

var _ services.Data = (*Data)(nil)

// SetPaths 摆好运行时目录。
func (f *Data) SetPaths(p services.RuntimePaths) {
	f.mu.Lock()
	f.paths, f.set = p, true
	f.mu.Unlock()
}

func (f *Data) RuntimePaths(context.Context) (services.RuntimePaths, error) {
	if err := f.enter("RuntimePaths"); err != nil {
		return services.RuntimePaths{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.set {
		return services.RuntimePaths{
			UserDataPath: `C:\fake\userdata`, DataDir: `C:\fake\userdata\data`, DatabasePath: `C:\fake\userdata\data\knowledge.db`,
			BackupsDir: `C:\fake\userdata\backups`, LogsDir: `C:\fake\userdata\logs`,
		}, nil
	}
	return f.paths, nil
}

func (f *Data) OpenPath(_ context.Context, path string) error { return f.enter("OpenPath", path) }

func (f *Data) Relaunch(context.Context) error { return f.enter("Relaunch") }

// ── Backup ──

// Backup 是 services.Backup 的假实现：旧快照与完整快照各一份内存列表，恢复口令至少 12 位。
type Backup struct {
	calls
	legacy   []services.BackupInfo
	snaps    []services.BackupInfo
	status   services.RepositoryStatus
	password string // 仓库当前的恢复口令（InitRepository 设定）
	markdown services.MarkdownExport
	hasMD    bool
	portable services.PortableExport
	hasPort  bool
	imported services.PortableImport
	hasImp   bool
	preview  *services.RestorePreview
	next     int
}

var _ services.Backup = (*Backup)(nil)

// MinRepositoryPassword 是假实现接受的恢复口令最短长度（与真实实现一致）。
const MinRepositoryPassword = 12

// SetLegacy、SetSnapshots 摆好两份列表；SetStatus 摆好仓库状态。
func (f *Backup) SetLegacy(list []services.BackupInfo) {
	f.mu.Lock()
	f.legacy = append([]services.BackupInfo(nil), list...)
	f.mu.Unlock()
}
func (f *Backup) SetSnapshots(list []services.BackupInfo) {
	f.mu.Lock()
	f.snaps = append([]services.BackupInfo(nil), list...)
	f.mu.Unlock()
}
func (f *Backup) SetStatus(s services.RepositoryStatus) { f.mu.Lock(); f.status = s; f.mu.Unlock() }

// SetPassword 摆好仓库的恢复口令（修改口令时核对「当前口令」）。
func (f *Backup) SetPassword(pw string) { f.mu.Lock(); f.password = pw; f.mu.Unlock() }

// SetMarkdown、SetPortableExport、SetPortableImport 摆好对应动作的结果（比如 Canceled）。
func (f *Backup) SetMarkdown(r services.MarkdownExport) {
	f.mu.Lock()
	f.markdown, f.hasMD = r, true
	f.mu.Unlock()
}
func (f *Backup) SetPortableExport(r services.PortableExport) {
	f.mu.Lock()
	f.portable, f.hasPort = r, true
	f.mu.Unlock()
}
func (f *Backup) SetPortableImport(r services.PortableImport) {
	f.mu.Lock()
	f.imported, f.hasImp = r, true
	f.mu.Unlock()
}

// SetPreview 摆好恢复预检的结果；不摆时按快照列表里的同名快照给出。
func (f *Backup) SetPreview(p services.RestorePreview) { f.mu.Lock(); f.preview = &p; f.mu.Unlock() }

// Legacy、Snapshots 返回此刻的两份列表（副本）。
func (f *Backup) Legacy() []services.BackupInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.BackupInfo(nil), f.legacy...)
}
func (f *Backup) Snapshots() []services.BackupInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.BackupInfo(nil), f.snaps...)
}

func find(list []services.BackupInfo, name string) int {
	for i, b := range list {
		if b.FileName == name {
			return i
		}
	}
	return -1
}

func (f *Backup) List(context.Context) ([]services.BackupInfo, error) {
	if err := f.enter("List"); err != nil {
		return nil, err
	}
	return f.Legacy(), nil
}

func (f *Backup) Create(context.Context) (services.BackupInfo, error) {
	if err := f.enter("Create"); err != nil {
		return services.BackupInfo{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	info := services.BackupInfo{
		FileName: fmt.Sprintf("knowledge-manual-20260101-%06d.db", f.next), Kind: services.BackupKindManual,
		SizeBytes: 2048, CreatedAt: 1767225600000 + int64(f.next)*1000, Format: "legacy-db", Validation: "valid",
	}
	f.legacy = append([]services.BackupInfo{info}, f.legacy...)
	return info, nil
}

func (f *Backup) Restore(_ context.Context, fileName string) error {
	if err := f.enter("Restore", fileName); err != nil {
		return err
	}
	if find(f.Legacy(), fileName) < 0 {
		return errors.New("找不到指定的备份文件")
	}
	return nil
}

func (f *Backup) Delete(_ context.Context, fileName string) error {
	if err := f.enter("Delete", fileName); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	i := find(f.legacy, fileName)
	if i < 0 {
		return errors.New("找不到指定的备份文件")
	}
	f.legacy = append(f.legacy[:i:i], f.legacy[i+1:]...)
	return nil
}

func (f *Backup) ExportMarkdown(context.Context) (services.MarkdownExport, error) {
	if err := f.enter("ExportMarkdown"); err != nil {
		return services.MarkdownExport{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasMD {
		return services.MarkdownExport{Count: 3, Dir: `C:\fake\export\GuiZhi-Export-20260101-000000`}, nil
	}
	return f.markdown, nil
}

func (f *Backup) RepositoryStatus(context.Context) (services.RepositoryStatus, error) {
	if err := f.enter("RepositoryStatus"); err != nil {
		return services.RepositoryStatus{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *Backup) ListSnapshots(context.Context) ([]services.BackupInfo, error) {
	if err := f.enter("ListSnapshots"); err != nil {
		return nil, err
	}
	return f.Snapshots(), nil
}

func (f *Backup) InitRepository(_ context.Context, password string) error {
	if err := f.enter("InitRepository", password); err != nil {
		return err
	}
	if len([]rune(password)) < MinRepositoryPassword {
		return errors.New("恢复口令至少需要 12 个字符")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.password = password
	f.status = services.RepositoryStatus{Initialized: true, AutomaticAccessAvailable: true}
	return nil
}

func (f *Backup) ChangePassword(_ context.Context, current, next string) error {
	if err := f.enter("ChangePassword", current, next); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if current != f.password {
		return errors.New("当前口令不正确")
	}
	if len([]rune(next)) < MinRepositoryPassword {
		return errors.New("新的恢复口令至少需要 12 个字符")
	}
	f.password = next
	return nil
}

func (f *Backup) CreateSnapshot(_ context.Context, recoveryPassword string) (services.SnapshotCreated, error) {
	if err := f.enter("CreateSnapshot", recoveryPassword); err != nil {
		return services.SnapshotCreated{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	snap := services.BackupInfo{
		FileName: fmt.Sprintf("snap-%04d", f.next), Kind: services.BackupKindManual, SizeBytes: 4 << 20,
		CreatedAt: 1767225600000 + int64(f.next)*1000, Format: "repository-snapshot", Encrypted: true, Validation: "valid",
		Summary: &services.BackupSummary{ItemCount: 12, AssetCount: 5, ConfigDomains: []string{"ai", "shortcuts"}, SchemaVersion: 33, AppVersion: "0.0.0"},
	}
	f.snaps = append([]services.BackupInfo{snap}, f.snaps...)
	return services.SnapshotCreated{Snapshot: snap, CreatedObjects: 5, ReusedObjects: 2}, nil
}

func (f *Backup) PreviewRestore(_ context.Context, snapshotID, recoveryPassword string) (services.RestorePreview, error) {
	if err := f.enter("PreviewRestore", snapshotID, recoveryPassword); err != nil {
		return services.RestorePreview{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.preview != nil {
		return *f.preview, nil
	}
	i := find(f.snaps, snapshotID)
	if i < 0 {
		return services.RestorePreview{}, &services.RestorePreviewError{Message: "找不到指定的快照"}
	}
	return services.RestorePreview{Snapshot: f.snaps[i], Warnings: []string{}}, nil
}

func (f *Backup) RestoreSnapshot(_ context.Context, snapshotID, recoveryPassword string) error {
	return f.enter("RestoreSnapshot", snapshotID, recoveryPassword)
}

func (f *Backup) DeleteSnapshot(_ context.Context, snapshotID string) error {
	if err := f.enter("DeleteSnapshot", snapshotID); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	i := find(f.snaps, snapshotID)
	if i < 0 {
		return errors.New("找不到指定的快照")
	}
	f.snaps = append(f.snaps[:i:i], f.snaps[i+1:]...)
	return nil
}

func (f *Backup) ExportPortable(_ context.Context, snapshotID, recoveryPassword string) (services.PortableExport, error) {
	if err := f.enter("ExportPortable", snapshotID, recoveryPassword); err != nil {
		return services.PortableExport{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasPort {
		return services.PortableExport{FilePath: `C:\fake\export\GuiZhi.guizhi-backup`}, nil
	}
	return f.portable, nil
}

func (f *Backup) ImportPortable(_ context.Context, recoveryPassword string) (services.PortableImport, error) {
	if err := f.enter("ImportPortable", recoveryPassword); err != nil {
		return services.PortableImport{}, err
	}
	if recoveryPassword == "" {
		return services.PortableImport{}, errors.New("请先输入这份便携备份对应的恢复口令")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasImp {
		return services.PortableImport{}, nil
	}
	return f.imported, nil
}

// ── ConfigTransfer ──

// ConfigTransfer 是 services.ConfigTransfer 的假实现：读出预设的预览，应用时核对密码（预设了 WantPassword 才核对）。
type ConfigTransfer struct {
	calls
	export    services.ConfigExportResult
	hasExport bool
	read      services.ConfigRead
	hasRead   bool
	apply     services.ConfigApplyResult
	// WantPassword 非空时 Apply 只接受这个密码，其余返回 *ConfigWrongPasswordError。
	WantPassword string
	exports      []services.ConfigExportOptions
	applies      []ConfigApply
}

var _ services.ConfigTransfer = (*ConfigTransfer)(nil)

// ConfigApply 是一次被记录的导入：路径、选择（不含密码——密码在 Calls 里）。
type ConfigApply struct {
	FilePath  string
	Selection services.ConfigApplySelection
}

// SetExport、SetRead、SetApply 摆好三个动作的结果。
func (f *ConfigTransfer) SetExport(r services.ConfigExportResult) {
	f.mu.Lock()
	f.export, f.hasExport = r, true
	f.mu.Unlock()
}
func (f *ConfigTransfer) SetRead(r services.ConfigRead) {
	f.mu.Lock()
	f.read, f.hasRead = r, true
	f.mu.Unlock()
}
func (f *ConfigTransfer) SetApply(r services.ConfigApplyResult) {
	f.mu.Lock()
	f.apply = r
	f.mu.Unlock()
}

// Exports 返回每次导出收到的选项；Applies 返回每次导入的路径与选择。
func (f *ConfigTransfer) Exports() []services.ConfigExportOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.ConfigExportOptions(nil), f.exports...)
}
func (f *ConfigTransfer) Applies() []ConfigApply {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ConfigApply(nil), f.applies...)
}

func (f *ConfigTransfer) Export(_ context.Context, opts services.ConfigExportOptions) (services.ConfigExportResult, error) {
	f.mu.Lock()
	f.exports = append(f.exports, opts)
	f.mu.Unlock()
	if err := f.enter("Export", opts.Password); err != nil {
		return services.ConfigExportResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasExport {
		return services.ConfigExportResult{FilePath: `C:\fake\export\GuiZhi-Config-20260101-000000.json`}, nil
	}
	return f.export, nil
}

func (f *ConfigTransfer) Read(context.Context) (services.ConfigRead, error) {
	if err := f.enter("Read"); err != nil {
		return services.ConfigRead{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasRead {
		return services.ConfigRead{Canceled: true}, nil
	}
	return f.read, nil
}

func (f *ConfigTransfer) Apply(_ context.Context, filePath, password string, sel services.ConfigApplySelection) (services.ConfigApplyResult, error) {
	f.mu.Lock()
	f.applies = append(f.applies, ConfigApply{FilePath: filePath, Selection: sel})
	want := f.WantPassword
	f.mu.Unlock()
	if err := f.enter("Apply", filePath, password); err != nil {
		return services.ConfigApplyResult{}, err
	}
	if want != "" && password != want {
		return services.ConfigApplyResult{}, &services.ConfigWrongPasswordError{Message: "密码不正确"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apply, nil
}
