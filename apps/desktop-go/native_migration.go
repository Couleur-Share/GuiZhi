package main

// 旧版数据迁移（native/services/migration.go）的主包适配器。
//
// 与 native_adapters.go 的其余适配器同一做法：经 Bridge.call 调已登记的 IPC 处理器（legacy_migration.go，
// 与渲染进程同一份逻辑、同一套错误），再把 JSON 结果解码成 DTO；行为只有一个来源，对应关系由 native_migration_test.go
// 用真实处理器与真实 SQLite 守住。
//
//	Migration.DetectLegacy → migration:detectLegacy（没有旧库返回 null → found=false）
//	Migration.RunLegacy    → migration:runLegacy（空路径取默认旧库；在旧库的临时拷贝上读、单个事务写，原文件不动）

import (
	"context"

	"guizhi-mygo-shell/native/services"
)

type migrationAdapter struct{ b *Bridge }

var _ services.Migration = migrationAdapter{}

func (a migrationAdapter) DetectLegacy(ctx context.Context) (services.LegacyDetection, bool, error) {
	det, err := callAs[*services.LegacyDetection](ctx, a.b, chMigrationDetectLegacy)
	if err != nil || det == nil {
		return services.LegacyDetection{}, false, err
	}
	return *det, true, nil
}

func (a migrationAdapter) RunLegacy(ctx context.Context, sourcePath string) (services.LegacyMigrationStats, error) {
	return callAs[services.LegacyMigrationStats](ctx, a.b, chMigrationRunLegacy, sourcePath)
}
