/**
 * 端到端：跨进程、跨重启的落盘保证。
 * 这两条链路在单测里都只能覆盖到一半——退出落盘要经过
 * 渲染进程 beforeunload → IPC → SQLite → 重启后重新读出；
 * 采集标签要经过弹窗 → import_tasks 迁移列 → 队列 → 条目。
 */
import { test, expect, _electron as electron } from "@playwright/test";
import type { ElectronApplication } from "@playwright/test";
import fs from "fs";
import os from "os";
import path from "path";

function launch(userDataDir: string): Promise<ElectronApplication> {
  return electron.launch({
    args: [path.join(__dirname, "../../out/main/index.js")],
    env: {
      ...process.env,
      GUIZHI_E2E: "1",
      GUIZHI_E2E_USER_DATA_DIR: userDataDir,
    },
  });
}

test("退出时未保存的编辑不会丢：关窗落盘，重启后还在", async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-e2e-"));
  let app = await launch(userDataDir);

  try {
    let window = await app.firstWindow();
    await window.waitForLoadState("domcontentloaded");
    await expect(window.getByTestId("topbar-search")).toBeVisible({
      timeout: 20_000,
    });

    // 先存一份基线，确保条目本身已经在库里
    await window.getByTestId("topbar-new").click();
    await window.getByTestId("capture-blank-note").click();
    const titleInput = window.getByTestId("item-title-input");
    await expect(titleInput).toBeVisible();
    await titleInput.fill("保存前的标题");
    await window.keyboard.press("Control+s");
    await expect(
      window.getByTestId("item-list").getByText("保存前的标题"),
    ).toBeVisible({ timeout: 10_000 });

    // 再改一次，这次不按 Ctrl+S，直接走关闭流程——
    // 只有 beforeunload 里的兜底落盘能把它救回来
    await titleInput.fill("退出前来不及保存的标题");
    // beforeunload 里的 returnValue 会让 CDP 报一个「对话框将要打开」，
    // 而 Electron 紧接着自己把它撤销掉。Playwright 默认会去自动关闭对话框，
    // 这时就会扑空报 "No dialog is showing"——挂一个空监听即可让它别插手。
    window.on("dialog", () => {});
    // 从主进程发起关闭：和点窗口叉号是同一条路径
    await app.evaluate(({ BrowserWindow }) => {
      BrowserWindow.getAllWindows()[0]?.close();
    });
    await window.getByTestId("close-dialog-exit").click();
    await app.close();

    // 同一个用户目录重开，标题应是退出前那次编辑
    app = await launch(userDataDir);
    window = await app.firstWindow();
    await window.waitForLoadState("domcontentloaded");
    await expect(
      window.getByTestId("item-list").getByText("退出前来不及保存的标题"),
    ).toBeVisible({ timeout: 20_000 });
    await expect(
      window.getByTestId("item-list").getByText("保存前的标题"),
    ).toHaveCount(0);
  } finally {
    await app.close();
    fs.rmSync(userDataDir, { recursive: true, force: true });
  }
});

test("键盘删除后可撤销：条目从回收站原样回到列表", async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-e2e-"));
  const app = await launch(userDataDir);

  try {
    const window = await app.firstWindow();
    await window.waitForLoadState("domcontentloaded");
    await expect(window.getByTestId("topbar-search")).toBeVisible({
      timeout: 20_000,
    });

    await window.getByTestId("topbar-new").click();
    await window.getByTestId("capture-blank-note").click();
    const titleInput = window.getByTestId("item-title-input");
    await expect(titleInput).toBeVisible();
    await titleInput.fill("误删的条目");
    await window.keyboard.press("Control+s");

    const list = window.getByTestId("item-list");
    await expect(list.getByText("误删的条目")).toBeVisible({ timeout: 10_000 });

    // 焦点在标题输入框里时 Delete 必须还是编辑文字，不能删条目
    await titleInput.focus();
    await window.keyboard.press("Delete");
    await expect(list.getByText("误删的条目")).toBeVisible();

    // 焦点回到列表才生效
    await list.getByText("误删的条目").click();
    await window.keyboard.press("Delete");
    await expect(list.getByText("误删的条目")).toHaveCount(0, {
      timeout: 10_000,
    });

    // 撤销把它从回收站捞回来
    await window.getByTestId("toast-action").click();
    await expect(list.getByText("误删的条目")).toBeVisible({ timeout: 10_000 });
  } finally {
    await app.close();
    fs.rmSync(userDataDir, { recursive: true, force: true });
  }
});

test("采集时打的标签跟着走完队列，落在入库条目上", async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-e2e-"));
  const app = await launch(userDataDir);

  try {
    const window = await app.firstWindow();
    await window.waitForLoadState("domcontentloaded");
    await expect(window.getByTestId("topbar-search")).toBeVisible({
      timeout: 20_000,
    });

    await window.getByTestId("topbar-new").click();
    await window.getByTestId("capture-draft").fill("端到端采集正文");
    // 标签输入框回车成 chip
    const tagInput = window.getByTestId("capture-tags-input");
    await tagInput.fill("端到端");
    await tagInput.press("Enter");
    await window.getByTestId("capture-submit").click();

    // 队列跑完后条目出现在列表里
    const list = window.getByTestId("item-list");
    await expect(list.getByText("端到端采集正文")).toBeVisible({
      timeout: 20_000,
    });

    // 打开条目，标签应已经在详情页上
    await list.getByText("端到端采集正文").click();
    await expect(window.getByText("端到端").first()).toBeVisible({
      timeout: 10_000,
    });
  } finally {
    await app.close();
    fs.rmSync(userDataDir, { recursive: true, force: true });
  }
});
