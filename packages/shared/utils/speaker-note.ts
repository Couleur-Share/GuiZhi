/**
 * 说话人分离的正文标记。
 *
 * 标记由 Python 服务端生成（`funasr-server-script.ts` 里的 `speaker_label`
 * 与 `build_diarized_text`），这里负责识别。两处的格式必须一致，改一处要同步
 * 另一处——它同时是 AI 排版的保全对象：排版会重写正文，前缀掉了就等于分离白做。
 */

/** 段首的「说话人 N：」，N 从 1 起 */
const SPEAKER_PREFIX = /^说话人 \d+：/gm;

/** 正文里说话人前缀的条数（排版前后要相等） */
export function countSpeakerPrefixes(text: string): number {
  return text.match(SPEAKER_PREFIX)?.length ?? 0;
}

/** 出现过的说话人标签，按首次出现排序、去重 */
export function listSpeakers(text: string): string[] {
  const seen = new Set<string>();
  for (const match of text.match(SPEAKER_PREFIX) ?? []) {
    seen.add(match.replace(/：$/, ""));
  }
  return [...seen];
}

/** 正文是否是分离后的对话体 */
export function hasSpeakerLabels(text: string): boolean {
  return countSpeakerPrefixes(text) > 0;
}
