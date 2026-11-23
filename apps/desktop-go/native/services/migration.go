package services

import "context"

// 旧版（.NET）归知数据库的一次性迁移：首次启动检测到旧库且当前知识库为空时，界面提示迁移。
//
// 对应主进程的 migration:detectLegacy / migration:runLegacy（legacy_migration.go）。迁移只读旧库：处理器在旧库的
// 临时拷贝上读取、绝不改动原文件，旧库留有未合并的写入日志时拒绝迁移；当前知识库非空时拒绝（防覆盖）。
// 界面（viva/screens/overlays）只依赖本接口，适配器（package main 的 native_migration.go）接到现有处理器。

// LegacyDetection 是探测到的旧库：路径与未删除的条目数。
type LegacyDetection struct {
	Path      string `json:"path"`
	ItemCount int    `json:"itemCount"`
}

// LegacyMigrationStats 是一次迁移写入的各类记录条数（LegacyMigrationStats）。
type LegacyMigrationStats struct {
	Collections    int `json:"collections"`
	Tags           int `json:"tags"`
	Items          int `json:"items"`
	ItemTags       int `json:"itemTags"`
	Sources        int `json:"sources"`
	WikiPages      int `json:"wikiPages"`
	WikiLinks      int `json:"wikiLinks"`
	WikiSources    int `json:"wikiSources"`
	WikiIngestions int `json:"wikiIngestions"`
}

// Migration 是旧版数据迁移的能力。
type Migration interface {
	// DetectLegacy 探测旧库：found 为假表示没有需要迁移的旧库（没有文件、当前库非空、旧库没有条目，
	// 以及旧库读不出来——处理器把探测失败当作「没有」并写日志）；err 只表示没能完成探测（如读当前库失败）。
	DetectLegacy(ctx context.Context) (det LegacyDetection, found bool, err error)
	// RunLegacy 执行迁移。sourcePath 为空取默认旧库路径（用户数据目录下 data/guizhi.db）；
	// 文件不存在、不是旧版数据库、有残留日志、当前库非空或写入失败返回带原因的错误，此时当前库保持原样（单个事务）。
	RunLegacy(ctx context.Context, sourcePath string) (LegacyMigrationStats, error)
}
