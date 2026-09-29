# 鸿蒙端真机命令行回路

目的：让“改代码 → 构建 → 安装 → 截图/读界面 → 点击 → 再截图”能在命令行里自动闭环，不依赖手工点按。
它与你现有的 HoKit / DevEco 回路**并行、互不覆盖**：

- 独立的开发包：包名 `com.couleurapp.guizhi.capture.dev`，桌面名称「归知收集 Dev」，数据与正式包完全分开，可与正式包并存。
- 独立的构建目录：`build.ps1 -Isolated` 在系统临时目录的副本里构建，不写入本目录的 `entry/build` 与 `.hvigor`。
- 独立的 hdc 服务端口 `18710`：不会结束或抢占 HoKit / DevEco 使用的默认服务（8710）。
- 默认拒绝操作正式包；确需操作必须显式加 `-Force`。

## 证据边界

| 项目 | 状态 |
| --- | --- |
| 脚本命令构造、正式包护栏、日志脱敏、布局解析 | 已由 hdc 替身测试覆盖（`pnpm test:harmony`，仅 Windows 运行） |
| 真实 hdc 可执行、独立端口生效、`server-stop` 清理 | 已在开发机验证（无设备连接时 `targets` 返回 `[Empty]`） |
| `install` / `launch` / `shot` / `dump` / `tap` 对真实手机 | **尚未验证**，需要完成下方“一次性准备”后由你确认 |

## 一次性准备（需要你操作）

1. **关闭 HoKit**（或确认它没有占用 hdc）。同一台手机同一时间只交给一个工具操作；两个 hdc 服务同时抢 USB 时，后者会看到空设备列表。
2. 手机开启**开发者模式与 USB 调试**，用数据线连接电脑，在手机弹窗里点**允许调试**。
3. 为 **dev 产品**生成调试签名（签名必须与包名 `com.couleurapp.guizhi.capture.dev` 匹配，正式包的签名不能复用）：
   - DevEco Studio → File → Project Structure → Signing Configs，选择产品 `dev`，勾选自动生成签名（需要登录华为开发者账号，且当前手机在调试设备列表内）。
   - 也可手动填写已有的 `.p12` / `.cer` / `.p7b`，但其 Profile 必须绑定上述包名。
   - 签名信息只写入本机 `build-profile.json5`（已被 `.gitignore` 忽略，不会提交）；`build-profile.template.json5` 保持无签名。
   - 若你的本机 `build-profile.json5` 是旧版（没有 `dev` 产品），`build.ps1 -Product dev` 会提示对照模板合并，请保留已有的 `signingConfigs`。
4. 验收：下面“日常回路”的第 2 步安装成功，且 `shot` 得到一张能打开的 JPEG、`dump` 输出界面文字。

## 日常回路

以下命令在仓库根目录执行；`<工具根>` 是命令行工具目录（示例：`D:\Tools\HarmonyOS\26.0.0.851\command-line-tools`）。

```powershell
# 1. 构建开发包（隔离副本，产物复制到 artifacts/harmony/hap/dev/）
powershell -NoProfile -File apps/capture-harmony/scripts/build.ps1 -ToolsRoot '<工具根>' -Product dev -Isolated -Build

# 2. 安装（覆盖安装，保留开发包自己的数据）并启动
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 targets -ToolsRoot '<工具根>'
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 install -ToolsRoot '<工具根>'
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 launch -ToolsRoot '<工具根>'

# 3. 观察：截图落在 artifacts/harmony/shots/，布局落在 artifacts/harmony/dumps/
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 shot -Name home -ToolsRoot '<工具根>'
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 dump -Name home -ToolsRoot '<工具根>'

# 4. 操作：按文字或坐标点击、返回键
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 tap -Text 近期 -ToolsRoot '<工具根>'
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 key -Key Back -ToolsRoot '<工具根>'

# 5. 日志（已脱敏；先 -Clear 再复现，再读取）
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 logs -Clear -ToolsRoot '<工具根>'
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 logs -Filter 'guizhi' -Lines 100 -ToolsRoot '<工具根>'

# 6. 收工：只结束本脚本自己端口上的 hdc 服务
powershell -NoProfile -File apps/capture-harmony/scripts/device.ps1 server-stop -ToolsRoot '<工具根>'
```

