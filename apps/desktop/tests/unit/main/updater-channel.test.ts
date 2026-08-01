import { describe, expect, it } from "vitest";

import {
  pickPreviewFeedRelease,
  releaseTagForPreviewFeed,
} from "../../../src/main/updater-channel";

describe("pickPreviewFeedRelease", () => {
  it("正式版高于误标的 prerelease 时选正式版（0.17 > 0.16 prerelease）", () => {
    const choice = pickPreviewFeedRelease([
      { tag_name: "v0.17.0", prerelease: false, draft: false },
      { tag_name: "v0.16.0", prerelease: true, draft: false },
      { tag_name: "v0.15.0", prerelease: false, draft: false },
    ]);
    expect(choice).toEqual({
      tagName: "v0.17.0",
      prerelease: false,
      version: "0.17.0",
    });
    expect(releaseTagForPreviewFeed(choice)).toBeUndefined();
  });

  it("semver 预发布高于当前正式版时选预发布，并钉 tag 给镜像", () => {
    const choice = pickPreviewFeedRelease([
      { tag_name: "v0.18.0-beta.1", prerelease: true, draft: false },
      { tag_name: "v0.17.0", prerelease: false, draft: false },
    ]);
    expect(choice).toMatchObject({
      tagName: "v0.18.0-beta.1",
      prerelease: true,
      version: "0.18.0-beta.1",
    });
    expect(releaseTagForPreviewFeed(choice)).toBe("v0.18.0-beta.1");
  });

  it("同版本号时正式版优先于 GitHub prerelease 标记", () => {
    const choice = pickPreviewFeedRelease([
      { tag_name: "v0.17.0", prerelease: true, draft: false },
      { tag_name: "v0.17.0", prerelease: false, draft: false },
    ]);
    expect(choice?.prerelease).toBe(false);
    expect(releaseTagForPreviewFeed(choice)).toBeUndefined();
  });

  it("忽略 draft，且没有可用条目时返回 null", () => {
    expect(
      pickPreviewFeedRelease([
        { tag_name: "v0.19.0", prerelease: false, draft: true },
      ]),
    ).toBeNull();
    expect(pickPreviewFeedRelease([])).toBeNull();
    expect(releaseTagForPreviewFeed(null)).toBeUndefined();
  });
});
