# 采集平台

粘贴链接或分享口令即可采集。侧栏「平台」分区按来源分组；认不出专用连接器的 URL 走通用网页抓取。

| 类型 | 平台 | 能力概要 | 备注 |
| --- | --- | --- | --- |
| 视频 | <!-- source-platform:bilibili -->哔哩哔哩 | 元数据 + 音轨 → 转写 / 总结 / AI 标题 | 需 yt-dlp；认 `/video/` 与 `b23.tv` |
| 视频 | <!-- source-platform:youtube -->YouTube | 元数据 + 音轨 → 转写 / 总结 / AI 标题 | 需 yt-dlp；认 `/watch`、`/shorts/`、`youtu.be` |
| 视频 / 图文 | <!-- source-platform:douyin -->抖音 | 视频转写总结；图文配图入库 + 逐图 OCR；可选热门评论 | **不走 yt-dlp**；支持分享口令、公开 SSR 与内置登录采集；改版需跟修 |
| 图文 / 视频 | <!-- source-platform:xiaohongshu -->小红书 | 图文配图 + OCR；视频笔记；可选热门评论 | **不走 yt-dlp**；公开链路须用带 `xsec_token` 的分享链接，也可使用内置登录采集；改版需跟修 |
| 论坛 | <!-- source-platform:v2ex -->**V2EX** | 正文 + 全部回复 + AI 讨论总结 | 走官方只读 API，无需登录 |
| 论坛 | <!-- source-platform:nga -->**NGA** | 正文 + 楼主回复卡片 + AI 讨论总结（采样）+ 附件图 | guestJs 握手；不镜像水楼；总结采样约 12 页；附件图上限 80；需登录版块采不到 |
| 论坛 | <!-- source-platform:linuxdo -->**LINUX DO** | Discourse 正文 + 全部楼层 + AI 讨论总结 | 公开帖直取；遇登录或 Cloudflare 验证可切内置登录采集 |
| 论坛 | <!-- source-platform:appinn -->**小众软件** | Discourse 正文 + 全部楼层 + AI 讨论总结 | 采集 `meta.appinn.net` 公开主题，无需登录 |
| 论坛 | <!-- source-platform:twolibra -->**2Libra** | 正文 + 全部平铺评论 + 回复对象 / 楼层 + AI 讨论总结 | 走公开只读 API；分页失败时保留已取得内容并给出警告 |
| 图文 | <!-- source-platform:wechat -->微信公众号 | HTML 原文排版快照 + Markdown + 本地图片 | 精确识别 mp.weixin.qq.com；旧文章可手动补采；音视频保留原文入口 |
| 通用 | <!-- source-platform:web -->网页 | 内置 Crawl4AI + 独立 Chromium → Markdown | 无专用连接器的标准 URL；不支持系统或组件不可用时带原因回退 Readability |
| 通用 | <!-- source-platform:local -->本地文件 | 文本 / 图片 / 音视频拖入即入库 | 媒体资产化，详情页可预览播放 |

新增有限范围文档站导入、原文版本比较和指定网页研究，使用方式见 [内置网页采集](crawl4ai.md)。当前候选的全平台验收状态见 [实施记录](crawl4ai-implementation.md)，不以本机通过代替发布支持声明。

## 评论与讨论

- 论坛回复属于主题讨论，维持各平台已有的采集与刷新能力，不另设来源评论卡片。
- 抖音、小红书默认不采评论。详情页没有评论时隐藏卡片，通过「更多 → 采集评论」展开数量选择后手动采集；已有评论默认折叠显示条数，展开后可刷新。
- B 站、YouTube、通用网页和本地内容不提供独立评论采集入口。
- 研究精读的评论开关默认关闭，按研究记录保存。

## 分享口令

抖音 / 小红书等平台生成的「复制整段口令」可直接贴进快速采集框：归知会抠出链接并识别平台。提示栏会显示提取到的链接与将执行的动作，可一键改判（采集视频 vs 存成文本笔记）。

不支持省略协议的裸域名（如 `v.douyin.com/xxx`），以免把口令尾部噪音误判成链接。

## 未支持

- 其它论坛（Reddit 等）— 会退回通用网页抓取，效果通常较差
- NGA 需登录才能看的版块 / 帖子（公开帖经 guestJs 可采；NGA 尚未接入内置登录采集）
- 除抖音、小红书、LINUX DO 外，需要登录才能看的平台内容尚未接入内置登录采集
- 小红书地址栏手抠的无 `xsec_token` 链接（一律 404）
- 抖音 / 小红书只按任务配置采集热门评论（10 / 20 / 50 条），不是评论区完整镜像

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

## 研究精读与完整导入

上表描述完整导入。研究工作区只读取正文 / 文案、已有字幕；手动开启后读取有限评论，不走资产化、音频下载、转写或 OCR；B 站不读取评论。材料默认留在研究记录中，主动保存摘录或完整导入后才进入知识库。详见 [近期研究](./research.md)。

微信公众号的阅读、补采、离线导出及支持范围见 [微信公众号原文排版](wechat-snapshots.md)。
