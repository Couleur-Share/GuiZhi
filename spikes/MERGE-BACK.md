# 两个主线修复怎么合回去

这两个修复在做 MyGo spike 时发现，都是**产品原有缺陷**，与 MyGo 迁移无关，可以单独合并。它们各自在独立 worktree 里完成，**没有改动 `D:\Project\GuiZhi` 原目录和它的分支 `fix/design-decisions`**，也没有 push（本机分支）。

| 分支 | worktree | 提交 | 基于 | 改动 |
|---|---|---|---|---|
| `fix/fts-backfill-not-in` | `D:\Project\GuiZhi-fts-fix` | `43f10d0c` | `0c5775db` | 4 个文件 +72/−10：`packages/db/src/knowledge.ts`、`wiki.ts` 与两个单测 |
| `fix/empty-db-guard` | `D:\Project\GuiZhi-zero-db` | `4442e023` | `0c5775db` | 3 个文件 +122/−1：`packages/db/src/init.ts`、`apps/desktop/src/main/database/index.ts`、`tests/unit/db/startup-integrity.test.ts` |

## 它们修什么

1. **FTS 回写 O(n²)**：`backfillMissingFtsRows` 的补齐 SQL 用相关子查询 `NOT EXISTS`，实测 4000 条 1.8 s、8000 条 9.2 s。改成不相关的 `NOT IN (… IS NOT NULL)`，行为不变，补齐耗时随条数线性增长。新增回归测试。
2. **0 字节数据库被静默当空库**：磁盘写满或写入中断会留下 0 字节的 `knowledge.db`，现状是启动后表现为「资料全没了」且不报错。改为：文件是 0 字节且存在可恢复备份时拒绝启动，错误说明给出备份路径，以及「从备份恢复」与「删除这个空文件后重启」两条出路；没有可用备份（含备份本身是 0 字节）仍按空库启动，行为不变；任何情况下都不改动该文件。备份来源由主进程通过钩子 `findRecoverableBackups` 传入（`listBackups()` 的路径），库旁的 `<db>.integrity-backup-*` 在 db 包内直接枚举。

## 验证状态

| 项 | `fix/fts-backfill-not-in` | `fix/empty-db-guard` |
|---|---|---|
| 相关单测 | `knowledge-db.test.ts` + `wiki-db.test.ts` 74/74 | `startup-integrity.test.ts` 12/12（新增 6 个，先红 4 个后绿）；备份相关 4 个文件 53/53 |
| 全量 `pnpm test:unit` | 311 文件 / 2696 用例通过 | 311 文件 / 2699 用例通过 |
| 根 `pnpm typecheck` | 全部包通过 | 全部包通过 |
| `pnpm lint`、文件行数门禁 | 未单独跑 | 通过 |

**未验证**：0 字节库的报错在真实桌面上的呈现（沿用现有启动失败通道，没有改 UI，自动化环境不弹窗）；两个分支都没有在真实开发实例上跑过。

## 合并关系（已用 `git merge-tree` 离线演算，无冲突）

- 两个分支互不重叠：`fts` 动 `knowledge.ts`/`wiki.ts`，`empty-db-guard` 动 `init.ts`/`database/index.ts`，任意顺序、一起合都无冲突。
- 与 `spike/mygo`（PR #5）也无冲突。注意 `spike/mygo` 里带有同一个 FTS 修复的 cherry-pick（`ac0ed93a`，内容与 `43f10d0c` 相同），所以**先合 FTS 分支再合 PR #5 不会重复**；但 PR #5 是草稿且不建议合并，FTS 修复应走本分支。

## 建议的合并方式（由你在切换版本时执行）

用户的 `electron:dev` 可能在跑，原目录不适合在服务运行时改源码。等应用关掉后，在 `D:\Project\GuiZhi` 里：

```powershell
git status                      # 确认工作区干净
git merge --no-ff fix/fts-backfill-not-in
git merge --no-ff fix/empty-db-guard
pnpm typecheck; pnpm test:unit  # 合并后再跑一遍
```

想保持线性历史则把 `merge` 换成 `cherry-pick 43f10d0c 4442e023`。两个分支的提交说明已按仓库约定写成中文。合并后可删除 worktree 与分支：`git worktree remove D:\Project\GuiZhi-fts-fix` 与 `D:\Project\GuiZhi-zero-db`，再 `git branch -d` 两个分支。

## 不在本次范围

`PRAGMA quick_check` 不比较索引与表内容是否一致；这是已知限制，没有处理。
