# 冻结的对拍基准（golden）

Go 壳的差分测试把 Go 实现与原 Electron/TS 实现逐项对拍。这里是 TS 实现生成的基准输出，**已冻结进仓库**：
`go test` 默认读取本目录，不再需要 TS 主进程、`pnpm`、`tsx`，也不再因为没设环境变量而静默跳过。

## 来源

- 由 Electron 终版（标签 `electron-final-0.28.x`，分支 `electron-final`）的 TS 代码，加上 Go 壳分支 `refactor/go-mainline`
  上的生成脚本（`spikes/mygo-db/`、`spikes/mygo-ai/`）生成，日期 2026-11-13。
- 冻结当天用这批基准做过一次全量对拍：298 通过、12 跳过（均为需联网/真机/压测的手动项）、0 失败。
- 生成时的绝对路径里出现的本机用户名已统一改写为 `user`（`C:\Users\user\...`）。
- 这些文件按字节比较（含 CRLF 正文、SQLite 与备份文件），`.gitattributes` 对本目录设置了 `-text`，不要让编辑器或 git 改行尾。

## 目录与环境变量

`main_test.go` 的 `useFrozenGoldens` 在环境变量未设置时，把下表的变量指向对应子目录。显式设置了变量则以变量为准。

| 环境变量 | 子目录 | 内容 |
| --- | --- | --- |
| `GUIZHI_GOLDEN_DIR` | `main/` | 主基准 `golden.json`、`evidence.json` 与各场景的 SQLite 夹具（库、检索、导入、备份、设置、MCP、更新器、FunASR 等） |
| `GUIZHI_FORUM_GOLDEN_DIR` | `forum/` | 论坛连接器 |
| `GUIZHI_MEDIA_GOLDEN_DIR` | `media/` | 媒体与 AI 用量 |
| `GUIZHI_THEMED_GOLDEN_DIR` | `themed/` | 主题阅读渲染层 |
| `GUIZHI_THEMED_DB_GOLDEN_DIR` | `themed-db/` | 主题阅读数据层 |
| `GUIZHI_THEMED_IPC_GOLDEN_DIR` | `themed-ipc/` | 主题阅读同步频道 |
| `GUIZHI_THEMED_PIPE_GOLDEN_DIR` | `themed-pipeline/` | 主题阅读生成管线 |
| `GUIZHI_THEMED_RT_GOLDEN_DIR` | `themed-runtime/` | 主题阅读运行时 |
| `GUIZHI_VIDEO_GOLDEN_DIR` | `video/` | 视频连接器 |
| `GUIZHI_WEBSNAP_GOLDEN_DIR` | `websnap/` | 原文快照净化 |
| `GUIZHI_WECHAT_GOLDEN_DIR` | `wechat/` | 公众号采集 |
| `GUIZHI_WIKICOMPILER_GOLDEN_DIR` | `wikicompiler/` | Wiki 编译器 |

## 冻结后降级的断言

下列断言依赖「TS 当时留在磁盘上的那棵目录树」，换一台机器或换检出位置就不可能成立，冻结后降级为跳过（测试里会输出原因）：

- `TestFunasrGoldenFilesystem` 的目录身份（设备号 / inode / 创建时间）。
- `TestFunasrGoldenBackupManifest` 后半段（TS 写下的清单与身份）。
- `TestFunasrGoldenBackupCleanup`、`TestFunasrGoldenConsumesTsDisposable`：本来就是「只能对原地目录验证一次」的活对拍。

`TestAppRuntimeParity/log` 会在测试内固定 `USERPROFILE`，不依赖运行机器的用户名。

另外，基准在 UTC+8 的机器上生成，备份文件名、MCP 条目的 `updated` 日期等都按本地时区格式化。`TestMain` 把 `time.Local` 固定为 UTC+8，因此测试结果与运行机器的时区无关（在 UTC 的 CI 上不固定会差 8 小时、日期错一天，已实测复现）。重新生成基准时须在同一时区进行。

## 重新生成

TS 主进程退役后无法在主线重新生成。确需对照，从 `electron-final-0.28.x` 检出 TS 终版，再用 `pnpm dlx tsx`（需 tsx ≥ 4.23，
本地旧版 tsx 对 CJS 命名导出的处理不同会失败）运行 `spikes/mygo-db/make-golden.mts <目录>` 与各 `run-golden-*.mts <目录>`，
然后用上表的环境变量指向新目录运行 `go test`，与本目录对比。
