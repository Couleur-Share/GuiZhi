# 归知收集 · HarmonyOS 原生第一版

目标真机：华为 Mate 80 Pro Max，HarmonyOS 7.0.0.109 SP6（用户提供）。

当前交付是 **已通过鸿蒙编译的源码和未签名 HAP**。2026-09-26 使用华为 Command Line Tools 26.0.0.851、HarmonyOS SDK 26.0.0.105 完成 ArkTS、资源编译与 HAP 打包。用户已通过 HoKit 1.8.9 为 0.1.0 签名并安装到目标真机，截图确认主页面成功打开；配对、系统分享与端到端投递仍待真机验收；2026-09-27 中转原生接口已部署并通过公网合成链路验证。本地构建脚本仍输出未签名包。

## 第一版范围

1. 扫码或粘贴桌面二维码链接，显示服务地址，用户确认后配对，等待电脑批准。
2. ArkUI 收集、近期记录、连接三个页面，跟随系统浅色/深色主题，沿用归知图标。
3. UIAbility 冷启动/再次拉起接收系统分享的文本与链接；先保存到草稿，再确认发送，可编辑尚未发送的草稿。
4. 本地草稿队列、手动重试和回执状态。请求先落盘，结果不确定时沿用原编号；不会自动后台常驻或悄悄重投。
5. 系统 Asset Store 保存设备凭证，禁止同步和明文回落；服务端撤销设备、本机清除连接。

正文阅读、知识库同步、图片/文件分享、系统推送不在第一版范围。

## 工程与构建

本目录是独立 DevEco Stage 工程，不是 pnpm 包；不改变桌面工作区的依赖和构建。纯协议与队列逻辑放在 `entry/src/main/ets/core/*.ts`，由真实客户端和 Node 测试共同执行；ArkUI 与系统能力在 `.ets` 中。

模板保留 **HarmonyOS 6.1.0(23)** 为最低兼容基线，0.1.3 起将 targetSdkVersion 升至 **26.0.0**，启用 ArkUI 原生沉浸光感。使用 API 23 的 `HttpRequestOptions.maxRedirects=0` 禁止凭证随重定向外发。新材质与悬浮接口在调用前检查 `apiAvailable` 是否存在及 `apiAvailable('26.0.0')`；旧版本走普通系统页签。最低兼容配置与本地逻辑检查不等于旧系统真机验收，目标设备仍为用户的 HarmonyOS 7 手机。

