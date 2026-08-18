/**
 * README 知识库截图：灌入演示条目后分别截卡片视图与列表视图。
 *
 *   pnpm shot --steps scripts/readme-library-shots.mjs --out ../../docs/images
 */
import { createRequire } from "node:module";
import path from "node:path";
import { randomUUID } from "node:crypto";

const require = createRequire(import.meta.url);
const { Database } = require("node-sqlite3-wasm");

const DEMO_COLLECTIONS = [
  { name: "阅读笔记", icon: "📚" },
  { name: "工程实践", icon: "🛠️" },
];

const DEMO_TAGS = [
  { name: "AI", colorKey: "purple" },
  { name: "Electron", colorKey: "blue" },
  { name: "SQLite", colorKey: "teal" },
  { name: "local-first", colorKey: "green" },
  { name: "pnpm", colorKey: "orange" },
  { name: "会议", colorKey: "pink" },
  { name: "全文检索", colorKey: "indigo" },
  { name: "前端", colorKey: "blue" },
  { name: "安全", colorKey: "red" },
  { name: "工程效率", colorKey: "amber" },
  { name: "性能", colorKey: "green" },
  { name: "成本", colorKey: "orange" },
  { name: "架构", colorKey: "purple" },
  { name: "检索", colorKey: "teal" },
  { name: "笔记方法", colorKey: "pink" },
  { name: "算法", colorKey: "indigo" },
  { name: "系统思维", colorKey: "gray" },
  { name: "设计", colorKey: "pink" },
  { name: "阅读", colorKey: "amber" },
  { name: "踩坑", colorKey: "red" },
];

/** @type {Array<{
 *   title: string;
 *   itemType: string;
 *   collection: string;
 *   tags: string[];
 *   summary: string;
 *   content: string;
 *   favorite?: boolean;
 *   source?: { kind: "url" | "file"; uri: string; platform: string };
 *   select?: boolean;
 * }>} */
