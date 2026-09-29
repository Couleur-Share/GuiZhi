// 系统分享入口的记录选择：保留文本与链接，忽略图片/文件并计数，只有没有任何可用内容时才失败。
// 独立于 @kit.ShareKit：EntryAbility 负责把 SDK 记录映射为 ShareRecordView，
// 这样选择逻辑可在 Node 测试与 ArkTS 中运行同一份源码。

const TEXT_UTDS: string[] = ['general.hyperlink', 'general.plain-text', 'general.text'];

export class ShareRecordView {
  utd: string;
  content: string;
  constructor(utd: string, content: string) { this.utd = utd; this.content = content; }
}

export class ShareSelection {
  text: string = '';
  // 被忽略的不受支持记录（图片、文件等）数量。
  ignored: number = 0;
}

export function pickShareText(records: ShareRecordView[]): ShareSelection {
  if (records.length === 0) throw new Error('分享内容为空');
  const parts: string[] = [];
  let ignored = 0;
  for (let i = 0; i < records.length; i++) {
    const record = records[i];
    if (!TEXT_UTDS.includes(record.utd)) { ignored++; continue; }
    if (!record.content.trim()) continue; // 空白文字记录没有可保存的内容，直接略过。
    parts.push(record.content); // 原样保存，不抽取或归一化 URL 参数。
  }
  if (parts.length === 0) {
    throw new Error(ignored > 0 ? '暂不支持图片或文件分享，请分享链接或文字' : '分享缺少文字内容');
  }
  const selection = new ShareSelection();
  selection.text = parts.join('\n');
  selection.ignored = ignored;
  return selection;
}

export function shareSavedNotice(ignored: number): string {
  return ignored > 0
    ? `分享已保存到本机草稿，确认后发送。已忽略 ${ignored} 项图片或文件（暂不支持）`
    : '分享已保存到本机草稿，确认后发送';
}

// 分享失败时给出具体原因；原因缺失时退回通用说明。
export function shareFailureMessage(error: Error): string {
  const reason = error.message ? error.message : '';
  return reason ? `本次分享未保存：${reason}` : '本次分享未保存。请保留原内容，打开归知后重试，或复制文字到输入框。';
}
