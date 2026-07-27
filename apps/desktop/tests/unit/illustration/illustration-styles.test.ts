/**
 * 风格预设的写入路径。
 *
 * 读取可以宽容（用户手改 JSON 改坏了不该连累整个功能），写入必须严格：
 * `normalizeStyle` 会把缺字段的条目直接丢掉，如果保存也沿用这套宽容逻辑，
 * 用户点了保存、界面报成功，重开却发现那套风格没了。
 */
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import {
  BUILT_IN_ILLUSTRATION_STYLES,
  configureRuntimePaths,
  coreIllustrationStyleService,
  getIllustrationStylesFilePath,
  resetRuntimePaths,
} from "@guizhi/core";
import { validateStyleDrafts } from "../../../src/renderer/components/illustration/use-style-drafts";

let workDir: string;

const base = () => ({ ...BUILT_IN_ILLUSTRATION_STYLES[0] });

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-styles-test-"));
  configureRuntimePaths({ userDataPath: workDir });
});

afterEach(() => {
  resetRuntimePaths();
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("CoreIllustrationStyleService.write", () => {
  it("落盘后读回来是同一份", () => {
    const result = coreIllustrationStyleService.write([
      { ...base(), name: "我的风格" },
    ]);

    expect(result.success).toBe(true);
    expect(fs.existsSync(getIllustrationStylesFilePath())).toBe(true);
    const stored = coreIllustrationStyleService.read();
    expect(stored).toHaveLength(1);
    expect(stored[0].name).toBe("我的风格");
  });

  it("画法为空的那条指名道姓地拒绝，且整批都不落盘", () => {
    const result = coreIllustrationStyleService.write([
      base(),
      { ...base(), id: "blank", name: "空风格", visualDna: "   " },
    ]);

    expect(result.success).toBe(false);
    expect(result.error).toContain("空风格");
    expect(fs.existsSync(getIllustrationStylesFilePath())).toBe(false);
  });

  it("一条都不剩时不予保存", () => {
    expect(coreIllustrationStyleService.write([]).success).toBe(false);
    expect(coreIllustrationStyleService.write(null).success).toBe(false);
  });

  it("撞号的 id 顺延后缀，否则选择器会选中错的那条", () => {
    const result = coreIllustrationStyleService.write([
      base(),
      { ...base(), name: "副本" },
    ]);

    expect(result.success).toBe(true);
    expect(result.styles.map((style) => style.id)).toEqual([
      "hand-note",
      "hand-note-2",
    ]);
  });

  it("越界的张数与标注数截回合法区间", () => {
    const result = coreIllustrationStyleService.write([
      { ...base(), maxShots: 99, maxLabels: 0 },
    ]);

    expect(result.styles[0].maxShots).toBe(12);
    expect(result.styles[0].maxLabels).toBe(5);
  });
});

/**
 * 版本升级新增的内置风格要能到已有用户手上。
 *
 * 此前只有「首次播种 + 恢复内置预设（整份覆盖）」两条路：新预设要么到不了，
 * 要么得让人做一次会冲掉自己改动的破坏性操作。
 */
describe("内置预设增量补齐", () => {
  const writeRawFile = (payload: unknown) => {
    fs.mkdirSync(path.dirname(getIllustrationStylesFilePath()), {
      recursive: true,
    });
    fs.writeFileSync(
      getIllustrationStylesFilePath(),
      JSON.stringify(payload),
      "utf8",
    );
  };

  it("老文件（没有 dismissed 名单）缺的内置风格自动补进来并落盘", () => {
    writeRawFile({ styles: [base()] });

    const styles = coreIllustrationStyleService.read();

    expect(styles).toHaveLength(BUILT_IN_ILLUSTRATION_STYLES.length);
    // 落了盘：再读一次不依赖补齐逻辑也是全的
    const onDisk = JSON.parse(
      fs.readFileSync(getIllustrationStylesFilePath(), "utf8"),
    ) as { styles: unknown[] };
    expect(onDisk.styles).toHaveLength(BUILT_IN_ILLUSTRATION_STYLES.length);
  });

  it("用户删掉的内置风格不会被补回来", () => {
    // 保存时按「内置里当前没有的」反推 dismissed
    coreIllustrationStyleService.write([base()]);

    expect(coreIllustrationStyleService.read()).toHaveLength(1);
  });

  it("恢复内置预设后 dismissed 自然清空，不留永久拉黑记录", () => {
    coreIllustrationStyleService.write([base()]);
    coreIllustrationStyleService.write(BUILT_IN_ILLUSTRATION_STYLES);

    const onDisk = JSON.parse(
      fs.readFileSync(getIllustrationStylesFilePath(), "utf8"),
    ) as { dismissedBuiltIns: string[] };
    expect(onDisk.dismissedBuiltIns).toEqual([]);
  });

  it("自己新建的风格照旧留着，补齐只补内置的", () => {
    writeRawFile({
      styles: [{ ...base(), id: "mine", name: "我自己的" }],
      dismissedBuiltIns: [],
    });

    const styles = coreIllustrationStyleService.read();

    expect(styles[0].id).toBe("mine");
    expect(styles).toHaveLength(BUILT_IN_ILLUSTRATION_STYLES.length + 1);
  });
});

describe("内置预设本身", () => {
  // 撞号会被 write 静默改名成 xxx-2，界面上只看得出「怎么多了个奇怪的 id」
  it("id 不重复", () => {
    const ids = BUILT_IN_ILLUSTRATION_STYLES.map((style) => style.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("每套都填齐了必填项，且上限在合法区间内", () => {
    for (const style of BUILT_IN_ILLUSTRATION_STYLES) {
      expect(style.name.trim()).not.toBe("");
      expect(style.description.trim()).not.toBe("");
      expect(style.visualDna.trim()).not.toBe("");
      expect(style.negative.trim()).not.toBe("");
      expect(style.maxShots).toBeGreaterThanOrEqual(1);
      expect(style.maxShots).toBeLessThanOrEqual(12);
      expect(style.maxLabels).toBeGreaterThanOrEqual(1);
      expect(style.maxLabels).toBeLessThanOrEqual(10);
    }
  });

  it("整份内置预设保存得进去，一条不落", () => {
    const result = coreIllustrationStyleService.write(
      BUILT_IN_ILLUSTRATION_STYLES,
    );

    expect(result.success).toBe(true);
    expect(coreIllustrationStyleService.read()).toHaveLength(
      BUILT_IN_ILLUSTRATION_STYLES.length,
    );
  });
});

describe("validateStyleDrafts", () => {
  const messages = { name: "名称不能为空", visualDna: "画法不能为空" };

  it("逐条挑出必填项没填的风格", () => {
    const errors = validateStyleDrafts(
      [
        base(),
        { ...base(), id: "no-name", name: "  " },
        { ...base(), id: "no-dna", visualDna: "" },
      ],
      messages,
    );

    expect(Object.keys(errors)).toEqual(["no-name", "no-dna"]);
    expect(errors["no-name"].name).toBe(messages.name);
    expect(errors["no-dna"].visualDna).toBe(messages.visualDna);
  });

  it("填齐了就没有错误", () => {
    expect(validateStyleDrafts([base()], messages)).toEqual({});
  });
});
