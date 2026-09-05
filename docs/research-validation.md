# 近期研究增强交付与验证记录

验证日期：2026-09-05，Windows 本地工作区。未提交、推送、发布，也未对生产知识库执行试迁移。现有研究采集修复及其他未提交改动均保留。

## 七个审查单元

以下是逻辑审查边界，不是七个已经创建的 Git 提交。IPC、共享类型及数据库边界按依赖共同交付。

| 单元 | 主要实现 | 对应验证 |
| --- | --- | --- |
| 1. 证据准入 | shared 的 `research-policy.ts` / `research-analysis.ts`；采集作者 ID；日期分层 | `research-policy.test.ts`、`research-analysis.test.ts`、20 组中文样例 |
| 2. 查询覆盖 | `collect-research.ts`、`collectors.ts`、查询尝试持久化、`ResearchCoverage` | `bilibili-collector.test.ts`、研究恢复测试、离屏覆盖截图 |
| 3. 报告快照 | `report-evidence.ts`、`ResearchWorkflowDB`、主进程生成与取消接口 | 引用重排、无效编号、无引用结论、旧报告保留、迟到结果、原文 Markdown 引用注入回归 |
| 4. 主进程协调 | `research-ai.ts`、`budget.ts`、`ResearchService`、IPC / preload / store | 总页数预算、跨查询去重、规划失败回退、取消和恢复 |
| 5. 文字精读 | `read-research.ts`、字幕 cue、子进程终止、`ResearchEvidencePanel`、摘录保存 | `research-reading.test.ts`、`research-workflow.test.ts`、字幕临时目录清理与时间定位 |
| 6. 手动比较 | shared 的 `research-comparison.ts`、序列 / 基线、`ResearchComparisonPanel` | 新增、持续、窗口移出、覆盖不可比、计划变化、独立同名研究不合并 |
| 7. 本地关联与入库 | `local-evidence.ts`、已有向量 DAO 的范围过滤、共享 RRF / 向量解析、升级前备份 | 范围内归档、回收站排除、语义失败回退、异步范围二次校验、自引用排除、旧库备份和迁移 |

入口类型集中在 `packages/shared/types/research*.ts`；迁移追加为 0025 / 0026 / 0027，新库 schema 同步。研究正文、快照与序列由研究 DAO 管理，正式知识条目仍由现有知识 DAO 写入。

## 本地门禁结果

| 门禁 | 结果 |
| --- | --- |
| `pnpm lint` | 通过，含文件长度检查 |
| `pnpm typecheck` | 通过，全部工作区包 |
| `pnpm test:unit` | 182 个测试文件、1715 项测试通过；最后一次全量运行约 101 秒 |
| `pnpm --filter @guizhi/desktop build` | 通过，desktop / MCP 构建 |
| `pnpm --filter @guizhi/desktop bundle:budget` | 通过 |
| 离屏 Playwright `smoke.spec.ts` | 1 项通过：创建条目、搜索、手动备份 |
| `git diff --check` | 通过；Git 的既有 LF / CRLF 提示不是空白错误 |

全量统计包含工作区现有测试，不代表全部测试都由本次新增。最后一轮还覆盖了外部原文不能覆盖报告引用定义的回归。

## 离屏研究流程

运行：`pnpm --filter @guizhi/desktop exec node scripts/screenshot.mjs --steps tests/e2e/research-shots.mjs --out .tmp-shots/research`。

脚本启动隔离用户目录的真实 Electron，在该进程中替换研究 IPC 返回值，不调用真实平台或付费模型。已检查：

- 开启本地关联但未选择范围时，开始按钮禁用。
- 查询计划、窗口内数量、未知日期和采样上限可见。
- 点击报告引用可打开冻结摘录，显示字幕时间点及模型摘录边界。
- 比较页展示新增和无法判断，明确提示「未检索到不等于删除」。

截图输出：`apps/desktop/.tmp-shots/research/` 下的 `research-create-scope.png`、`research-coverage.png`、`research-evidence-reference.png`、`research-comparison.png`。截图已人工查看；未操作正在使用的可见归知窗口。

## 真实平台与真实模型的证据边界

| 平台 / 能力 | 时间、状态与结果 |
| --- | --- |
| B 站匿名公开搜索 | 北京时间 2026-09-05 16:39:19–16:39:20；未登录；`/x/web-interface/wbi/search/type`；查询「本地知识库」；第一页 5 条；HTTP 200，业务码 0，实际返回 5 条 JSON 搜索结果 |
| 小红书 / 抖音登录态精读 | 本次没有使用真实用户会话验证；解析、评论失败和取消流程使用隔离测试替身 |
| B 站真实字幕 | 字幕解析、发布者优先 / 自动字幕回退及清理已用替身验证；本次未下载真实视频字幕 |
| 真实模型规划 / 报告 | 本次未使用生产模型配置或付费调用；固定响应验证流程与格式，不等同于语义质量认证 |

上述 B 站结果只证明该次公开搜索返回数据，不证明时间窗内全部候选合格、字幕可用或平台完整覆盖。真实模型仍需逐段核对引用原文；不能把提示词存在、编号合法或固定响应通过作为事实正确的证明。

## 备份与回退

隔离文件库测试确认：备份发生在新增研究表之前；旧报告内容不变；迁移可重复运行；备份失败会在 schema 变更之前中止；可重新启动。恢复旧代码时使用兼容的升级前备份，不删除新字段作为逆迁移。研究删除不会级联删除已经保存的报告和摘录；保存报告的序列归属仍用于防止后续自引用。

正文配图、转写引擎和知识库筛选等并存改动不作为本次研究增强的交付内容，未做整树提交。
