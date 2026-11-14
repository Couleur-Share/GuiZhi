package main

// 数据库 WAL 模式（2026-11-14 起）。
//
// 为什么：回滚日志模式下读写互相等待——后台读（复检、慢查询、MCP 服务进程）会让写入提交排队，
// 实测 20 万行库上复检期间写入最长 1.58 s，WAL 下 28 ms；单次小事务写入 3.29 ms → 0.34 ms。
//
// 代价与约束（改这里之前必读）：
//   - 库打开时**不能只拷主文件**：最近的写入可能还在 -wal 里，单拷主文件会静默丢数据。
//     备份用 VACUUM INTO（vacuumInto）；要字节级保存现场用 copyDatabaseFiles；
//     换库前用 checkpointLiveWAL 把 -wal 合进主文件。
//   - 只读连接关闭后可能留下 -wal/-shm，这是正常的，下次读写打开会恢复。
//   - 不支持网络文件系统；切换失败（PRAGMA 返回的不是 wal）时回落到原有的回滚日志模式，功能不受影响。
//   - 不在退出时切回回滚日志：Electron 版已退役，切回会让每次退出变重并与 MCP 读者抢锁。
//
// SQLite WAL mode: readers and the writer no longer block each other. Never copy only the main file
// while the database is open; use VACUUM INTO, copyDatabaseFiles or checkpointLiveWAL.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// walMagic 是 WAL 文件头的两种合法魔数（0x377f0682 / 0x377f0683，大端）。
func isWALMagic(b []byte) bool {
	return len(b) >= 4 && b[0] == 0x37 && b[1] == 0x7f && b[2] == 0x06 && (b[3] == 0x82 || b[3] == 0x83)
}

// hasLiveWAL 报告 <db>-wal 是否是一个带合法文件头、可能含未合并内容的 WAL。
// 空文件或乱码文件 SQLite 自己也会忽略，不当作 WAL 处理。
func hasLiveWAL(dbPath string) bool {
	f, err := os.Open(dbPath + "-wal")
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < 32 {
		return false
	}
	head := make([]byte, 4)
	if _, err := f.ReadAt(head, 0); err != nil {
		return false
	}
	return isWALMagic(head)
}

// ensureWALMode 把连接所在的库切到 WAL（对已是 WAL 的库是空操作，模式写在文件头里持久生效）。
// 返回切换后的实际模式；不是 "wal" 表示该文件系统不支持，调用方记日志并继续用原模式。
func ensureWALMode(ctx context.Context, conn *sql.Conn) (string, error) {
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return "", err
	}
	return strings.ToLower(mode), nil
}

// checkpointLiveWAL 在换库/保存快照之前，把 -wal 里的内容合进主文件并截断。
// 调用时不应有别的连接持有该库；没有合法 WAL 时什么也不做（旧库、伪造的旁车文件都不受影响）。
// 合并失败或仍有残留时返回错误：此时继续拷主文件会丢数据，宁可中止。
func checkpointLiveWAL(ctx context.Context, dbPath string) error {
	if !hasLiveWAL(dbPath) {
		return nil
	}
	db, err := openReadWrite(dbPath)
	if err != nil {
		return fmt.Errorf("无法打开数据库合并 WAL：%w", err)
	}
	var busy, logFrames, checkpointed int
	err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	db.Close()
	if err != nil {
		return fmt.Errorf("合并 WAL 失败：%w", err)
	}
	if busy != 0 {
		return fmt.Errorf("合并 WAL 被其他连接占用（可能有别的归知进程或工具正在读取数据库）")
	}
	if hasLiveWAL(dbPath) {
		return fmt.Errorf("合并 WAL 后仍有未合并内容")
	}
	return nil
}

// copyDatabaseFiles 字节级保存数据库现场：主文件，加上仍带内容的 -wal（存为 dst+"-wal"，SQLite 按名字自动配对）。
// 用于损坏库的修复前备份——这类库上 VACUUM INTO 可能直接失败，所以不能用逻辑备份代替。
func copyDatabaseFiles(src, dst string) error {
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if hasLiveWAL(src) {
		if err := copyFile(src+"-wal", dst+"-wal"); err != nil {
			os.Remove(dst)
			return err
		}
	}
	return nil
}