也可以设置环境变量 `HARMONY_COMMAND_LINE_TOOLS`（或 `GUIZHI_HDC` 直接指向 `hdc.exe`），省去每次的 `-ToolsRoot`。多台设备时用 `-Device <key>` 指定。

`launch` 默认启动 `EntryAbility`；`-Ability <名称>` 可启动同一个包里的其他 Ability（名称只允许字母、数字、下划线，脚本先校验再调用 hdc）。dev 包的分享诊断页用 `launch -Ability ShareDiagAbility`，用法见 [share-spike.md](share-spike.md)。

`inspect-hap.ps1` 只读检查任意 HAP 的包名、版本、Ability、扩展、路由页面、权限和资源文字，用来在不装机的情况下核对构建产物：

```powershell
# 开发包：应带分享诊断扩展
powershell -NoProfile -Command "& apps/capture-harmony/scripts/inspect-hap.ps1 -Hap artifacts/harmony/hap/dev/entry-dev-unsigned.hap -ExpectBundle com.couleurapp.guizhi.capture.dev -ExpectText '归知收集 Dev','分享诊断' -ExpectExtension ShareDiagExtAbility"

# 正式包：不得出现 Dev、任何扩展和分享诊断代码
powershell -NoProfile -Command "& apps/capture-harmony/scripts/inspect-hap.ps1 -Hap artifacts/harmony/hap/default/entry-default-unsigned.hap -ExpectBundle com.couleurapp.guizhi.capture -ExpectText '归知收集' -ForbidText 'Dev' -ForbidExtensions -ForbidCode 'ShareDiag','sharediag'"
```

`-ExpectText`、`-ForbidText`、`-ForbidCode`、`-ExpectExtension` 都接受多个值。用 `powershell -File` 传数组会被当成位置参数报错，所以多值时用上面正式包那种 `-Command "& …"` 的写法，或直接在已打开的 PowerShell 里用 `&` 调用。

## 使用开发包时的注意

- 开发包是**另一台设备**：需要在电脑「设置 → 手机收集」重新配对；每个邮箱最多 50 台设备，用完请在电脑撤销。
- 开发包发送的内容会**真的进入你桌面的收件箱**。测试文字请以 `[dev-test]` 开头，事后在电脑上删除；不要拿真实链接反复测试。
- 开发包与正式包各自保存草稿与设置，互不影响；卸载开发包不会影响正式包。
- 截图和布局文件可能含有界面上的正文，`artifacts/` 已被忽略，不要提交或外发。

## 故障排查

| 现象 | 处理 |
| --- | --- |
| `targets` 为空 | 确认已点“允许调试”；关闭 HoKit / DevEco 对该手机的占用；换线或换 USB 口；必要时先 `server-stop` 再重试 |
| 安装失败并提示签名或 profile 错误 | 签名与包名不匹配，或手机不在调试设备列表；按“一次性准备”第 3 步重新生成 dev 签名 |
| 找不到已签名 HAP | 构建产物是 `*-unsigned.hap`，说明 `build-profile.json5` 里 dev 产品还没有签名 |
| `dump` 失败 | 应用需要在前台并已解锁屏幕；先 `launch` |
| `shot` 全黑 | 手机处于息屏或锁屏 |
| 构建进程以 `-1073741819`（0xC0000005）退出 | Windows PowerShell 5.1 的杀软扫描接口（AMSI）偶发访问冲突，与代码无关，直接重跑 |
| 构建日志出现 `launcher_name conflict, first declared` | 预期提示：dev 资源目录排在前面，覆盖了桌面名称 |