const DEMO_ITEMS = [
  {
    title: "Zettelkasten 与「原子笔记」的边界",
    itemType: "note",
    collection: "阅读笔记",
    tags: ["笔记方法", "阅读", "系统思维"],
    summary: "原子笔记不是越碎越好：一条笔记应能独立被检索与引用。",
    content: `一条笔记该多「原子」？拆到无法再引用，就只剩索引噪音。

## 可检验的标准

- 标题本身说得清它回答什么问题
- 正文离开上下文仍可读
- 能被至少两条其他笔记自然链到

超过这个粒度再拆，检索召回的是碎片，不是知识。`,
  },
  {
    title: "播客：本地大模型的推理成本曲线",
    itemType: "audio",
    collection: "工程实践",
    tags: ["AI", "成本", "性能"],
    summary: "同尺寸模型在 CPU / GPU 上的延迟与电费差距，远比参数量直观。",
    content: `> 平台：本地文件 · 时长：42:18

## 要点

- 延迟受制于内存带宽时，加核几乎无效
- 量化从 Q8 到 Q4 省显存，但长上下文质量掉得更快
- 「能跑」和「愿意每天跑」是两回事，电费会投票`,
    source: {
      kind: "file",
      uri: "C:/Users/demo/Podcasts/llm-cost.m4a",
      platform: "local",
    },
  },
  {
    title: "创新者的窘境：为什么好公司会错过下一波",
    itemType: "note",
    collection: "阅读笔记",
    tags: ["阅读", "系统思维", "架构"],
    summary: "听现有客户的话，往往会系统性地推迟破坏性创新。",
    content: `克里斯坦森的核心不是「大公司笨」，而是激励结构：现有客户给的利润，
会把资源锁在延续性改进上。

破坏性创新一开始在这些指标上更差，所以被理性地推迟——直到市场换了一套指标。`,
  },
  {
    title: "用 AGENTS.md 给项目装一份长期记忆",
    itemType: "webpage",
    collection: "工程实践",
    tags: ["AI", "工程效率", "架构"],
    summary: "把「为什么这样写」写进仓库，比写进聊天记录靠谱得多。",
    content: `AI 助手每次会话都从零开始。AGENTS.md 把约束、坑与决策留给下一次。

值得写进去的：实测过的结论、故意不做的事、改坏的代价。
不值得写的：翻文档就能查到的 API 清单。`,
    source: {
      kind: "url",
      uri: "https://example.com/agents-md",
      platform: "web",
    },
  },
  {
    title: "pnpm workspace 依赖提升的陷阱",
    itemType: "note",
    collection: "工程实践",
    tags: ["pnpm", "踩坑", "工程效率"],
    summary: "幽灵依赖在 pnpm 下变少了，但 peer 与 hoist 图案仍会悄悄换版本。",
    content: `## 现象

A 包能 import 到 B，本地 \`node_modules\` 里却找不到 B——它被提升到了别处。

## 处理

- 用 \`pnpm why\` 看真实解析路径
- 该声明的依赖写进 package.json，别靠提升
- CI 与本地用同一份 lockfile`,
  },
  {
    title: "Local-first 软件：数据所有权不是口号",
    itemType: "webpage",
    collection: "阅读笔记",
    tags: ["local-first", "架构", "安全"],
    summary: "先能单机工作，同步是增强——顺序反了就会变成又一个云笔记。",
    content: `Local-first 的检验：断网时核心路径是否仍可用。
同步冲突要可解释，不能静默选一边。`,
    source: {
      kind: "url",
      uri: "https://www.inkandswitch.com/local-first/",
      platform: "web",
    },
  },
  {
    title: "白板草图：v0.5 采集管线",
    itemType: "image",
    collection: "工程实践",
    tags: ["架构", "设计", "Electron"],
    summary: "队列 → 连接器 → 落库 → 转写/OCR，失败要能重试且不丢元数据。",
    content: `![白板](local-image://demo-whiteboard.webp)

采集管线的关键不是「快」，而是失败可恢复：条目先落库，贵的步骤后补。`,
    source: {
      kind: "file",
      uri: "C:/Users/demo/Pictures/pipeline-whiteboard.png",
      platform: "local",
    },
  },
  {
    title: "RRF 融合排序：关键词与向量各取所长",
    itemType: "note",
    collection: "工程实践",
    tags: ["检索", "算法", "AI"],
    summary: "不用调权重：倒数排名融合对分数刻度不敏感。",
    content: `倒数排名融合（RRF）把两路召回按名次合成一路。

\`score = Σ 1 / (k + rank)\`

k 常用 60。它对绝对分数不敏感，适合 FTS 与 cosine 混排。`,
  },
  {
    title: "视频总结：Core Web Vitals 里 LCP / INP / CLS",
    itemType: "video",
    collection: "工程实践",
    tags: ["前端", "性能", "阅读"],
    summary: "三个指标分别卡加载、交互、稳定——优化要分开做。",
    content: `> 平台：哔哩哔哩 · 作者：前端性能周刊 · 时长：12:40
> 相关链接：https://web.dev/articles/vitals

## 视频总结

LCP 看最大内容何时可见，INP 看交互延迟，CLS 看布局是否跳。
同一页面上三者常互相打架：为了 LCP 预加载的图可能推高 CLS。`,
    source: {
      kind: "url",
      uri: "https://www.bilibili.com/video/BV1demoWebVitals",
      platform: "bilibili",
    },
  },
  {
    title: "Electron IPC：主进程才是系统能力的边界",
    itemType: "webpage",
    collection: "工程实践",
    tags: ["Electron", "安全", "架构"],
    summary: "渲染进程不碰 Node；白名单 IPC 比 contextIsolation 口号更具体。",
    content: `preload 只暴露用得上的方法。频道名集中在一处常量，
两边对不齐时编译期就能发现，而不是运行时静默失败。`,
    source: {
      kind: "url",
      uri: "https://www.electronjs.org/docs/latest/tutorial/ipc",
      platform: "web",
    },
  },
  {
    title: "周会录音：Q3 知识库检索体验复盘",
    itemType: "audio",
    collection: "工程实践",
    tags: ["会议", "检索", "工程效率"],
    summary: "用户抱怨「搜得到标题、找不到正文」——摘要投影抢了 snippet 的位置。",
    content: `## 文字稿

说话人 1：这周反馈最多的是搜索摘要全是元数据。
说话人 2：剥掉引用块再压平，正文才能露出来。`,
    source: {
      kind: "file",
      uri: "C:/Users/demo/Recordings/weekly-q3.m4a",
      platform: "local",
    },
  },
  {
    title: "Thinking in Systems：反馈回路比目标陈述管用",
    itemType: "note",
    collection: "阅读笔记",
    tags: ["系统思维", "阅读", "设计"],
    summary: "杠杆点往往不在「更努力」，而在信息流与反馈延迟。",
    content: `系统行为由结构决定。想改结果，先画清存量、流量与反馈。
延迟过长的反馈，会把修正变成震荡。`,
  },
  {
    title: "SQLite FTS5 中文全文检索踩坑记录",
    itemType: "note",
    collection: "工程实践",
    tags: ["SQLite", "全文检索", "踩坑"],
    summary: "unicode61 按字切中文；BM25 要自己加权标题与标签。",
    content: `## 问题

FTS5 默认的 \`unicode61\` 分词器对中文是按字切的，这没问题；
真正的坑是英文前缀与中文混排时的查询编译。

## 方案

\`\`\`sql
CREATE VIRTUAL TABLE knowledge_fts USING fts5(
  title, content, tags,
  tokenize = "unicode61 remove_diacritics 2"
);
\`\`\`

## 排序

BM25 加权：标题 > 标签 > 正文。不要指望默认排名「懂」你的产品。`,
    favorite: true,
    select: true,
  },
  {
    title: "V2EX：有没有靠谱的本地知识库方案？",
    itemType: "forum",
    collection: "阅读笔记",
    tags: ["local-first", "AI", "阅读"],
    summary: "讨论集中在「要不要同步」与「AI 该不该上传全文」两极。",
    content: `> 平台：V2EX · 发布：2026-07-12

## 讨论总结

- 多数人要本地优先，同步当可选项
- 上传全文给云端 AI 的顾虑集中在日记与工作文档
- 有人提到用 MCP 让 IDE 只读本机库

## 正文

求推荐一个 Windows 上能用的本地知识库，最好能接 AI。

## 讨论（3 条）

**L1** 楼主 · 同步我可以自己用网盘扛
**L2** 匿名 · 别把 Key 和日记放同一份云备份里
**L3** 匿名 · 试试归知，MCP 能让 Cursor 直接搜`,
    source: {
      kind: "url",
      uri: "https://www.v2ex.com/t/114514",
      platform: "v2ex",
    },
  },
  {
    title: "抖音：Agent 工程里最容易被听错的词",
    itemType: "video",
    collection: "工程实践",
    tags: ["AI", "踩坑", "检索"],
    summary: "本地 ASR 毁专名；排版时塞进标题/简介里的拉丁词表能救回来。",
    content: `> 平台：抖音 · 作者：工程夜话 · 时长：8:12

## 视频总结

SenseVoice 中文稳，拉丁专名容易音近错误。把标题与简介里的词做成封闭表，
只改「表内已有的错误形式」，比让模型自由改写安全。`,
    source: {
      kind: "url",
      uri: "https://www.douyin.com/video/7123456789",
      platform: "douyin",
    },
  },
];

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForItemList(win) {
  await win.getByTestId("item-list").or(win.getByTestId("item-table")).waitFor({
    state: "visible",
    timeout: 15_000,
  });
}

