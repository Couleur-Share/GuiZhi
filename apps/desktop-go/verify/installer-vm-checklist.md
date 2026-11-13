# MyGo 壳安装包：虚拟机验收清单

**状态：清单与取证脚本已就绪，尚未在任何虚拟机上执行。** 安装包 `out/installer/GuiZhi-Setup-0.28.0-x64.exe` 只做过「构建 + 更新器接受 latest.yml + sha512 校验」，安装器本身的运行行为全部未验证。

## 为什么必须用虚拟机

安装包与现有 Electron 版共用固定 guid `fa5aa350-68bd-43b9-b440-a7ecb8fcc78a`。在开发机上运行会覆盖真实的卸载登记项、安装目录和 `Software\GuiZhi\InstallerState`。`installer-vm-probe.ps1` 检测不到虚拟机会直接拒绝采集。

## 环境准备

- 虚拟机：Windows 11 x64，建议 Hyper-V 或 VMware。所有场景开始前先做检查点，每个场景结束后回滚。
- 把以下文件复制进虚拟机，不要共享文件夹挂载宿主目录：
  - `GuiZhi-Setup-0.28.0-x64.exe`
  - `verify\installer-vm-probe.ps1`
  - 旧版 Electron 安装包（场景 B 用，来自 GitHub Release 的 0.27.x 或更早版本）
- 取证脚本用法（管理员或普通 PowerShell 均可，只读）：

```powershell
.\installer-vm-probe.ps1 -Action Snapshot -Out before.json
# …执行安装 / 覆盖 / 卸载…
.\installer-vm-probe.ps1 -Action Snapshot -Out after.json
.\installer-vm-probe.ps1 -Action Check -Before before.json -After after.json -Scenario fresh   # 或 overwrite / uninstall
```

`Check` 的断言覆盖：登记版本、`GuiZhi.exe` 存在且体积合理、签名状态 NotSigned、无 Electron 残留、`InstallerState` 登记、快捷方式、用户数据（`%APPDATA%\GuiZhi`）前后逐文件一致、卸载后各项清理。**脚本断言的是状态，下表中「人工观察」列的对话框与界面行为必须人眼确认并截图。**

## 场景

| 编号 | 场景 | 步骤 | 脚本断言（Scenario） | 人工观察 |
|---|---|---|---|---|
| A | 干净机器全新安装 | 无任何归知痕迹 → 双击安装包，按向导装到默认位置并启动 | `fresh` | SmartScreen 提示出现并能点「仍要运行」；无 WebView2 提示；安装后启动成功、能新建/看到界面；首次数据落在 `%APPDATA%\GuiZhi` |
| B | 覆盖旧 Electron 版 | 先装旧 Electron 版并造一条知识条目 → 脚本快照 → 运行新安装包 | `overwrite` | 弹出「检测到已安装的归知…建议先备份」，选「取消」应中止且旧版完好；选「确定」后继续；覆盖后旧资料仍在、能打开 |
| C | 静默覆盖（自动更新路径） | 同 B 的起点，命令行 `GuiZhi-Setup-0.28.0-x64.exe /S` | `overwrite` | 全程无对话框（备份提示与 WebView2 提示在静默模式下均跳过）；退出码 0 |
| D | 卸载 | 控制面板或 `Uninstall GuiZhi.exe` 卸载；先造数据 | `uninstall` | 卸载不询问也不删除数据；`%APPDATA%\GuiZhi` 原样保留；重装后数据仍在 |
| E | 缺少 WebView2 | 在已回滚的虚拟机里卸载 WebView2 运行时（Windows 11 默认自带，需手动卸载，或用 Windows 10 无 WebView2 镜像）→ 运行安装包 | 无（未安装） | 弹出「未检测到 Microsoft Edge WebView2 运行时…」；选「是」打开微软下载页并退出安装；选「否」也退出；不留下半装状态（再跑 Snapshot 应无卸载登记项） |
| F | SmartScreen / 未签名 | 在场景 A 里观察 | — | 记录 SmartScreen 的措辞与点击路径，写进发布说明（本版本按约定不签名） |
| G | 更新后启动 | 场景 B 后，用 `GUIZHI_REAL_INSTALLER_DIR` 等价的方式在虚拟机里让应用检查更新（需有可访问的 latest.yml 来源，**不要指向真实 GitHub Release**） | — | 更新器能识别当前版本、下载、校验 sha512、静默安装 |

## 验收结论的写法

每个场景记录：结果（通过/失败/未取得）、`Check` 输出原文、截图路径、虚拟机型号与 Windows 版本。**没跑的场景写「未取得」，不能因为脚本通过就推断人工观察项通过。**

## 已知边界

- `latest.yml` 不含 `size` 字段，更新器依赖 sha512，已在宿主机离线验证；真实安装后的更新流程只在场景 G 验证。
- 脚本不判断 SmartScreen、对话框文案与界面渲染，这些只能人工验收。
- 场景 E 在 Windows 11 上需要手动卸载 WebView2，部分 OEM 镜像卸载会失败；做不到时如实记录「未取得」，不要用伪造注册表的办法替代。
