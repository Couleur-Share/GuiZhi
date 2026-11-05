# 德国机 Linux 预检

默认使用 Bero 上已存在的公共 `gs-preflight` Buildx/BuildKit 构建器。与 GatewaySentry 文档站共用执行器、GC 和内容寻址的 npm/pnpm 下载缓存，各仓库保持独立源码、依赖安装树、临时数据库和证据目录。无需安装第二套 Docker、builder、定时任务或执行器。

## 当前操作

1. 本机一次性在 `~/.config/gs-linux-preflight/client.json` 登记公共源码目录：`{"directory":"E:/Tools/Gatewaysentry-SSH/ops/linux-preflight"}`。这是非敏感路径；换电脑时配置自己的位置，也可通过 `GS_LINUX_PREFLIGHT_CONFIG` 指定配置文件。公共客户端和 `runner.py` 只有一个维护源。
2. `pnpm check:linux:prepare` 打包当前源码（未提交、新增、删除均计入，凭据和生成目录排除）。SoftMemo 准确提交可加 `--sha <完整SHA>`。输出 `prepared_only` 只代表打包；普通 `check:linux` 强制新检查，`check:linux:reuse` 缺少有效结果时打包并退出 2，等待 Agent 交接。
3. Agent 使用 XTerminal 实时列出服务器、选择 Bero，创建请求中的远程目录并上传 `request.json`；确认传输 completed 后执行 `python3 /usr/local/lib/gs-linux-preflight/runner.py plan <request.json>`。`upload_required` 才上传 `input.tar.gz`，确认 completed 后执行 `runner.py run <request.json>`；`busy` 等待原任务，不重复启动。
4. 下载完整 output（含公共 `receipt.json`、项目结果及原有日志/trace）到本机运行目录；远端 reused 回执中的 `evidenceDirectory` 指向原完整证据，无需再复制服务器产物。执行 `pnpm check:linux:verify --run <本机运行目录>`；失败、未完成、过期、规则/源码/镜像不匹配或关键验收缺失都会失败。

成功回执有效期 24 小时，最新同源码失败或未完成阻止旧成功复用；公共执行器变化同样使回执失效。公共构建器不发布端口、不挂载宿主凭据、生产目录或 Docker socket。BuildKit 缓存 GC 目标 5 GiB（非硬配额），成功证据 7 天、失败 30 天，未知/未完成目录保留，证据达到 5 GiB 时停止新构建。只回收公共工具拥有的完成记录，不全局 Docker prune。

原有检查规则保持在 `scripts/wslc/run.sh`、`checks.sh` 及验收脚本中。依赖阶段使用原有固定镜像、pnpm 11.1.0 和 frozen lockfile；成功的依赖层可复用，源码检查独立运行。环境准备只在已由依赖层成功完成时跳过重复安装。`pnpm test:wslc` 继续验证快照、凭据排除和暂存区保护。

Node 24、shared/capture-web/capture-relay 类型检查、中转全量单测、两端构建、真实进程健康与网页、临时 SQLite 重启持久性验收全部保留。默认检查时限 900 秒，`CHECK_LINUX_TIMEOUT` 仍接受 1–86400 秒并进入请求回执；镜像/包准备另有最多 600 秒余量。Windows 桌面隔离验证和 Harmony 构建/设备验收不变。

## 已退役本机 WSLC 的历史参考

以下仅保留原本机运行方式与历史验收。2026-10-27 本机 WSL/WSLC 已卸载，`check:linux:local` 别名已移除，`--local` 和直接启动旧脚本均返回非零；不得按历史内容自动重装或恢复会话。当前可用操作以上方德国机步骤为准，打包成功不代表门禁通过。


在 Windows 的仓库根目录运行：

    pnpm check:linux

Node 24/Linux；shared、capture-web、capture-relay 类型检查，中转服务全量单测（包含 Harmony 共用逻辑），网页与服务构建，真实进程 /healthz、静态网页与临时 SQLite 数据库重启持久性检查。

首次使用需要下载固定 Node 镜像与 pnpm 11.1.0，随后复用镜像与项目独立 pnpm store。输入为 Git 列出的当前工作区文件，包含未提交、新增及删除文件；明确排除 .env*、.npmrc、私钥、数据库、node_modules 与生成目录。若项目需要私有 registry，应单独设计凭据注入，不能直接复制宿主 .npmrc。每个项目可独立运行，不依赖 GatewaySentry 检出。

证据保存在 artifacts/wslc-linux/<运行编号>/output/，包括 source.json（基准提交、文件清单与 SHA-256 指纹）、validation.log、result.json（成功/失败阶段、退出码、环境和耗时），成功时另有 build-artifacts.tar.gz。脚本失败返回非零状态，正常结束/失败/中断只清理本次唯一名称的容器，证据保留。启动前失败可能没有容器日志，强制关闭终端后需检查遗留容器。

容器运行有整体时限，默认 900 秒（暖启动实测约 35 至 70 秒，首次下载镜像与依赖需要余量）；可用环境变量 `CHECK_LINUX_TIMEOUT`（1 至 86400 的整数秒）调整，无效值直接报错而不回退默认值。超时后停止本次唯一名称的容器并以非零状态退出，错误信息为 `Linux check exceeded the overall limit of N s and was stopped.`，清理结果同样写入 `output/cleanup.json`。时限只覆盖容器运行阶段，不含快照与会话启动。

AGENTS.md 规定相关源码变化后的交付前自动执行；这是 Agent 验证规则，不是常驻后台服务。纯文档改动无需重跑。检查通过不等于浏览器、完整镜像、真实数据库或生产验收。

预检工具自身的快照边界测试：

    pnpm test:wslc

Linux CI 直接运行项目原生检查，此入口仅用于 Windows WSLC。

## WSLC 会话与残留回收

2026-10-06 起，预检读取本机 `%LOCALAPPDATA%/wslc/preflight.json`，固定连接 `couleur-preflight`，复用现有 `wslc-cli-g2014/storage.vhdx`。普通/管理员入口均不再隐式创建各自的默认会话，也不修改 Windows UAC。配置必须指向已存在的虚拟盘；缺失、锁定或会话查询失败时停止，不创建备用盘。

首次需要时启动隐藏的会话持有进程；四个项目通过短期租约共享会话。所有启动器退出且容器清单确认为空后，会话持有进程自动结束。没有新增开机任务或定时任务。若进程在启动锁持有期间被强杀，需人工核对 `preflight-runtime/bootstrap.lock` 的 PID；不会擅自绕过锁。

每次创建容器前写入项目证据目录下的 `owners/<UUID>.json`，容器附带项目路径指纹与运行 ID 标签。下次完整预检只回收本项目中启动器已退出、标签完全匹配的遗留容器。PID 仍存活（包括复用）或权限不明时保留；未知归属和无记录的旧容器不自动删除。来源路径含链接、记录损坏或回收无法确认时停止新运行。

正常、失败与中断后均查询容器清单确认已移除，并写入 `output/cleanup.json`。删除命令成功不等于回收成功；清单超时、格式错误或容器仍存在都会返回失败。历史证据、镜像、依赖缓存和虚拟盘本身继续保留，没有全局 prune 或历史产物保留策略变更。
