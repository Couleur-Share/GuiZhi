# 采集平台

粘贴链接或分享口令即可采集。侧栏「平台」分区按来源分组；认不出专用连接器的 URL 走通用网页抓取。

| 类型 | 平台 | 能力概要 | 备注 |
| --- | --- | --- | --- |
| 视频 | 哔哩哔哩 | 元数据 + 音轨 → 转写 / 总结 / AI 标题 | 需 yt-dlp；认 `/video/` 与 `b23.tv` |
| 视频 | YouTube | 同上 | 需 yt-dlp；认 `/watch`、`/shorts/`、`youtu.be` |
| 视频 / 图文 | 抖音 | 视频转写总结；图文配图入库 + 逐图 OCR | **不走 yt-dlp**；分享页 SSR（`_ROUTER_DATA`）；可用分享口令整段粘贴；改版需跟修 |
| 图文 / 视频 | 小红书 | 图文配图 + OCR；视频笔记同链路 | **不走 yt-dlp**；笔记页 SSR（`__INITIAL_STATE__`）；须用分享面板带 `xsec_token` 的链接；改版需跟修 |
| 论坛 | **V2EX** | 正文 + 全部回复 + AI 讨论总结 | 走官方只读 API，无需登录 |
| 论坛 | **NGA** | 正文 + 全部回复 + 附件图入库 + AI 讨论总结 | guestJs 握手读公开帖；附件图上限 80 张；需登录的版块采不到 |
| 通用 | 网页 | Readability → Markdown | 无专用连接器的 URL 都落这里 |
| 通用 | 本地文件 | 文本 / 图片 / 音视频拖入即入库 | 媒体资产化，详情页可预览播放 |

## 分享口令

抖音 / 小红书等平台生成的「复制整段口令」可直接贴进快速采集框：归知会抠出链接并识别平台。提示栏会显示提取到的链接与将执行的动作，可一键改判（采集视频 vs 存成文本笔记）。

不支持省略协议的裸域名（如 `v.douyin.com/xxx`），以免把口令尾部噪音误判成链接。

## 未支持

- 其它论坛（Discourse、linux.do、Reddit 等）— 会退回通用网页抓取，效果通常较差
- NGA 需登录才能看的版块 / 帖子（公开帖经 guestJs 可采；平台 cookies 尚未实现）
- 需要登录才能看的内容（平台 cookies 采集尚未实现）
- 小红书地址栏手抠的无 `xsec_token` 链接（一律 404）
- 抖音 / 小红书评论区（简介与正文之外的链接目前采不到）

## 排障：抖音 / 小红书突然采不了

这两站绑的是未公开页面结构，改版后常见错误码是 `[structure_missing]`（任务失败文案前缀；设置 → 数据 → 打开日志里也有 marker / 页面哈希诊断）。

1. 看导入任务错误是否带 `[structure_missing]` / `[token_invalid]` / `[note_unavailable]`
2. 小红书先确认链接来自「分享 → 复制链接」（含 `xsec_token`）
3. 开发者可对样例公开 URL 跑线上探针（**不进默认测试**）：

```bash
# 仓库根目录
set GUIZHI_PROBE_DOUYIN_URL=https://www.iesdouyin.com/share/video/<id>/
set GUIZHI_PROBE_XHS_URL=https://www.xiaohongshu.com/explore/<id>?xsec_token=...
node apps/desktop/scripts/probe-platform-parsers.mjs
```

本地工具（yt-dlp / ffmpeg / FunASR）的安装说明见 [功能说明 · 本地工具链](./features.md#本地工具链--应用内一键安装)。
