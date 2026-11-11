// 更新器纯逻辑的差分基准：真实的 utils/version.ts、utils/changelog.ts、main/updater-channel.ts。
export async function buildUpdaterGolden(): Promise<unknown> {
  const ver = await import("../../apps/desktop/src/utils/version.ts");
  const cl = await import("../../apps/desktop/src/utils/changelog.ts");
  const ch = await import("../../apps/desktop/src/main/updater-channel.ts");

  const versions = [
    "0.1.0", "0.1.1", "0.2.0", "1.0.0", "1.0.0-beta.1", "1.0.0-beta.2", "1.0.0-beta.10", "1.0.0-alpha", "1.0.0-alpha.1",
    "1.0.0-rc.1", "1.0.0-1", "1.0.0-2", "1.0.0-beta", "v1.0.0", "V1.0.0", " v2.3.4 ", "2.3.4+build5", "v2.3.4+x", "10.0.0", "9.9.9",
    "0.28.0", "0.28.1-preview.3", "0.28.1", "", "x", "1", "1.2", "1.2.3.4", "1.10", "1.9", "abc", "abd", "v", "+", "0.0.0",
    "1.0.0-beta.1.2", "1.0.0-beta.x", "1.0.0-0", "01.2.3",
  ];
  const compare: unknown[] = [];
  for (const a of versions) for (const b of versions) compare.push({ a, b, r: Math.sign(ver.compareVersions(a, b)) });
  const normalize = versions.map((v) => ({ v, n: ver.normalizeVersion(v), pre: ver.isPrereleaseVersion(v), tag: ver.getReleaseTagForVersion(v) }));

  const rel = (tag: unknown, o: Record<string, unknown> = {}) => ({ tag_name: tag, ...o });
  const releaseLists: Record<string, unknown[]> = {
    empty: [],
    onlyDraft: [rel("v1.0.0", { draft: true })],
    single: [rel("v0.28.0")],
    higherPre: [rel("v0.28.0"), rel("v0.29.0-beta.1", { prerelease: true })],
    lowerPre: [rel("v0.28.0"), rel("v0.27.0-beta.1", { prerelease: true })],
    sameVersionMislabeled: [rel("v0.28.0", { prerelease: true }), rel("v0.28.0")],
    sameVersionOrder2: [rel("v0.28.0"), rel("v0.28.0", { prerelease: true })],
    bothPre: [rel("v1.0.0-beta.1", { prerelease: true }), rel("v1.0.0-beta.2", { prerelease: true })],
    badTags: [rel(""), rel("   "), rel(5), rel(undefined), rel("v0.1.0")],
    trimmed: [rel("  v2.0.0  ")],
    draftHigher: [rel("v3.0.0", { draft: true }), rel("v1.0.0")],
    mixed: [rel("v0.30.0", { prerelease: true, draft: true }), rel("v0.29.1"), rel("v0.29.2-preview.1", { prerelease: true }), rel("v0.29.0")],
    plusBuild: [rel("v1.0.0+a"), rel("v1.0.0+b", { prerelease: true })],
  };
  const pick = Object.entries(releaseLists).map(([name, list]) => {
    const choice = ch.pickPreviewFeedRelease(list as never);
    return { name, list, choice, tag: ch.releaseTagForPreviewFeed(choice) ?? null };
  });

  const changelogs: Record<string, string> = {
    empty: "",
    noHeadings: "just text\nmore",
    basic: "# CHANGELOG\n\n## v0.3.0\n\n- c\n\n---\n\n## v0.2.0\n\n- b\n\n## v0.1.0\n\n- a\n",
    brackets: "## [0.3.0] - 2026-01-01\nnotes3\n## [0.2.0]\nnotes2\n",
    noV: "## 0.3.0\nx\n## 0.2.0\ny\n",
    prerelease: "## v0.3.0-beta.1\nbeta notes\n## v0.2.0\nstable\n",
    crlf: "## v0.3.0\r\n- a\r\n---\r\n## v0.2.0\r\n- b\r\n",
    loneCr: "## v0.3.0\r- a\r## v0.2.0\r- b\r",
    dashLines: "## v0.3.0\n---\ntext\n---   \n\n---\n## v0.2.0\nz\n",
    indentedHeading: "  ## v0.3.0\nx\n## v0.2.0\ny\n",
    h3: "### v0.3.0\nx\n## v0.2.0\ny\n",
    unicode: "## v0.3.0\n中文说明 — 完成\n\u2028## v0.2.0\n老版本\n",
    trailingSpaces: "## v0.3.0   \n  body  \n\n\n## v0.2.0\nold\n\n\n",
    sameVersionTwice: "## v0.3.0\na\n## v0.3.0\nb\n",
    headingNoSpace: "##v0.3.0\nx\n## v0.2.0\ny\n",
    preludeBeforeFirst: "intro\n\n## v1.0.0\nbody\n",
    dashOnlyBody: "## v1.0.0\n---\n",
    heading4part: "## v1.2.3.4\nx\n## v1.2.3\ny\n",
  };
  const ranges: Array<[string, string]> = [["0.3.0", "0.1.0"], ["0.3.0", "0.2.0"], ["0.3.0", "0.3.0"], ["0.2.0", "0.3.0"], ["1.0.0", "0.0.0"], ["0.3.0-beta.1", "0.1.0"], ["0.3.0", "0.3.0-beta.1"], ["2.0.0", "0.2.5"]];
  const changelog: unknown[] = [];
  for (const [name, text] of Object.entries(changelogs)) {
    changelog.push({
      name, text,
      versions: cl.parseChangelogVersions(text),
      latest: cl.extractLatestChangelogSection(text),
      ranges: ranges.map(([nv, cv]) => ({ nv, cv, r: cl.extractChangelogRange(text, nv, cv) })),
    });
  }

  return { versions, compare, normalize, pick, changelog };
}
