package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// settingsCoreFixtureJSON 是设置页四个核心分区（网络、快捷键、数据与备份、关于）对照场景的数据夹具：
// 与 React 采集器（scripts/parity/lib/react-settings-core.mjs）读的是同一个文件，两侧因此处在同一组合成数据里。
// 备份、仓库、运行时路径的字段与 IPC 回复一致（services.BackupInfo 等的 JSON 标签），React 一侧页内替换对应频道时原样返回。
//
//go:embed testdata/settings-core-fixture.json
var settingsCoreFixtureJSON []byte

// SettingsCoreProxy 是「手动代理」场景里逐项填入的值（协议由下拉选，其余是文本框）。
//
// 没有口令：Viva 的口令框（Secure）出于保密从不把值放进可访问树，渲染层的口令框在可访问树里是一串圆点（value=••••），
// 填了口令两边的 L1 状态必然不一致，而这一类偏差没有登记、也不新增 Waiver；口令框的圆点显示与保密行为由 L4 测试覆盖。
type SettingsCoreProxy struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Bypass   string `json:"bypass"`
}

// SettingsCoreFixture 是夹具的内容。
type SettingsCoreFixture struct {
	// Version 是关于页显示的应用版本号（updater:version）。
	Version string `json:"version"`
	// RuntimePaths 是备份目录与日志目录（app:getRuntimePaths）：有了它「打开备份目录」「打开日志」两个按钮才可用。
	RuntimePaths services.RuntimePaths `json:"runtimePaths"`
	// Shortcuts 是各动作的组合键（shortcuts:get）；空串表示未设置。
	Shortcuts map[string]string `json:"shortcuts"`
	// Proxy 是手动代理场景填入的值。
	Proxy SettingsCoreProxy `json:"proxy"`
	// LegacyBackups 是旧数据库快照列表（backup:list），新的在前。
	LegacyBackups []services.BackupInfo `json:"legacyBackups"`
	// Repository 是完整备份仓库：状态（backupRepository:status）与快照列表（backupRepository:listSnapshots）。
	Repository struct {
		Status    services.RepositoryStatus `json:"status"`
		Snapshots []services.BackupInfo     `json:"snapshots"`
	} `json:"repository"`
}

// LoadSettingsCoreFixture 解码夹具。
func LoadSettingsCoreFixture() (SettingsCoreFixture, error) {
	var f SettingsCoreFixture
	if err := json.Unmarshal(settingsCoreFixtureJSON, &f); err != nil {
		return SettingsCoreFixture{}, fmt.Errorf("解码设置核心分区夹具失败：%w", err)
	}
	return f, nil
}
