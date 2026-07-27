/**
 * 说话人标记的识别。格式由 Python 服务端生成（funasr-server-script.ts），
 * 这里是消费端——两边对不上就等于分离白做。
 */
import { describe, expect, it } from "vitest";

import {
  countSpeakerPrefixes,
  hasSpeakerLabels,
  listSpeakers,
} from "../../../../../packages/shared/utils/speaker-note";

const DIALOG = [
  "说话人 1：这个季度要把准确率提上去。",
  "说话人 2：那得先解决方言。",
  "说话人 1：好，我这周整理评测集。",
].join("\n\n");

describe("说话人标记", () => {
  it("只认段首的标记，正文里提到「说话人」不算", () => {
    expect(countSpeakerPrefixes(DIALOG)).toBe(3);
    expect(
      countSpeakerPrefixes("这段讲的是说话人 1：怎么识别的原理。"),
    ).toBe(0);
  });

  it("说话人去重且按首次出现排序", () => {
    expect(listSpeakers(DIALOG)).toEqual(["说话人 1", "说话人 2"]);
  });

  it("单说话人与无标记区分得开", () => {
    expect(listSpeakers("说话人 1：全程只有我在说。")).toEqual(["说话人 1"]);
    expect(hasSpeakerLabels("普通的一段文字稿，没有分离。")).toBe(false);
  });
});
