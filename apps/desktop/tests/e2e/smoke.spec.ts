/**
 * 端到端冒烟：真实 Electron 启动 → 新建条目 → 全文搜索命中 → 手动备份成功。
 * 运行前需 `pnpm build`（test:e2e 脚本已包含）。
 */
import { test, expect, _electron as electron } from "@playwright/test";
import fs from "fs";
import os from "os";
import path from "path";

test("冒烟：新建条目 → 搜索命中 → 数据设置手动备份", async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-e2e-"));
  const app = await electron.launch({
    args: [path.join(__dirname, "../../out/main/index.js")],
    env: {
      ...process.env,
      GUIZHI_E2E: "1",
      GUIZHI_E2E_USER_DATA_DIR: userDataDir,
    },
  });

  try {
    const window = await app.firstWindow();
    await window.waitForLoadState("domcontentloaded");
    await expect(window.getByTestId("topbar-search")).toBeVisible({
      timeout: 20_000,
    });

    // 顶栏「新建」→ 快速采集里的空白笔记；命名后 Ctrl+S 立即落盘，写入 FTS 索引
    await window.getByTestId("topbar-new").click();
    await window.getByTestId("capture-blank-note").click();
    const titleInput = window.getByTestId("item-title-input");
    await expect(titleInput).toBeVisible();
    await titleInput.fill("冒烟测试条目");
    await window.keyboard.press("Control+s");

    // 顶栏全文搜索命中（中文按字分词）
    await window.getByTestId("topbar-search").fill("冒烟测试");
    await expect(
      window.getByTestId("item-list").getByText("冒烟测试条目"),
    ).toBeVisible({ timeout: 10_000 });

    // 设置 → 数据：手动备份产生备份文件并出现在列表
    await window.getByTestId("rail-settings").click();
    await window.getByTestId("settings-nav-data").click();
    await expect(window.getByTestId("data-settings")).toBeVisible();
    await window.getByTestId("backup-create").click();
    await expect(
      window.getByTestId("data-settings").getByText(/knowledge-manual-/),
    ).toBeVisible({ timeout: 10_000 });

    // 备份文件确实落在 E2E 用户目录的 backups 下
    const backupsDir = path.join(userDataDir, "backups");
    const backupFiles = fs
      .readdirSync(backupsDir)
      .filter((name) => name.startsWith("knowledge-manual-"));
    expect(backupFiles.length).toBeGreaterThan(0);
  } finally {
    await app.close();
    fs.rmSync(userDataDir, { recursive: true, force: true });
  }
});
