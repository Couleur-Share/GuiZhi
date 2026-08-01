/**
 * 更新通道选源：对齐 electron-builder 的通道层级。
 *
 * - stable：只看正式版
 * - preview（≈ beta）：正式版 ∪ prerelease，取 semver 更高者
 *
 * 官方文档原文：Users which receive "beta" version will get "latest" versions too.
 * @see https://www.electron.build/tutorials/release-using-channels.html
 */

import { compareVersions, normalizeVersion } from "../utils/version";

export interface GithubReleaseCandidate {
  draft?: boolean;
  prerelease?: boolean;
  tag_name?: string;
}

export interface PreviewFeedChoice {
  /** GitHub tag，如 v0.17.0 */
  tagName: string;
  /** 是否来自 GitHub 的 prerelease 标记（与 semver 预发布后缀无关） */
  prerelease: boolean;
  version: string;
}

/**
 * 从 GitHub releases 列表里挑预览通道该跟的那一个：
 * 正式版与 prerelease 都参与比较，版本更高者胜出；同版本偏好正式版。
 */
export function pickPreviewFeedRelease(
  releases: GithubReleaseCandidate[],
): PreviewFeedChoice | null {
  let best: PreviewFeedChoice | null = null;

  for (const release of releases) {
    if (release.draft === true) continue;
    if (typeof release.tag_name !== "string" || !release.tag_name.trim()) {
      continue;
    }

    const tagName = release.tag_name.trim();
    const version = normalizeVersion(tagName);
    const candidate: PreviewFeedChoice = {
      tagName,
      prerelease: release.prerelease === true,
      version,
    };

    if (!best) {
      best = candidate;
      continue;
    }

    const cmp = compareVersions(candidate.version, best.version);
    if (cmp > 0) {
      best = candidate;
      continue;
    }
    // 同版本：正式版优先于 GitHub prerelease 标记（避免钉在误标的 prerelease 上）
    if (cmp === 0 && best.prerelease && !candidate.prerelease) {
      best = candidate;
    }
  }

  return best;
}

/**
 * 镜像 / generic feed 是否需要钉到具体 tag。
 * 赢家是正式版时走 latest/download；赢家是 prerelease 时才钉 tag。
 */
export function releaseTagForPreviewFeed(
  choice: PreviewFeedChoice | null,
): string | undefined {
  if (!choice || !choice.prerelease) return undefined;
  return choice.tagName;
}
