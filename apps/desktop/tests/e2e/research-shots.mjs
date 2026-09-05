// Run through scripts/screenshot.mjs: isolated profile, offscreen Electron, fixture IPC only.
export default async function researchShots({ win, app, shot }) {
  await win.evaluate(async () => {
    const stored = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}'); stored.state.language = "zh"; localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    await window.api.settings.set({ language: "zh" });
  });
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  const now = Date.now();
  const run = { id: "research-ui", topic: "本地 AI 知识库近期变化", dayRange: 30, rangeFrom: now - 30 * 86400000, rangeTo: now, depth: "deep", sources: ["bilibili", "douyin", "xiaohongshu"], status: "partial", reportStatus: "ready", reportMarkdown: "## 主要发现\n\n该材料介绍了本地保存与有限文字精读，未覆盖扫描图片。 [R1]\n\n[R1]: <https://www.bilibili.com/video/BV1research>", reportError: null, reportPromptVersion: "research-report-v2", savedItemId: null, candidateCount: 1, clusterCount: 0, createdAt: now, updatedAt: now, completedAt: now, context: { seriesId: "series-ui", phase: "idle", policyVersion: "research-policy-v2", reportOutdated: true, savedReportId: "snapshot-ui", knowledgeScope: { kind: "all" }, plan: { version: "research-plan-v1", intent: "recent", queries: ["本地 AI 知识库近期变化", "离线知识库 研究摘录"], entities: [] } } };
  const candidate = { id: "candidate-ui", runId: run.id, source: "bilibili", externalId: "BV1research", url: "https://www.bilibili.com/video/BV1research", normalizedUrl: "https://www.bilibili.com/video/BV1research", title: "本地知识库：研究摘录与证据引用", author: "示例作者", snippet: "在本地保存材料与出处，引用保留字幕时间点。", publishedAt: now - 86400000, dateConfidence: "high", mediaType: "video", engagement: { views: 1200, likes: 25 }, discoveryMethod: "public-api", relevanceScore: 90, recencyScore: 98, engagementScore: 60, overallScore: 85, clusterId: null, state: "available", importTaskId: null, importedItemId: null, createdAt: now, updatedAt: now, eligibility: "recent" };
  const passages = [{ kind: "caption", position: 0, startMs: 62000, endMs: 69000, text: "资料在本地保存。研究只精读文字与已有字幕，不处理扫描图片。" }];
  const sources = run.sources.map((source) => ({ runId: run.id, source, status: source === "xiaohongshu" ? "failed" : "succeeded", method: source === "bilibili" ? "public-api" : "authenticated-browser", collectedCount: source === "bilibili" ? 1 : 0, errorCode: source === "xiaohongshu" ? "verification_required" : null, error: source === "xiaohongshu" ? "需要完成平台验证" : null, startedAt: now - 20000, finishedAt: now }));
  const attempts = [{ id: "attempt-ui", runId: run.id, source: "bilibili", query: run.topic, cursor: null, nextCursor: "2", finished: false, method: "public-api", startedAt: now - 20000, finishedAt: now, returnedCount: 20, inWindowCount: 12, unknownDateCount: 2, capped: true }];
  const detail = { run, sources, attempts, candidates: [candidate], clusters: [], documents: [{ id: "document-ui", runId: run.id, candidateId: candidate.id, source: "bilibili", url: candidate.url, title: candidate.title, author: candidate.author, publishedAt: candidate.publishedAt, capturedAt: now, status: "partial", passages, contentHash: "fixture-hash", truncated: false, warning: "文字来自平台自动字幕，可能存在识别误差" }] };
  const packet = { runId: run.id, topic: run.topic, rangeFrom: run.rangeFrom, rangeTo: run.rangeTo, sourceRuns: sources, attempts, snapshotId: "snapshot-ui", operationId: "operation-ui", policyVersion: "research-policy-v2", items: [{ ref: "R1", candidateId: candidate.id, source: candidate.source, title: candidate.title, author: candidate.author, url: candidate.url, urls: [candidate.url], publishedAt: candidate.publishedAt, dateConfidence: "high", overallScore: 85, engagement: candidate.engagement, snippet: passages[0].text, passages, excerptTruncated: true }] };
  await app.evaluate(({ ipcMain }, fixture) => {
    for (const [channel, value] of Object.entries(fixture)) { ipcMain.removeHandler(channel); ipcMain.handle(channel, () => value); }
  }, { "research:list": [run], "research:get": detail, "research:evidence": packet, "research:baselines": [{ ...run, id: "baseline-ui", rangeTo: now - 86400000 }], "research:compare": { runId: run.id, baselineRunId: "baseline-ui", warnings: ["coverage_incomparable"], changes: [{ kind: "new", current: candidate }, { kind: "unknown", previous: { ...candidate, id: "previous-ui", title: "上轮出现的离线工具实践" } }] } });
  const later = win.getByRole("button", { name: /稍后再说|maybe later/i });
  if (await later.waitFor({ state: "visible", timeout: 3000 }).then(() => true).catch(() => false)) await later.click();
  await win.getByRole("button", { name: "研究", exact: true }).click();
  await win.getByText("关联已有知识", { exact: true }).click();
  await win.getByLabel("研究主题", { exact: true }).fill("本地 AI 知识库");
  if (await win.getByRole("button", { name: "开始研究", exact: true }).isEnabled()) throw new Error("Scope must be selected after opting in");
  await shot("research-create-scope");
  await win.getByRole("button", { name: /本地 AI 知识库近期变化/ }).click();
  await win.getByText(/查询计划与实际覆盖/).click();
  await shot("research-coverage");
  await win.getByRole("button", { name: "研究报告", exact: true }).click();
  await win.getByRole("button", { name: "R1", exact: true }).click();
  await win.getByTestId("research-evidence").waitFor();
  await win.locator("blockquote").filter({ hasText: passages[0].text }).first().waitFor();
  await shot("research-evidence-reference");
  await win.getByRole("button", { name: "本轮变化", exact: true }).click();
  await win.getByTestId("research-comparison").waitFor();
  await win.getByText("新增发现", { exact: true }).waitFor();
  await shot("research-comparison");
}
