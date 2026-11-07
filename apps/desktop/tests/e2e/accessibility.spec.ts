import { test, expect, _electron as electron, type Page } from "@playwright/test";
import * as axe from "axe-core";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

type Locale = Record<string, unknown>;

const LOCALES: Locale[] = ["zh", "en"].map((name) =>
  JSON.parse(
    fs.readFileSync(path.join(__dirname, `../../src/renderer/i18n/locales/${name}.json`), "utf8"),
  ),
);

function message(locale: Locale, key: string): string {
  const value = key
    .split(".")
    .reduce<unknown>((node, part) => (node as Locale | undefined)?.[part], locale);
  if (typeof value !== "string") throw new Error(`语言包缺少文案：${key}`);
  return value;
}

/** 界面语言跟随系统，按设置按钮的可访问名认出当前语言包，按钮名称从同一语言包取。 */
async function currentLocale(page: Page): Promise<Locale> {
  const label = await page.getByTestId("rail-settings").getAttribute("aria-label");
  const locale = LOCALES.find((candidate) => message(candidate, "header.settings") === label);
  if (!locale) throw new Error(`无法按设置按钮的可访问名判断界面语言：${label}`);
  return locale;
}

async function expectNoSeriousViolations(page: Page, surface: string): Promise<void> {
  if (!(await page.evaluate(() => Boolean((window as typeof window & { axe?: unknown }).axe)))) {
    await page.evaluate(axe.source);
  }
  const result = await page.evaluate(async () => {
    const runtime = (window as typeof window & { axe: typeof axe }).axe;
    return runtime.run(document, {
      runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"] },
    });
  });
  const violations = result.violations.filter(
    (violation) => violation.impact === "critical" || violation.impact === "serious",
  );
  expect(
    violations.map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      nodes: violation.nodes.map((node) => ({
        target: node.target,
        html: node.html,
        failureSummary: node.failureSummary,
      })),
    })),
    `${surface} 存在 serious/critical axe 违规`,
  ).toEqual([]);
}

test("核心工作流通过 axe WCAG A/AA 检查", async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-a11y-"));
  const app = await electron.launch({
    args: [path.join(__dirname, "../../out/main/index.js")],
    env: {
      ...process.env,
      GUIZHI_E2E: "1",
      GUIZHI_E2E_USER_DATA_DIR: userDataDir,
    },
  });

  try {
    const page = await app.firstWindow();
    await page.waitForLoadState("domcontentloaded");
    await expect(page.getByTestId("topbar-search")).toBeVisible({ timeout: 20_000 });
    const locale = await currentLocale(page);
    const setupLater = page.getByRole("button", { name: message(locale, "setup.later"), exact: true });
    if (
      await setupLater
        .waitFor({ state: "visible", timeout: 3_000 })
        .then(() => true)
        .catch(() => false)
    ) {
      await setupLater.click();
      await expect(setupLater).toBeHidden();
    }

    await expectNoSeriousViolations(page, "知识库");

    const themes = [
      { id: "royal-blue", hue: 220, saturation: 70 },
      { id: "blue", hue: 210, saturation: 35 },
      { id: "purple", hue: 260, saturation: 30 },
      { id: "green", hue: 150, saturation: 30 },
      { id: "orange", hue: 25, saturation: 40 },
      { id: "teal", hue: 175, saturation: 30 },
    ];
    for (const dark of [false, true]) {
      for (const theme of themes) {
        await page.evaluate(({ hue, saturation, dark }) => {
          document.documentElement.style.setProperty("--theme-hue", String(hue));
          document.documentElement.style.setProperty("--theme-saturation", String(saturation));
          document.documentElement.classList.toggle("dark", dark);
        }, { ...theme, dark });
        // 主题切换会触发 150ms 的颜色过渡；axe 应检查稳定后的最终配色，
        // 否则会把前后两套颜色正在交叉插值的瞬间误判成对比度缺陷。
        await page.waitForTimeout(200);
        await expectNoSeriousViolations(page, `${theme.id}/${dark ? "dark" : "light"}`);
      }
    }
    await page.evaluate(() => {
      document.documentElement.classList.remove("dark");
      const shell = document.querySelector("#root > div");
      shell?.classList.add("app-background-mode-image");
      document.body.style.background = "linear-gradient(135deg, #17324d, #d8a47f)";
    });
    await page.waitForTimeout(200);
    await expectNoSeriousViolations(page, "背景图模式");
    await page.evaluate(() => {
      document.querySelector("#root > div")?.classList.remove("app-background-mode-image");
      document.body.style.background = "";
    });

    await app.evaluate(({ BrowserWindow }) => {
      const window = BrowserWindow.getAllWindows()[0];
      window?.setSize(800, 600);
      window?.webContents.setZoomFactor(2);
    });
    await page.waitForTimeout(100);
    await expectNoSeriousViolations(page, "最小窗口/200% 缩放");
    await app.evaluate(({ BrowserWindow }) => {
      const window = BrowserWindow.getAllWindows()[0];
      window?.webContents.setZoomFactor(1);
      window?.setSize(1200, 800);
    });

    await page.getByTestId("topbar-new").click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expectNoSeriousViolations(page, "快速采集");
    await page.keyboard.press("Escape");

    for (const key of ["nav.ask", "nav.wiki", "nav.imports", "nav.inbox"]) {
      const name = message(locale, key);
      await page.getByRole("button", { name, exact: true }).click();
      await page.waitForTimeout(100);
      await expectNoSeriousViolations(page, name);
    }

    await page.getByTestId("rail-settings").click();
    for (const section of ["ai", "capture", "data"] as const) {
      await page.getByTestId(`settings-nav-${section}`).click();
      await expect(page.getByTestId("settings-content-shell")).toBeVisible();
      await page.waitForTimeout(100);
      await expectNoSeriousViolations(page, `设置/${section}`);
    }
  } finally {
    await app.close();
    fs.rmSync(userDataDir, { recursive: true, force: true });
  }
});