/**
 * @param {{
 *   win: import('playwright').Page;
 *   app: import('playwright').ElectronApplication;
 *   shot: (name: string, options?: object) => Promise<string>;
 *   userDataDir: string;
 * }} ctx
 */
export default async function readmeLibraryShots({ win, app, shot, userDataDir }) {
  // 与现有 README 图一致：暗色 + 稍大窗口，侧栏与详情都露得出去
  await app.evaluate(({ BrowserWindow }) => {
    const window = BrowserWindow.getAllWindows()[0];
    if (!window) return;
    window.setSize(1440, 900);
  });

  await win.evaluate(() => {
    window.localStorage.setItem("guizhi-setup-dismissed", "1");
    const key = "guizhi-settings";
    const raw = window.localStorage.getItem(key);
    const parsed = raw ? JSON.parse(raw) : { state: {}, version: 0 };
    parsed.state = {
      ...(parsed.state ?? {}),
      themeMode: "dark",
      themeColor: "royal-blue",
      language: "zh-CN",
    };
    window.localStorage.setItem(key, JSON.stringify(parsed));

    const uiKey = "ui-storage";
    const uiRaw = window.localStorage.getItem(uiKey);
    const uiParsed = uiRaw ? JSON.parse(uiRaw) : { state: {} };
    uiParsed.state = {
      ...(uiParsed.state ?? {}),
      appModule: "library",
      libraryViewMode: "card",
      itemListPaneWidth: 320,
      sidebarPanelWidth: 240,
    };
    window.localStorage.setItem(uiKey, JSON.stringify(uiParsed));
  });
  await win.reload();
  await win
    .getByTestId("topbar-search")
    .waitFor({ state: "visible", timeout: 30_000 });

  const seeded = await win.evaluate(
    async ({ collections, tags, items }) => {
      const api = window.api;
      const collectionByName = {};
      for (const collection of collections) {
        const created = await api.collection.create(collection);
        collectionByName[collection.name] = created.id;
      }
      for (const tag of tags) {
        await api.tag.create(tag);
      }

      /** @type {Array<{ id: string; title: string; source?: object; favorite?: boolean; select?: boolean }>} */
      const createdItems = [];
      for (const item of items) {
        const created = await api.knowledge.create({
          title: item.title,
          content: item.content,
          itemType: item.itemType,
          collectionId: collectionByName[item.collection] ?? null,
          tagNames: item.tags,
        });
        await api.knowledge.update(created.id, {
          summary: item.summary,
          isFavorite: item.favorite === true,
        });
        createdItems.push({
          id: created.id,
          title: item.title,
          source: item.source,
          select: item.select === true,
        });
      }
      return createdItems;
    },
    { collections: DEMO_COLLECTIONS, tags: DEMO_TAGS, items: DEMO_ITEMS },
  );

  const dbPath = path.join(userDataDir, "data", "knowledge.db");
  const db = new Database(dbPath);
  try {
    const insert = db.prepare(
      `INSERT INTO source_records
         (id, item_id, source_type, source_uri, normalized_uri, content_hash, platform, captured_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    );
    const now = Date.now();
    for (const item of seeded) {
      if (!item.source) continue;
      insert.run([
        randomUUID(),
        item.id,
        item.source.kind,
        item.source.uri,
        item.source.uri,
        `hash-${item.id.slice(0, 8)}`,
        item.source.platform,
        now,
      ]);
    }
  } finally {
    db.close();
  }

  await win.reload();
  await win
    .getByTestId("topbar-search")
    .waitFor({ state: "visible", timeout: 30_000 });
  await waitForItemList(win);

  const selected = seeded.find((item) => item.select) ?? seeded[seeded.length - 1];
  await win.getByText(selected.title, { exact: true }).first().click();
  await win.getByTestId("item-title-input").waitFor({ state: "visible" });
  // 等列表虚拟化与详情正文落稳，避免截到半加载态
  await sleep(600);
  await shot("library-card");

  await win.getByRole("button", { name: "列表视图" }).click();
  await win.getByTestId("item-table").waitFor({ state: "visible" });
  await sleep(400);
  await shot("library-list");
}
