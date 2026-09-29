import { CaptureMode, apiErrorMessage } from './Protocol';
import { CaptureParse, MAX_INPUT_BYTES, MAX_ITEMS } from './CaptureParser';

export type HintTone = 'info' | 'warning' | 'error';

// 编辑面板“识别提示行”的内容与可用操作，全部由解析结果推导，界面只负责渲染。
export class CaptureHint {
  tone: HintTone = 'info';
  text: string = '';
  // 保存草稿是否可用：内容为空、超过 32 KiB、超过 20 个链接时不可用。
  canSave: boolean = false;
  // 直接发送是否可用：在可保存的基础上，链接模式下没有识别到链接时发送必被拒绝，因此不可用。
  canSend: boolean = false;
  // 大于 0 时提供“按 20 条拆分保存”，值为拆分后的草稿份数。
  splitParts: number = 0;
  // 自动识别到“文字里夹着链接”：提供“按链接采集 / 存为文字”两个显式选项。
  offerModes: boolean = false;
}

// 接近 32 KiB 上限时才显示已用量，避免平时打扰。
const SIZE_WARN_BYTES: number = 24576;

function sizeSuffix(bytes: number): string {
  if (bytes <= SIZE_WARN_BYTES || bytes > MAX_INPUT_BYTES) return '';
  return ` · 已用 ${(bytes / 1024).toFixed(1)} / 32 KiB`;
}

export function captureHint(parse: CaptureParse, mode: CaptureMode): CaptureHint {
  const hint = new CaptureHint();
  if (parse.code === 'empty_input') return hint;
  if (parse.code === 'input_too_large') {
    hint.tone = 'error'; hint.text = apiErrorMessage('input_too_large');
    return hint;
  }
  if (parse.code === 'no_links') {
    // 仍允许存为草稿，稍后改成文字模式；但直接发送一定会被中转拒绝。
    hint.tone = 'error'; hint.text = apiErrorMessage('no_links');
    hint.canSave = true;
    return hint;
  }
  if (parse.code === 'too_many_links') {
    hint.tone = 'warning';
    hint.text = `共 ${parse.linkCount} 个链接，一次最多 ${MAX_ITEMS} 个`;
    hint.splitParts = Math.ceil(parse.linkCount / MAX_ITEMS);
    return hint;
  }
  hint.canSave = true; hint.canSend = true;
  const suffix = sizeSuffix(parse.bytes);
  if (parse.kind === 'mixed') {
    hint.offerModes = true;
    const limit = parse.linkCount > MAX_ITEMS ? `（按链接采集一次最多 ${MAX_ITEMS} 个）` : '';
    hint.text = `含 ${parse.linkCount} 个链接，电脑将自动判断${limit}${suffix}`;
  } else if (parse.kind === 'urls') {
    hint.text = `识别到 ${parse.linkCount} 个链接${suffix}`;
  } else {
    hint.text = (mode === 'text' ? '将作为文字整段保存' : '没有识别到链接，将作为文字整段保存') + suffix;
  }
  return hint;
}
