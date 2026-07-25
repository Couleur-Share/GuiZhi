import { compareVersions } from "./version";

/**
 * 版本标题正则，兼容 `## v0.4.0`、`## 0.4.0` 与 `## [0.4.0]` 三种写法。
 * electron-builder.config.cjs 里有一份等价副本，改这里要同步改那边。
 */
const VERSION_HEADING_SOURCE =
  "^## \\[?v?(\\d+\\.\\d+\\.\\d+(?:-[0-9A-Za-z.-]+)?)\\]?";

interface ChangelogSection {
  version: string;
  startIndex: number;
}

export function parseChangelogVersions(content: string): ChangelogSection[] {
  const regex = new RegExp(VERSION_HEADING_SOURCE, "gm");
  const sections: ChangelogSection[] = [];

  let match: RegExpExecArray | null;
  while ((match = regex.exec(content)) !== null) {
    sections.push({ version: match[1], startIndex: match.index });
  }

  return sections;
}

function sliceSection(
  content: string,
  sections: ChangelogSection[],
  index: number,
): string {
  const start = sections[index].startIndex;
  const end = sections[index + 1]?.startIndex ?? content.length;
  return content
    .slice(start, end)
    .replace(/^---\s*$/gm, "")
    .trim();
}

/** 取最新一节版本记录，用于写入更新清单，避免整份 CHANGELOG 进入 latest.yml */
export function extractLatestChangelogSection(content: string): string {
  const sections = parseChangelogVersions(content);
  return sections.length > 0 ? sliceSection(content, sections, 0) : "";
}

/** 取 (currentVersion, newVersion] 区间内的版本记录，跨版本升级时会拼接多节 */
export function extractChangelogRange(
  content: string,
  newVersion: string,
  currentVersion: string,
): string {
  const sections = parseChangelogVersions(content);
  const relevant: string[] = [];

  for (let index = 0; index < sections.length; index += 1) {
    const { version } = sections[index];
    if (
      compareVersions(version, currentVersion) > 0 &&
      compareVersions(version, newVersion) <= 0
    ) {
      relevant.push(sliceSection(content, sections, index));
    }
  }

  return relevant.join("\n\n---\n\n");
}
