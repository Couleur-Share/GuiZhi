import { BrowserWindow, webContents } from "electron";
import {
  createReadingView,
  updateReadingView,
  destroyReadingView,
  probeReadingPage,
} from "../src/main/services/themed-reading/v3-views";
import { exportThemedReadingHtml } from "../src/main/services/themed-reading/export";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { getDatabase } from "../src/main/database";
import { KnowledgeItemDB } from "@guizhi/db";
import { ThemedReadingDB } from "@guizhi/db/themed-reading";
import { themedReadingFingerprint } from "../src/main/services/themed-reading/content";
import { createReadingGeneration } from "../src/main/services/themed-reading/v3-pipeline";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { ThemedReadingTask } from "@guizhi/shared/types/themed-reading";

export function readingV3FixturePage(
  code = "document.getElementById('add').addEventListener('click',()=>document.getElementById('value').textContent=String(Number(document.getElementById('value').textContent)+1));",
): ThemedReadingVersion {
  const text =
    "这是一份用于验证独立阅读环境的完整文章。页面中的交互只改变本页的演示数值，原始知识条目始终独立保存。关闭增强交互以后，正文、图注和说明仍然可读。点击下方按钮，可以观察计数器变化；缩小阅读区以后，正文应自动换行。";
  return {
    id: "v3-fixture",
    itemId: "v3-item",
    sourceKind: "body",
    role: "current",
    formatVersion: 3,
    createdAt: 1,
    updatedAt: 1,
    warnings: [],
    assets: [],
    options: {
      style: "",
      generateImages: false,
      maxImages: 0,
      research: false,
      action: "create",
      enhancedInteraction: true,
      researchDepth: "standard",
    },
    source: {
      title: "互动阅读验证",
      content: text,
      sourceUri: null,
      fingerprint: "a".repeat(64),
      blocks: [{ id: "b0", markdown: text, html: `<p>${text}</p>`, text }],
    },
    reconstruction: {
      notes: [],
      queries: [],
      references: [],
      draft: [{ title: "完整正文", markdown: text, referenceIds: [] }],
      interactions: [],
      outline: {
        title: "互动阅读验证",
        direction: "阅读优先",
        questions: [],
        sections: [{ title: "完整正文", brief: "验证" }],
      },
    },
    design: {
      html: `<main><h1>互动阅读验证</h1><h2 id="body">完整正文</h2><p>${text}</p><section id="counter"><button id="add">增加</button> <output id="value">0</output><p>静态说明：按钮每次将计数增加一。</p></section><details><summary>补充说明</summary><p>这段正文支持原生折叠，静态导出会展开。</p></details></main>`,
      css: "",
      direction: "阅读优先",
      assets: [],
      scripts: code ? [{ id: "counter-script", code, status: "ready" }] : [],
      libraries: [],
    },
  };
}
export async function readingV3Fixture(action: string, input: any = {}) {
  const owner = BrowserWindow.getAllWindows().find((w) =>
    w.webContents.getURL().startsWith("http"),
  )?.webContents;
  if (!owner) throw new Error("隔离截图窗口不存在");
  if (action === "seed") {
    const db = getDatabase(),
      page = readingV3FixturePage();
    if (input.preview) {
      page.id = "first-preview";
      page.source.title = "首次新稿预览";
    }
    const item = new KnowledgeItemDB(db).create({
      title: page.source.title,
      content: page.source.content,
      itemType: "note",
    });
    page.itemId = item.id;
    page.source.fingerprint = themedReadingFingerprint(
      page.source.title,
      page.source.content,
      null,
      "body",
    );
    page.role = "working";
    const pages = new ThemedReadingDB(db);
    if (input.preview) {
      page.generation = createReadingGeneration(page);
      page.generation.sections = [{ id: "intro", html: page.design.html }];
      page.generation.revision = 1;
      page.generation.scripts = page.design.scripts;
      page.design = null;
      pages.saveVersion(page);
      pages.saveTask({
        id: "first-preview-task",
        itemId: item.id,
        sourceKind: "body",
        versionId: page.id,
        title: page.source.title,
        state: "running",
        stage: "design",
        completed: 0,
        total: 2,
        previewRevision: 1,
        createdAt: Date.now(),
        updatedAt: Date.now(),
      });
      return item;
    }
    pages.saveVersion(page);
    pages.publish(page.id);
    return item;
  }
  if (action === "startPreview") {
    const pages = new ThemedReadingDB(getDatabase()),
      page = structuredClone(pages.get(input.itemId, "body"));
    page.id = "background-preview";
    page.role = "working";
    page.generation = createReadingGeneration(page);
    page.generation.sections = [
      { id: "intro", html: page.design.html + "<p>新稿预览第一部分</p>" },
    ];
    page.generation.scripts = page.design.scripts;
    page.generation.revision = 1;
    page.design = null;
    pages.saveVersion(page);
    const task: ThemedReadingTask = {
      id: "background-preview-task",
      itemId: page.itemId,
      sourceKind: "body",
      versionId: page.id,
      title: page.source.title,
      state: "running",
      stage: "design",
      completed: 0,
      total: 2,
      previewRevision: 1,
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };
    pages.saveTask(task);
    owner.send(IPC_CHANNELS.THEMED_READING_PROGRESS, task);
    return task;
  }
  if (action === "advancePreview") {
    const pages = new ThemedReadingDB(getDatabase()),
      task = pages.getTask(input.taskId),
      page = pages.getVersion(task.versionId);
    if (!input.publish) {
      page.generation.sections.push({
        id: "next",
        html: "<section><h2>新稿第二部分</h2><p>完整内容单元渐进更新。</p></section>",
      });
      page.generation.revision++;
      task.previewRevision = page.generation.revision;
    } else {
      page.design = {
        html: page.generation.sections.map((s) => s.html).join("\n"),
        css: "",
        direction: "",
        assets: [],
        scripts: page.generation.scripts,
        libraries: [],
      };
      page.generation.done = true;
    }
    pages.saveVersion(page, task);
    if (input.publish) {
      pages.publish(page.id);
      task.state = "completed";
      task.stage = "done";
      pages.saveTask(task);
    }
    owner.send(IPC_CHANNELS.THEMED_READING_PROGRESS, task);
    return task;
  }
  if (action === "create") {
    const page = readingV3FixturePage(input.code);
    if (input.libraries) {
      page.design.libraries = ["echarts", "mermaid", "animation"];
      page.design.html +=
        '<figure><div id="chart" style="width:300px;height:200px"></div><figcaption>静态说明：示例柱形数值为一和二。</figcaption></figure><figure><div id="diagram"></div><figcaption>流程：理解、应用。</figcaption></figure>';
      page.design.scripts.push({
        id: "libraries",
        status: "ready",
        code: "echarts.init(document.getElementById('chart'),null,{renderer:'svg'}).setOption({xAxis:{data:['甲','乙']},yAxis:{},series:[{type:'bar',data:[1,2]}]});mermaid.render('mermaid1','graph LR;A[理解]-->B[应用]').then(r=>document.getElementById('diagram').innerHTML=r.svg);document.body.dataset.animation=typeof anime.animate;",
      });
    }
    const id = await createReadingView(owner, page, !!input.preview);
    updateReadingView(owner, id, {
      x: 0,
      y: 0,
      width: input.width ?? 900,
      height: 650,
      visible: true,
    });
    return {
      id,
      ownerPid: owner.getOSProcessId(),
      guests: webContents
        .getAllWebContents()
        .filter((w) => w.getURL().startsWith("guizhi-reading:"))
        .map((w) => ({ id: w.id, pid: w.getOSProcessId() })),
    };
  }
  if (action === "destroy") {
    destroyReadingView(owner, input.id);
    return;
  }
  if (action === "probe")
    return await probeReadingPage(readingV3FixturePage(input.code));
  if (action === "export")
    return await exportThemedReadingHtml(
      readingV3FixturePage(),
      false,
      !!input.staticOnly,
    );
}
