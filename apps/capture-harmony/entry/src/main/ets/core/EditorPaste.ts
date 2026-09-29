// 编辑面板“粘贴”按钮的合并规则：把剪贴板文字接到已有输入之后，绝不静默丢内容。
// 纯函数，便于在 Node 中验证；系统剪贴板读取由界面层完成。

// 输入框字符数上限（与 TextArea.maxLength 一致）；发送时另有 32 KiB 字节上限，由解析提示单独判断。
export const EDITOR_MAX_CHARS: number = 32768;

export class PasteOutcome {
  // 粘贴后的输入框内容；未粘贴时等于原内容。
  text: string = '';
  changed: boolean = false;
  // 未粘贴的原因；空字符串表示已粘贴。
  message: string = '';
}

function trimEndText(value: string): string {
  let end = value.length;
  while (end > 0 && value.charAt(end - 1).trim() === '') end -= 1;
  return value.slice(0, end);
}

export function pasteInto(current: string, clipboard: string): PasteOutcome {
  const outcome = new PasteOutcome();
  outcome.text = current;
  // 网页、聊天软件复制出来的内容常带尾随换行，去掉后再拼接。
  const body = trimEndText(clipboard);
  if (body.trim() === '') { outcome.message = '剪贴板里没有文字'; return outcome; }
  // 已有内容与新内容之间保证换行分隔；输入框只有空白时直接替换，避免开头留空行。
  const base = current.trim() === '' ? '' : current;
  const separator = base === '' || base.endsWith('\n') ? '' : '\n';
  const next = base + separator + body;
  if (next.length > EDITOR_MAX_CHARS) {
    outcome.message = `剪贴板内容（${body.length} 字）加上已输入的内容超过 ${EDITOR_MAX_CHARS} 字上限，未粘贴`;
    return outcome;
  }
  outcome.text = next; outcome.changed = true;
  return outcome;
}
