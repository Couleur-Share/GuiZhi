// 把多行输入压缩成单行预览：取第一条非空行，合并连续空白，超出按字数截断并加省略号。
// 批量发送的失败提示与发送记录共用，避免各处对“预览”各有一套规则。
export function previewText(input: string, maxLength: number): string {
  const lines = input.split(/\r?\n/);
  let first = '';
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].replace(/\s+/g, ' ').trim();
    if (line.length > 0) { first = line; break; }
  }
  if (first.length <= maxLength) return first;
  let end = maxLength;
  // 不在代理对中间截断，避免留下半个表情符号。
  const code = first.charCodeAt(end - 1);
  if (code >= 0xD800 && code <= 0xDBFF) end -= 1;
  return `${first.slice(0, end)}…`;
}
