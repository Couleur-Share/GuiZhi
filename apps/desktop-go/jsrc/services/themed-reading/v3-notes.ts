import type { ReadingGeneration } from "@guizhi/shared/types/reading-page-v3";
import { readingPool } from "./v3-stream";

/** 超长来源逐层汇总；原分块笔记仍留在检查点，不因预算丢弃资料。 */
export async function reduceReadingNotes(
  notes: string[],
  generation: ReadingGeneration,
  call: (input: unknown) => Promise<Record<string, any>>,
  save: () => void,
): Promise<string> {
  let layer = notes,
    depth = 0;
  generation.reducedNotes ??= {};
  while (layer.join("\n").length > 80000) {
    const groups: string[][] = [];
    let group: string[] = [],
      length = 0;
    for (const note of layer) {
      if (length + note.length > 40000 && group.length) {
        groups.push(group);
        group = [];
        length = 0;
      }
      group.push(note);
      length += note.length;
    }
    if (group.length) groups.push(group);
    const next = new Array<string>(groups.length);
    await readingPool(groups, generation.limited ? 1 : 2, async (notes, i) => {
      const key = `${depth}-${i}`;
      let reduced = generation.reducedNotes[key];
      if (!reduced) {
        const response = await call({
          task: "合并这些来源笔记，返回{notes:string}，最多10000字符。保留关键数字、条件、对立观点和来源段落标记；不要新增事实。",
          notes,
        });
        if (
          typeof response.notes !== "string" ||
          !response.notes.trim() ||
          response.notes.length > 10000
        )
          throw new Error("汇总笔记无效，原分块已保留");
        reduced = generation.reducedNotes[key] = response.notes;
        save();
      }
      next[i] = reduced;
    });
    layer = next;
    depth++;
  }
  return layer.join("\n");
}