1. 从[华为官方下载中心](https://developer.huawei.com/consumer/cn/download/command-line-tools-for-hmos)获取 DevEco 或带 SDK 的命令行工具。本机命令行工具已解压到 `D:\Tools\HarmonyOS\26.0.0.851\command-line-tools`。
2. 运行 `powershell -NoProfile -File .\scripts\build.ps1`。脚本仅在缺失时复制 `build-profile.template.json5` 为本地配置；不覆盖已有签名配置。缺工具时退出码为 2。
3. 在 DevEco 打开本目录，完成 Sync；或运行 `powershell -NoProfile -File .\scripts\build.ps1 -ToolsRoot 'D:\Tools\HarmonyOS\26.0.0.851\command-line-tools' -Build`。脚本先执行 `ohpm install`，再构建 HAP，仅为当前进程配置工具环境。
4. 在 DevEco 的 Signing Configs 中用自己的华为开发者账号配置调试签名，连接真机并批准 USB 调试，执行 Run。未签名 HAP 不能当成可直接分发的安装包。

本机已验证的替代安装路径：HoKit 1.8.9「一键签装」导入 HAP → 初始化检查 → 签名安装。后续更新沿用现有签名材料，先覆盖安装，不主动卸载或重置证书，以保留草稿和连接；若失败，先检查具体错误。

`build-profile.json5`、证书、私钥、IDE 配置和构建产物已在本目录忽略；提交的是无签名模板。不要把自己的签名配置复制进模板。

未签名产物：`entry/build/default/outputs/default/entry-default-unsigned.hap`。工程没有签名配置，需要在外部签名后安装。构建仍会提示若干系统 API 的异常处理/设备能力检查警告，以及 PreBuild 阶段的模块 SemVer 警告（工程声明为合法的 0.2.1，原因待核对）；异步失败由上层操作捕获，但相关能力仍需真机确认。重复资源及弃用解码 API 的警告已修正。

## 中转接口

2026-09-27 已更新 `capture.couleurapp.com` 中转服务，`GET /v1/meta` 返回 `nativePairing: true`；原生配对、确认、投递、回执和撤销通过公网合成 API 验证。桌面端协议不变，原网页 Cookie 配对继续保留。手机需重新发起确认；二维码超过五分钟时，在管理连接中仅清除本机连接，再扫描电脑新生成的二维码（本机草稿保留）。真实手机与桌面接收仍待验收。

| 接口 | 作用 |
| --- | --- |
| `GET /v1/meta` | 返回新增能力标记 `nativePairing: true` |
| `POST /v1/pairings/native-claim` | 与网页 claim 使用同一 nonce/过期/幂等/桌面确认机制；拒绝 Origin、Cookie、Sec-Fetch-Site；不设置 Cookie |
| `GET /v1/session` | 原生端通过 Bearer 检查是否已确认 |
| `DELETE /v1/session` | 手机撤销自身及其快捷指令，允许取消待确认配对 |
| `/v1/captures`、`/v1/history` | 复用现有 v1 投递、幂等、权限与结果回执 |

原生入口不是凭请求头识别可信应用：真正授权仍来自二维码随机 nonce、设备凭证和桌面确认。所有写请求仍要求 JSON 与 `X-Guizhi-Protocol: 1`；网页 CSRF 校验不放宽。

配对开始前将凭证与 nonce 写入系统安全存储，响应丢失时可重试。不会读取浏览器 Cookie，也不会携带桌面收件箱凭证。草稿留在应用本地 Preferences；卸载/清理数据会丢失草稿。已尝试发送的草稿绑定原配对，重新配对后禁止直接投向另一台电脑。

## 可复现验证

从仓库根目录执行：

```powershell
pnpm --filter @guizhi/capture-relay test
pnpm --filter @guizhi/capture-relay typecheck
```

测试覆盖真实 Fastify + 临时 SQLite 的原生配对、确认前拒收、幂等投递、桌面 ACK/进度、撤销与浏览器 CSRF；客户端同份核心逻辑覆盖落盘失败、响应丢失、重启、分享不覆盖输入、编辑保护、错误回执和混合结果。

这些测试不模拟或声称证明 Asset Store、Scan Kit、ArkUI、Share Kit 的真机行为。鸿蒙 UI 不适用桌面 `pnpm shot`；必须由鸿蒙模拟器/真机补齐截图与交互验证。

## 真机验收进度

2026-09-26，依据用户提供的 HoKit 日志和手机投屏截图：0.1.0（1000000）已签名安装成功，深色模式主页面可打开，收集输入框、模式按钮、空草稿列表及三个底部标签可见。该证据不代表草稿持久化、扫码、系统分享或发送流程通过。

截图暴露内容较少时滚动区域垂直居中，首页标题下有大块空白。0.1.1（1000001）将三页统一设为顶部对齐；用户已覆盖安装，后续截图确认内容上移，同时指出系统栏黑底与默认底部导航未适配。

0.1.2（1000002）重做三页视觉与系统区域适配：灰绿深浅主题、紧凑标题、输入与操作卡片、图标导航和空状态。`WindowAppearance` 开启沉浸式布局，系统栏透明，根背景铺满；通过窗口回调读取状态栏、挖孔与手势区尺寸，使用当前 UIContext 将 px 转 vp 后避让，旋转/窗口变化时刷新。主题变化时刷新系统栏文字颜色，键盘使用 RESIZE 并在弹出时收起底部应用导航。失败时恢复系统自动避让，不隐藏系统状态栏或手势条。

用户随后覆盖安装 0.1.2，2026-09-26 截图确认首页与上下系统区域背景连贯、图标底栏可见；深浅切换、键盘和横屏尚未验收。尝试通过 Computer Use 控制 HoKit 时，工具初始化报“failed to write kernel assets / 系统找不到指定的路径”，重置后仍失败，未执行自动安装或生成假验收截图。需覆盖安装后检查深浅色、上下安全距离、输入法展开/收起、横屏和大字体。

0.1.3（1000003）改用原生 `Tabs` / `BottomTabBarStyle`：底部悬浮胶囊使用 `uiMaterial.ImmersiveMaterial(THIN)`，开启阴影、交互形变和点光源；`adaptToHandedness` 交给系统处理握姿。只对支持的设备构造材质，不强制设备等级。开启模块级 `ohos.arkui.UIMaterial.state=enable`，让已有删除/解绑确认弹窗及文本选择菜单使用系统材质。主内容卡片仍使用语义颜色，未在不支持的内容区域强加玻璃效果。

三页滚动内容底部留出浮栏净空，根布局仍统一避让真实手势区；键盘出现时清除浮栏及遮罩，并将页签高度归零，收起后恢复。保留三个页面的本机草稿、配对、手动发送行为。

0.1.3 已通过 ArkTS、资源与 HAP 打包，包内版本、API 基线、材质开关和 ABC 文件均核对通过。对同份材质修饰器运行隔离桩检查：旧系统无版本接口、版本不足、正常材质、键盘展开/收起、不支持材质的设备，共六种情况通过；不能证明系统渲染和真机兼容。新增 API 的版本告警已消除，原有存储/窗口等异常处理提示仍存在。尚未安装 0.1.3，实际光效、握姿行为、键盘切换与弹窗显示需要真机确认。

以下项目仍待执行（其中 0.1.0 签名安装和主页面启动已确认）：

1. 签名安装、亮/暗主题、窄屏/大字体、重启恢复草稿；扫码后桌面确认；拒绝/过期/撤销后提示可理解。
2. 浏览器、微信、小红书分别测试系统分享入口可见性、冷启动/热启动、原文及 URL 参数完整性；来源不提供系统分享时保留复制粘贴入口。
3. 飞行模式保存、联网后手动发送、提交响应丢失后重复点击/重启重试；同一请求只生成一条投递。
4. 电脑关闭时连续收集，电脑上线后核对处理结果；混合成功/失败/重复逐项显示，失败不报成功。
5. 解除配对和本机清除连接；安全存储不可用时不产生明文凭证；确认重新配对不会自动发送旧电脑的未决请求。

## 0.2.0 交互重构与设置页

主参考为 [ClashBox](https://github.com/xiaobaigroup/ClashBox)：检查了公开发行记录（1.7.4）和 master 分支的 `TopBar.ets`、`Configuration/ConfigAdd.ets`、`Settings/Appearance.ets`，采用单一页面标题、分组列表、原生新增面板与次要操作菜单的交互思路。其 main 分支主要提供发布说明，多数源码在 master；未复制其代码、图标或依赖。另检查 Liny 的冲浪喵的发布记录和组件组织；未采用久未更新的 STUFFS_NEXT 作为主参考。

首页以草稿列表为主，新建/继续输入进入原生编辑面板；保存到收集箱后由用户确认发送。收起面板前会再次等待输入落盘，失败时保留面板并显示可复制原因。草稿按最近新增优先展示，支持展开原文、操作菜单、未连接时进入配对；结果不确定的草稿仍禁止改写。近期记录隐藏默认可见的请求编号，详情可展开复制，失败原因保持可见。连接页显示三步配对说明，备用链接表单按需展开。页面拆出 `CaptureEditor`、`DraftCard`、`ReceiptCard`、`SettingsPage` 组件。

独立第四个页签「设置」包括：

- 外观：跟随系统/浅色/深色；材质系统推荐/轻薄/均衡/厚实/关闭；按压光效与形变；智感握姿。
- 收集：下一条新内容的默认识别方式；列表 3/6 行预览；编辑字数显示。
- 连接与隐私：电脑连接入口、本机数据/系统凭证/中转明文边界说明。
- 关于：版本信息、恢复默认偏好；重置前确认且不删除草稿或解绑电脑。

设置通过独立 Preferences 名称 `guizhi_capture_settings_v1` 保存，不接触队列状态与凭证。写入成功后更新界面；读取损坏数据时停止写入并提供重试。颜色偏好调用应用级 `setColorMode`，系统栏颜色继续由 `WindowAppearance` 跟随配置变化刷新。材质应用于底栏、编辑面板、应用操作菜单与确认弹窗；系统推荐为底栏 THIN / 面板 THICK，实际强度服从系统设置与设备能力。关闭时底栏恢复普通样式，弹层使用空材质。模块材质开关改为 default，由应用组件显式配置，避免全局 enable 覆盖用户关闭选择。系统自带文本复制菜单采用系统默认外观。

0.2.0（1000020）已完成 ArkTS/资源编译及未签名 HAP 打包。设置服务和材质兼容测试 6 项通过，原生队列/协议回归 7 项通过；行数门禁与差异空白检查通过。测试替换系统接口，只证明设置序列化、失败传播、版本判断和材质参数；不代表 Preferences 真机重启、ArkUI 渲染、输入法和系统主题切换已通过。新增偏好服务仍有系统 API 异常处理提示，失败由调用层捕获。

用户最新截图已确认 0.2.0 的列表首页、四个页签和悬浮底栏显示，反馈整体外观明显改善；过渡不足和握姿无明显反应进入 0.2.1 修正。真机仍需检查：保存/收起/重新打开编辑面板、键盘遮挡、菜单删除确认、切换材质后关闭/恢复、重启保留设置与草稿，以及深浅主题下系统栏对比度。

```powershell
node --test apps/capture-harmony/tests/preferences.test.cjs
pnpm --filter @guizhi/capture-relay exec tsx --test tests/harmony-core.test.ts
```

## 0.2.1 动效、握持识别与官方材质名称

用户截图确认 0.2.0 首页运行后，本次保留布局，打磨实际交互：

- 页签点击与页面内导航使用 Tabs 控制器和 260ms 原生过渡；草稿新增/移除、展开/收起、回执详情、配对表单及提示条增加短淡入和位移。新建入口增加轻按反馈，展开按钮命中高度统一为 44vp，设置选项整行可点。
- 设置增加「减少动效」：关闭本应用配置的过渡、位移和按压形变，保留系统弹层自带动画；光效选择不被覆盖。旧版本偏好缺少该字段时使用默认值，不自动重写存储，不重置原主题和材质。
- 材质名称按官方 `ImmersiveStyle` 五档显示为「超薄、薄、常规、厚、超厚」，补齐超薄与超厚；另有「自动（按组件）」和「关闭」。自动仍使用底栏薄 / 面板厚，旧 thin/regular/thick 设置值原样保留。实际显示依赖设备和系统光感设置。
- 智感握姿接入 `motion.on('holdingHandChanged')`，声明 `ohos.permission.DETECT_GESTURE`（normal / system_grant，无需运行时授权弹窗）。稳定 280ms 后，新建按钮移向识别到的左/右手；双手或未握持回到默认位置。设置显示真实识别状态、能力/权限错误和重新检测入口，不伪造成功反馈。
- 握持订阅与 UIAbility 前后台及页面生命周期绑定：关闭功能、离开前台或销毁页面时取消订阅和待执行切换；状态只在内存中使用。取消订阅失败后停止响应，重启监听复用原回调，避免重复注册。

原握姿开关仅控制 `barFloatingStyle.adaptToHandedness`。查阅 [OpenHarmony Tabs 实现](https://github.com/openharmony/arkui_ace_engine/blob/3a8b490538def485c73dd6dc5f0de85a84a18e96/frameworks/core/components_ng/pattern/tabs/tabs_pattern.cpp)，该路径读取触摸事件的操作手，并要求适合跟手的宽屏布局（源码注释为宽度大于 600vp）；它不等于订阅手机的握持事件。因此本次保留原生宽屏底栏适配，额外独立接入握持识别驱动手机新建入口。该公开实现用于解释约束，不能证明华为闭源系统内部完全相同。

交叉参考 [ClashBox HoldingHandleUtil](https://github.com/xiaobaigroup/ClashBox/blob/master/entry/src/main/ets/common/utils/HoldingHandleUtil.ets)、[华为握持感知最佳实践](https://developer.huawei.com/consumer/cn/doc/best-practices/bpta-matext-guide)、[官方材质枚举](https://github.com/openharmony/docs/blob/master/zh-cn/application-dev/reference/apis-arkui/arkts-apis-uimaterial.md)和[官方权限清单](https://github.com/openharmony/docs/blob/master/zh-cn/application-dev/security/AccessToken/permissions-for-all.md)。实现独立编写，接口和权限同时核对本机 SDK 声明。

设置与系统边界测试共 12 项通过，覆盖旧偏好迁移、五档枚举映射、关闭/恢复材质、减少动效、握持去重/防抖、停止后延迟事件、权限与能力失败。这些测试替换了系统接口与计时器，不证明真机识别率或动画流畅度。0.2.1（1000021）完成 ArkTS、资源编译与未签名 HAP 打包，仍需 HoKit 沿用现有签名覆盖安装。

覆盖后可在约 1 分钟内检查：进入设置，左右手分别自然握持，观察「智感握姿」状态；回到收集页检查新建按钮位置；切换页签、展开草稿和配对表单，再开启「减少动效」对比。若识别状态一直等待，本版会如实显示，不能把编译成功视为目标设备握姿验收通过。暂未进行本版本真机安装、渲染或传输流程验收。

### 可切换主题色（2026-09-27）

默认主题色从绿色改为清爽蓝；设置中将「主题色」与「深浅模式」分开，提供清爽蓝、柔和紫、玫瑰、琥珀、中性灰五种配色、选中标记和配色预览。交互参考 [ClashBox 外观设置](https://github.com/xiaobaigroup/ClashBox/blob/master/entry/src/main/ets/components/Settings/Appearance.ets)，颜色资源与组件独立实现。

通过同一语义配色更新按钮、导航、开关、草稿、回执、编辑面板和提示；深浅模式仍跟随应用/系统，删除操作保留错误色。新偏好 themeColor 仍存入独立设置区，旧设置缺失该字段时默认蓝色，不改写原存储，也不重置草稿或连接。保存成功后才更新界面，恢复默认回到蓝色和跟随系统。

15 项偏好与系统边界测试通过，包括旧设置迁移、五色持久化、恢复默认、非法配色和失败写入；所有配色在深浅模式下的按钮文字、强调文字和提示底色对比度均达到 4.5:1。独立临时源码副本通过 ArkTS、资源编译和未签名 HAP 打包。资源配色预览不等于 ArkUI 真机截图；切色即时刷新、重启保留、深浅切换和编辑面板仍需覆盖安装后验收。

## GitHub 实现参考（2026-09-26）

阅读应用源码和官方示例后独立实现，未复制第三方应用代码或资产：

- [orange-cloud / MainShell](https://github.com/chen2he/orange-cloud/blob/6ac763d357b2cabb86bffe24c1770a333462af95/apps/harmonyos/entry/src/main/ets/views/MainShell.ets)：HDS 系统悬浮页签、握姿适配和内容从浮栏下方透出。参考其布局思路；归知使用 SDK 26 的 ArkUI 路径。
- [Petrelgram / hdsTabsFloating](https://github.com/miramira8295/Petrelgram/blob/054afef5b9743258570709bef8a1d343a4c02367/entry/src/main/ets/util/hdsTabsFloating.ets)：版本判断放到属性修饰器中，避免旧系统调用不存在的方法；区分页签宽度和浮栏宽度，防止图标溢出胶囊。
- [NextE / AppPrompt](https://github.com/erosTeam/NextE/blob/a842a9ab893dcf0dc7030f731cfc85ae6a1d6451/shared/src/main/ets/utils/AppPrompt.ets)：API 26 材质分场景使用，弹层内部避免实色覆盖系统材质。
- [OpenHarmony Tabs 官方示例 24](https://github.com/openharmony/docs/blob/f41b9345badd47c7ab0c263344cd7f4b5a549afb/zh-cn/application-dev/reference/apis-arkui/arkui-ts/ts-container-tabs.md)：交叉核对 `barFloatingStyle`、握姿、光效及悬浮生效条件；最终编译依据本机华为 SDK 声明。

[MemoFlow](https://github.com/spring-soft/MemoFlow) 的 README 区分 HDS 底栏真实材质与普通组件的模糊回退，可作视觉参考；其声明“仅限个人使用”，不引入其源码/资源。

桌面服务卡片、ShareExtensionAbility 分享半模态、意图框架/小艺入口尚未实现。下一阶段优先验证现有系统分享到草稿的真实链路，再评估服务卡片快捷收集；本版没有把这些能力算作已完成。

## API 依据

- [华为开启沉浸光感](https://developer.huawei.com/consumer/cn/doc/harmonyos-guides/arkts-immersive-light-sense-enable)：目标 SDK 与模块开关；效果受组件位置、设备能力和系统设置影响。
- [华为 Share Kit 分享内容接收](https://developer.huawei.com/consumer/en/doc/harmonyos-guides/share-sec-panel)：数据读取使用 `systemShare.getSharedData`。本版使用主 UIAbility 展示完整收集页，未实现 ShareExtensionAbility 嵌入详情页。
- [华为 Scan Kit 默认扫码](https://developer.huawei.com/consumer/en/doc/harmonyos-guides-V13/scan-scanbarcode-V13)：`startScanForResult`，仅使用系统扫码入口。
- [OpenHarmony HTTP 声明](https://github.com/openharmony/interface_sdk-js/blob/master/api/@ohos.net.http.d.ts)：API 23 `maxRedirects`，最终以安装的 HarmonyOS SDK 声明为准。
- [OpenHarmony Asset 声明](https://github.com/openharmony/interface_sdk-js/blob/master/api/@ohos.security.asset.d.ts)：`DEVICE_UNLOCKED`、`SyncType.NEVER` 与凭证查询/写入。

许可证延续仓库根目录 AGPL-3.0 与 NOTICE；没有复制第三方应用源码。

## 0.2.2 配对等待与恢复

- 手机确认配对立即显示当前阶段、已等待秒数；超过 5 秒提示网络响应较慢，结束后明确显示成功或可复制的错误。
- 待确认状态每 2 秒查询，回到前台立即检查；请求不重叠，后台停止轮询，成功后停止自动查询。网络故障按 2–30 秒退避恢复，限流冷却 60 秒；明确鉴权错误停止自动重试。
- 状态查询总时限 8 秒，其他请求总时限 20 秒。重试只有遇到明确的 HTTP 401 才重新提交；网络超时不会被当作未提交，也不会清除已有凭证。
- 收到电脑批准后立即显示绑定成功，自动检查不再等待历史记录。新操作开始后忽略旧状态响应。
- 桌面源码同步修复确认成功后仍显示旧错误/待确认项的问题，并将成功提示移到配对卡片顶部；桌面修复需要后续桌面更新才能进入已安装版。

验证使用真实页面控制方法的 Node 测试（系统边界替身）、中转合成链路和 Hvigor 编译。未完成鸿蒙真机的网络耗时、ArkUI 显示和覆盖安装验收，不能据此保证两秒内必定绑定。版本为 0.2.2（1000022），构建输出未签名 HAP；沿用现有签名覆盖安装，保留连接和草稿。

## 0.3.0 统一交互与动效

按用户要求直接阅读 ClashBox master 源码，完成四个手机端入口的交互整理，完整参考路径和验收边界见 [ClashBox 设计对照](docs/clashbox-design-reference.md)。

- 设置改为外观与动效、收集偏好、连接与隐私、关于归知四个入口；使用原生 Navigation 二级页面，进入时收起底栏、返回恢复。
- 主页面统一单一标题和 44vp 图标操作；列表图标、卡片、留白与详情展开统一；近期增加原生下拉刷新，刷新结束或无法刷新时收起状态。
- 编辑面板使用三段收集方式，保留输入自动落盘、保存后再确认发送、失败时不关闭面板的行为。
- 新增轻快/标准/舒缓三档应用动效、悬浮/贴底底栏；springMotion 与按压反馈集中管理，列表错峰延迟封顶；减少动效保留原速度偏好并去除应用配置的位移、缩放和等待。
- 设置菜单标记当前选项，沿用清爽蓝与五色主题、官方五档材质和握姿能力判断。旧偏好补齐新字段，不重置草稿或配对。

35 项相关测试及文件行数门禁通过。独立副本完成 ArkTS、资源和未签名 HAP 构建，版本 0.3.0（1000030）；本轮未完成真机渲染、手势与动效验收。沿用现有签名覆盖安装后，可先检查设置二级页返回、切换动画速度和下拉刷新。
