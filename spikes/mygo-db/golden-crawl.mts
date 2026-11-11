// 文档批次相关差分基线：validateCrawlInput、CrawlJobDB 操作序列、robots.txt 规则与 sitemap 候选链接。
import fs from "node:fs";
import path from "node:path";
import { parseHTML } from "linkedom";
import { initDatabase, closeDatabase, CrawlJobDB } from "../../packages/db/src/index.ts";

const sleepMs = (ms: number) => {
  const end = Date.now() + ms;
  while (Date.now() < end);
};

export async function buildCrawlGolden(out: string): Promise<unknown> {
  const scope = await import("../../packages/shared/utils/web-scope.ts");
  const robots = await import("../../apps/desktop/src/main/services/web-capture/robots.ts");

  // ── validateCrawlInput ──
  const seed = (extra: Record<string, unknown> = {}) => ({ mode: "page", url: "https://a.test/x#h", ...extra });
  const base = (extra: Record<string, unknown> = {}) => ({ purpose: "documents", seeds: [seed()], ...extra });
  const validCases: [string, unknown][] = [
    ["minimal", base()],
    ["research", base({ purpose: "research", researchRunId: "r1" })],
    ["research-null-id", base({ researchRunId: null })],
    ["research-num-id", base({ researchRunId: 5 })],
    ["all-fields", base({ maxPages: 300, maxDepth: 5, collectionId: "c1", duplicatePolicy: "update" })],
    ["policy-other", base({ duplicatePolicy: "x" })],
    ["collection-empty", base({ collectionId: "" })],
    ["collection-num", base({ collectionId: 5 })],
    ["collection-zero", base({ collectionId: 0 })],
    ["maxpages-null", base({ maxPages: null, maxDepth: null })],
    ["directory", base({ seeds: [{ mode: "directory", url: "https://a.test/docs/guide/intro.html" }] })],
    ["directory-explicit", base({ seeds: [{ mode: "directory", url: "https://a.test/docs/guide/intro.html", directory: "/docs" }] })],
    ["directory-encoded", base({ seeds: [{ mode: "directory", url: "https://a.test/%E6%96%87%E6%A1%A3/a", directory: "/%E6%96%87%E6%A1%A3/" }] })],
    ["directory-null", base({ seeds: [{ mode: "directory", url: "https://a.test/d/a", directory: null }] })],
    ["ten-seeds", base({ seeds: Array.from({ length: 10 }, (_, i) => seed({ url: `https://a.test/${i}` })) })],
    ["page-with-directory", base({ seeds: [seed({ directory: "bad" })] })],
    ["bad-null", null], ["bad-string", "abc"], ["bad-number", 5], ["bad-array", []], ["bad-purpose", base({ purpose: "x" })], ["bad-purpose-missing", { seeds: [seed()] }],
    ["bad-seeds-missing", { purpose: "documents" }], ["bad-seeds-empty", base({ seeds: [] })], ["bad-seeds-11", base({ seeds: Array.from({ length: 11 }, () => seed()) })],
    ["bad-seeds-string", base({ seeds: "abc" })], ["bad-seeds-object", base({ seeds: { length: 1 } })],
    ["bad-maxpages-0", base({ maxPages: 0 })], ["bad-maxpages-301", base({ maxPages: 301 })], ["bad-maxpages-frac", base({ maxPages: 1.5 })], ["bad-maxpages-str", base({ maxPages: "5" })],
    ["bad-maxdepth-neg", base({ maxDepth: -1 })], ["bad-maxdepth-6", base({ maxDepth: 6 })], ["bad-maxdepth-bool", base({ maxDepth: true })],
    ["bad-seed-null", base({ seeds: [null] })], ["bad-seed-string", base({ seeds: ["https://a.test/"] })], ["bad-seed-number", base({ seeds: [5] })],
    ["bad-mode", base({ seeds: [{ mode: "x", url: "https://a.test/" }] })], ["bad-mode-missing", base({ seeds: [{ url: "https://a.test/" }] })],
    ["bad-url-ftp", base({ seeds: [seed({ url: "ftp://a.test/" })] })], ["bad-url-invalid", base({ seeds: [seed({ url: "not a url" })] })], ["bad-url-missing", base({ seeds: [{ mode: "page" }] })],
    ["bad-url-credentials", base({ seeds: [seed({ url: "https://u:p@a.test/" })] })], ["bad-url-num", base({ seeds: [seed({ url: 5 })] })], ["bad-url-null", base({ seeds: [seed({ url: null })] })],
    ["bad-dir-relative", base({ seeds: [{ mode: "directory", url: "https://a.test/a", directory: "docs" }] })], ["bad-dir-query", base({ seeds: [{ mode: "directory", url: "https://a.test/a", directory: "/x?y" }] })],
    ["bad-dir-number", base({ seeds: [{ mode: "directory", url: "https://a.test/a", directory: 5 }] })], ["bad-dir-outside", base({ seeds: [{ mode: "directory", url: "https://a.test/docs/a", directory: "/other/" }] })],
    ["bad-dir-dotdot", base({ seeds: [{ mode: "directory", url: "https://a.test/a", directory: "/x/../y/" }] })], ["bad-dir-encoded-sep", base({ seeds: [{ mode: "directory", url: "https://a.test/a", directory: "/x%2Fy/" }] })],
    ["bad-second-seed", base({ seeds: [seed(), { mode: "x" }] })],
  ];
  const validate = validCases.map(([name, input]) => {
    try {
      return { name, input, ok: true, v: scope.validateCrawlInput(input as never) };
    } catch (e) {
      return { name, input, ok: false, v: (e as Error).message };
    }
  });

  // ── CrawlJobDB 操作序列（id 用占位引用） ──
  const database = initDatabase(path.join(out, "crawl-scenario.db"));
  const jobs = new CrawlJobDB(database);
  const jobIds: string[] = [];
  type Op = { fn: string; args: unknown[] };
  const J = (n: number) => ({ $job: n });
  const P = (job: number, n: number) => ({ $page: [job, n] });
  const ops: Op[] = [
    { fn: "create", args: [base({ seeds: [seed(), { mode: "directory", url: "https://a.test/docs/guide/intro.html" }], maxPages: 5, maxDepth: 1, duplicatePolicy: "update" })] },
    { fn: "get", args: [J(0)] }, { fn: "list", args: [] }, { fn: "pages", args: [J(0)] },
    { fn: "discover", args: [J(0), "https://a.test/docs/guide/a", 1, 1] },
    { fn: "discover", args: [J(0), "https://a.test/docs/guide/a#frag", 1, 1] },
    { fn: "discover", args: [J(0), "https://a.test/docs/guide/deep", 2, 1] },
    { fn: "discover", args: [J(0), "ftp://a.test/x", 1, 1] },
    { fn: "discover", args: [J(0), "not a url", 1, 1] },
    { fn: "discover", args: [J(0), "https://a.test/docs/guide/b", 1, 1] },
    { fn: "discover", args: [J(0), "https://a.test/docs/guide/c", 1, 1] },
    { fn: "discover", args: [J(0), "https://a.test/docs/guide/d", 1, 1] },
    { fn: "pages", args: [J(0)] }, { fn: "get", args: [J(0)] },
    { fn: "setStatus", args: [J(0), "running"] }, { fn: "get", args: [J(0)] },
    { fn: "save", args: [{ $pageUpdate: [0, 0, { status: "running", stage: "fetching" }] }] },
    { fn: "save", args: [{ $pageUpdate: [0, 1, { status: "added", itemId: "item-1", importTaskId: "task-1", result: { title: "T", markdown: "m", complete: true } }] }] },
    { fn: "save", args: [{ $pageUpdate: [0, 2, { status: "failed", error: "网页采集失败" }] }] },
    { fn: "pages", args: [J(0)] }, { fn: "get", args: [J(0)] },
    { fn: "setStatus", args: [J(0), "paused", "出错了"] }, { fn: "get", args: [J(0)] },
    { fn: "setStatus", args: [J(0), "paused"] }, { fn: "get", args: [J(0)] },
    { fn: "setStatus", args: ["missing", "paused"] },
    { fn: "get", args: ["missing"] },
    { fn: "create", args: [base({ seeds: [seed({ url: "https://b.test/" })] })] },
    { fn: "setStatus", args: [J(1), "running"] },
    { fn: "save", args: [{ $pageUpdate: [1, 0, { status: "running" }] }] },
    { fn: "recover", args: [] },
    { fn: "get", args: [J(0)] }, { fn: "get", args: [J(1)] }, { fn: "pages", args: [J(1)] },
    { fn: "list", args: [] },
    { fn: "setStatus", args: [J(1), "running"] },
    { fn: "retry", args: [J(1)] },
    { fn: "setStatus", args: [J(1), "paused"] },
    { fn: "save", args: [{ $pageUpdate: [1, 0, { status: "failed", error: "x" }] }] },
    { fn: "retry", args: [J(1)] }, { fn: "pages", args: [J(1)] }, { fn: "get", args: [J(1)] },
    { fn: "retry", args: ["missing"] },
    { fn: "create", args: [base({ purpose: "bogus" })] },
  ];
  const resolve = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(resolve);
    if (v && typeof v === "object") {
      const o = v as Record<string, unknown>;
      if ("$job" in o) return jobIds[o.$job as number] ?? "missing-job";
      if ("$page" in o) {
        const [j, n] = o.$page as [number, number];
        return jobs.pages(jobIds[j])[n]?.id;
      }
      if ("$pageUpdate" in o) {
        const [j, n, patch] = o.$pageUpdate as [number, number, Record<string, unknown>];
        return { ...jobs.pages(jobIds[j])[n], ...patch };
      }
      return Object.fromEntries(Object.entries(o).map(([k, x]) => [k, resolve(x)]));
    }
    return v;
  };
  const results = ops.map((op) => {
    sleepMs(3);
    const args = resolve(op.args) as unknown[];
    try {
      const value = (jobs as unknown as Record<string, (...a: unknown[]) => unknown>)[op.fn](...args);
      if (op.fn === "create") jobIds.push((value as { id: string }).id);
      return { op, value: value === undefined ? null : value };
    } catch (e) {
      return { op, error: (e as Error).message };
    }
  });
  const dump = {
    jobs: (database.all("SELECT id, payload, status, created_at, updated_at FROM crawl_jobs ORDER BY created_at, rowid") as Record<string, unknown>[]).map((r) => Object.values(r)),
    pages: (database.all("SELECT id, job_id, url, depth, seed_index, status, payload FROM crawl_pages ORDER BY job_id, depth, rowid") as Record<string, unknown>[]).map((r) => Object.values(r)),
  };
  closeDatabase();

  // ── robots.txt ──
  const robotsTexts: string[] = [
    "",
    "User-agent: *\nDisallow: /private\nAllow: /private/open\nSitemap: https://a.test/sitemap.xml\n",
    "User-agent: GuiZhi\nDisallow: /\n\nUser-agent: *\nAllow: /\n",
    "user-agent: guizhi\r\nuser-agent: otherbot\r\ndisallow: /a*b$\r\nallow: /a*bc\r\n# comment\r\ndisallow: /tmp # trailing\r\n",
    "User-agent: *\nDisallow: /*.pdf$\nDisallow: /search?\nAllow: /search?q=ok\n",
    "User-agent: gui\nDisallow: /x\nUser-agent: *\nDisallow: /y\n",
    "User-agent: zhi\nDisallow: /z\n",
    "User-agent:\nDisallow: /empty-agent\n",
    "User-agent: *\nDisallow:\nAllow: /\n",
    "Disallow: /orphan\nUser-agent: *\nDisallow: /real\n",
    "User-agent: *\nDisallow: /a\nUser-agent: bot2\nDisallow: /b\nAllow: /c\n",
    "User-agent: *\nUser-agent: GuiZhi\nDisallow: /both\n",
    Array.from({ length: 14 }, (_, i) => `Sitemap: https://a.test/s${i}.xml`).join("\n"),
    "User-agent: *\nDisallow: /文档/\nDisallow: /a.b\nDisallow: /(x)\nDisallow: /$\n",
    "User-agent: *\nAllow: /same\nDisallow: /same\n",
    "User-agent: *\nDisallow: /same\nAllow: /same\n",
    "no colon line\nUser-agent *\nDisallow: /x\n",
    "User-agent: *\nSitemap:\nSitemap: https://a.test/x.xml\n",
  ];
  const robotUrls = [
    "https://a.test/", "https://a.test/private", "https://a.test/private/open", "https://a.test/private/open/x", "https://a.test/ab", "https://a.test/abc", "https://a.test/aXb", "https://a.test/aXbc",
    "https://a.test/tmp/x", "https://a.test/f.pdf", "https://a.test/f.pdf?x=1", "https://a.test/dir/f.pdf", "https://a.test/search?q=ok", "https://a.test/search?q=no", "https://a.test/search",
    "https://a.test/x", "https://a.test/y", "https://a.test/z", "https://a.test/empty-agent", "https://a.test/orphan", "https://a.test/real", "https://a.test/a", "https://a.test/b", "https://a.test/c",
    "https://a.test/both", "https://a.test/%E6%96%87%E6%A1%A3/a", "https://a.test/文档/a", "https://a.test/a.b", "https://a.test/aXb", "https://a.test/(x)", "https://a.test/same", "https://a.test/sam",
  ];
  const robotsCases = robotsTexts.map((text) => {
    const policy = robots.parseRobots(text);
    return { text, policy, allows: robotUrls.map((u) => robots.robotsAllows(u, policy)) };
  });

  // ── sitemap ──
  const sitemapTexts: string[] = [
    `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://a.test/docs/a</loc><lastmod>2024-01-01</lastmod></url><url><loc> https://a.test/docs/b </loc></url></urlset>`,
    `<urlset><url><loc>https://a.test/x?a=1&amp;b=2</loc></url><url><loc></loc></url><url><loc>
      https://a.test/multi
    </loc></url></urlset>`,
    `<sitemapindex><sitemap><loc>https://a.test/s1.xml</loc></sitemap></sitemapindex>`,
    `<urlset><url><loc><![CDATA[https://a.test/cdata]]></loc></url><url><loc>https://a.test/ok</loc></url></urlset>`,
    `<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"><url><loc>https://a.test/p</loc><image:image><image:loc>https://a.test/i.png</image:loc></image:image></url></urlset>`,
    `<urlset><URL><LOC>https://a.test/upper</LOC></URL></urlset>`,
    `<urlset><url><loc>https://a.test/a<b</loc></url><url><loc>https://a.test/&lt;enc&gt;</loc></url></urlset>`,
    `<urlset>` + Array.from({ length: 320 }, (_, i) => `<url><loc>https://a.test/p${i}</loc></url>`).join("") + `</urlset>`,
    `<html><body><url><loc>https://a.test/html</loc></url></body></html>`,
    `not xml at all`,
    `<urlset><url><loc>https://a.test/中文/页面</loc></url></urlset>`,
    `<urlset><url><loc>https://a.test/nested</loc><url><loc>https://a.test/inner</loc></url></url></urlset>`,
  ];
  const sitemaps = sitemapTexts.map((text) => {
    const { document } = parseHTML(text);
    return {
      text,
      locs: Array.from(document.querySelectorAll("url > loc"))
        .slice(0, 300)
        .map((n: { textContent: string | null }) => n.textContent?.trim() ?? null),
    };
  });

  fs.rmSync(path.join(out, "crawl-scenario.db"), { force: true });
  return { validate, ops: results, dump, robots: { urls: robotUrls, cases: robotsCases }, sitemaps };
}
