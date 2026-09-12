import { createHash } from "node:crypto";
import type { ThemedReadingAsset, ThemedReadingTask, ThemedReadingVersion } from "@guizhi/shared/types";

export function themedAsset(fileName = "theme-test.png", data = Buffer.from("theme image")): ThemedReadingAsset {
  return {id: fileName.replace(/\W/g, "-"), role: "generated", purpose: "主题头图", prompt: "麦穗插画", alt: "麦穗", aspectRatio: "16:9",
    fileName, sha256: createHash("sha256").update(data).digest("hex"), bytes: data.length, status: "ready"};
}

export function themedVersion(itemId: string, overrides: Partial<ThemedReadingVersion> = {}): ThemedReadingVersion {
  return {
    id: "theme-version-1", itemId, sourceKind: "body", role: "working", formatVersion: 1,
    source: {title: "啤酒知识", content: "保持原文全部内容", sourceUri: null, fingerprint: "a".repeat(64),
      blocks: [{id: "b0", markdown: "保持原文全部内容", html: "<p>保持原文全部内容</p>", text: "保持原文全部内容"}]},
    options: {style: "琥珀色", generateImages: true, maxImages: 3},
    design: {direction: "啤酒手册", html: '<main><div data-source-block="b0"></div></main>', css: "", assets: []},
    assets: [], warnings: [], createdAt: 1, updatedAt: 1, ...overrides,
  };
}

export function themedTask(version: ThemedReadingVersion, overrides: Partial<ThemedReadingTask> = {}): ThemedReadingTask {
  return {id: `task-${version.id}`, itemId: version.itemId, sourceKind: version.sourceKind, versionId: version.id,
    title: version.source.title, state: "running", stage: "images", completed: 0, total: 3, createdAt: 1, updatedAt: 1, ...overrides};
}
