# ClashBox 交互设计参考与归知落地

日期：2026-09-27。归知收集本地版本：0.3.0（1000030）。

用户要求直接参考 ClashBox 源码，范围是归知鸿蒙客户端的收集、近期、连接与设置，不扩展代理/VPN 业务。参考 master 提交 `e036a6a3097192ff74987ce7306eea2a03d6a3ac`，实现独立编写，没有引入 ClashBox 的源码、图标或组件依赖。

## 对照与落地

| 参考面 | ClashBox 源码 | 归知实现 |
| --- | --- | --- |
| 导航与页面层级 | [Settings.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/components/Settings/Settings.ets)、[Index.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/pages/Index.ets) | 设置首页四个入口，Navigation/NavDestination 原生二级页与返回；进入二级页收起底栏，返回恢复。保留归知四个主入口。 |
| 标题、图标、分组行 | [TopBar.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/components/Common/TopBar.ets)、[Common.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/components/Common/Common.ets) | PageHeader 单一标题、44vp 图标按钮；SettingsRows 统一图标、字号、选项值和选中标记；20vp 卡片圆角和一致页边距。 |
| 外观控制 | [Appearance.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/components/Settings/Appearance.ets) | 主题色与深浅模式独立；五色预览、悬浮/贴底底栏、五档材质、按压反馈与握姿设置；成功保存后应用。 |
| 动效与反馈 | [Animation.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/common/utils/Animation.ets) | InterfaceMotion 集中管理 springMotion、轻快/标准/舒缓 180/280/420ms、轻微缩放与位移、最长 96ms 的列表错峰；减少动效时关闭这些变形与等待。 |
| 新建面板与列表操作 | [ConfigAdd.ets](https://github.com/xiaobaigroup/ClashBox/blob/e036a6a3097192ff74987ce7306eea2a03d6a3ac/entry/src/main/ets/components/Configuration/ConfigAdd.ets) | 保留原生 bindSheet；编辑页使用明确的收起/保存操作和三段收集方式；近期增加原生下拉刷新，草稿/回执统一图标区与详情展开。 |

## 场景约束

- 仍然先保存在本机，用户确认后发送；不改队列编号、配对凭证、重试幂等或服务端协议。
- 收起编辑面板仍等待输入持久化，失败时保留面板与原因；失败回执和删除确认不能被纯视觉提示取代。
- animationSpeed 和 tabStyle 保存在原独立设置区，旧数据默认标准动效和悬浮底栏；保留原配色、材质、减少动效和收集偏好。
- 原生导航、弹层、滚动回弹和系统光感由鸿蒙控制；三档时长控制应用配置的动画，不宣称覆盖所有系统动画。原生光感仍按 SDK/设备能力判断，关闭材质时回退普通导航。
- 使用现有语义颜色与系统符号；默认清爽蓝。主题预览、分段选项提供选中语义，图标按钮保留可访问名称。

## 验证与限制

28 项设置/动效/配对控制测试和 7 项真实客户端核心逻辑测试通过；涵盖旧设置迁移、重启重载、失败写入、减少动效、底栏恢复、下拉刷新释放、配对响应竞争、离线草稿与幂等重试。系统边界使用替身，不等同于真机系统行为。

在独立临时源码副本中执行 Hvigor，完成 ArkTS、资源编译和未签名 HAP 打包；行数门禁通过。最终交付使用该副本输出，避免并行工作覆盖产物。

本轮没有有效的 ClashBox/归知真机界面截图，也没有将静态网页预览作为 ArkUI 验收。待覆盖安装后检查：设置二级页返回与底栏恢复、菜单选中、三档速度与减少动效、下拉刷新、键盘与编辑面板、大字体和深浅切换。手机无需为了源码参考保持解锁。
