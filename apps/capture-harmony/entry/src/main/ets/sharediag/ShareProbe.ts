// 分享入口 spike（仅 dev 包）：把系统分享给扩展的数据整理成“形状”记录，并生成可整体复制的诊断报告。
// 隐私边界：只保留字段是否存在、长度与类别，不保存正文、标题、链接或图片；报告可以原样发给开发者。
// 独立于 @kit.ShareKit 与 fileIo：由扩展把 SDK 对象映射为下面的输入类，整份逻辑因此能在 Node 测试与 ArkTS 中运行同一份源码。
import { pickShareText, ShareRecordView } from '../core/ShareRecords';
import { parseCapture } from '../core/CaptureParser';

// 诊断文件最多保留的条目数；超出时丢弃最早的。
export const SHARE_PROBE_LIMIT: number = 60;
const WANT_KEY_LIMIT: number = 30;
const SAFE_TOKEN: RegExp = /^[A-Za-z0-9._:\-]{1,80}$/;
const SCHEME_RE: RegExp = /^([A-Za-z][A-Za-z0-9+.\-]{0,15}):/;

// 扩展从系统 SharedRecord 映射出的输入；缺失字段用空串或 0，避免 undefined 分支。
export class ShareRecordInput {
  utd: string = '';
  content: string = '';
  uri: string = '';
  title: string = '';
  description: string = '';
  thumbnailBytes: number = 0;
  thumbnailUri: string = '';
}

// 单条记录的形状：只有类型标识、长度与协议头。
export class RecordShape {
  utd: string = '';
  contentLength: number = 0;
  titleLength: number = 0;
  descriptionLength: number = 0;
  uriLength: number = 0;
  // uri 的协议头（file、https 等），不含路径；没有 uri 为 none，无法识别为 other。
  uriScheme: string = 'none';
  thumbnailBytes: number = 0;
  thumbnailUriLength: number = 0;
}

// 一个探测项：state 为 pending | ok | none | fail | skipped；detail 只放状态码、耗时等，不含地址与凭证。
export class ProbeStep {
  state: string = 'pending';
  detail: string = '';
  constructor(state: string = 'pending', detail: string = '') { this.state = state; this.detail = detail; }
}

export class ShareProbeEntry {
  id: string = '';
  // share：扩展收到一次分享；main：主进程打开诊断页时的基线，用来判断扩展是否运行在独立进程。
  kind: string = 'share';
  at: number = 0;
  pid: number = 0;
  caller: string = '';
  reason: string = '';
  wantKeys: string[] = [];
  records: RecordShape[] = [];
  // 应用现有的 pickShareText / parseCapture 对这些记录会做什么，用来判断现有逻辑能否直接复用。
  pick: string = '';
  parse: string = '';
  assetRw: ProbeStep = new ProbeStep();
  credential: ProbeStep = new ProbeStep();
  network: ProbeStep = new ProbeStep();
  elapsedMs: number = 0;
}

export function safeToken(value: string, fallback: string): string {
  return SAFE_TOKEN.test(value) ? value : fallback;
}

function uriSchemeOf(uri: string): string {
  if (uri.length === 0) return 'none';
  const match = uri.match(SCHEME_RE);
  return match === null ? 'other' : match[1].toLowerCase();
}

export function shapeOfRecord(input: ShareRecordInput): RecordShape {
  const shape = new RecordShape();
  shape.utd = safeToken(input.utd, 'invalid');
  shape.contentLength = input.content.length;
  shape.titleLength = input.title.length;
  shape.descriptionLength = input.description.length;
  shape.uriLength = input.uri.length;
  shape.uriScheme = uriSchemeOf(input.uri);
  shape.thumbnailBytes = input.thumbnailBytes;
  shape.thumbnailUriLength = input.thumbnailUri.length;
  return shape;
}

// 用应用现有的选择与识别逻辑处理同一批记录：只返回结果类别与数量，不含任何内容。
function summarizeSelection(inputs: ShareRecordInput[]): string[] {
  const views: ShareRecordView[] = [];
  for (let i = 0; i < inputs.length; i++) views.push(new ShareRecordView(inputs[i].utd, inputs[i].content));
  try {
    const selection = pickShareText(views);
    const parsed = parseCapture(selection.text, 'auto');
    const parse = `${parsed.kind} 链接${parsed.linkCount} 条目${parsed.itemCount}${parsed.code ? ` code=${parsed.code}` : ''}`;
    return [`ok 长度${selection.text.length} 忽略${selection.ignored}`, parse];
  } catch (error) {
    // pickShareText 的错误文案是固定的中文说明，不含分享内容。
    return [`error ${(error as Error).message}`, '-'];
  }
}

export function newShareEntry(at: number, pid: number, caller: string, reason: string, wantKeys: string[], inputs: ShareRecordInput[]): ShareProbeEntry {
  const entry = new ShareProbeEntry();
  entry.id = `${at}-${pid}`;
  entry.kind = 'share';
  entry.at = at;
  entry.pid = pid;
  entry.caller = safeToken(caller, 'unknown');
  entry.reason = safeToken(reason, '');
  const keys: string[] = [];
  for (let i = 0; i < wantKeys.length && keys.length < WANT_KEY_LIMIT; i++) {
    if (SAFE_TOKEN.test(wantKeys[i])) keys.push(wantKeys[i]);
  }
  entry.wantKeys = keys.sort();
  for (let i = 0; i < inputs.length; i++) entry.records.push(shapeOfRecord(inputs[i]));
  const summary = summarizeSelection(inputs);
  entry.pick = summary[0];
  entry.parse = summary[1];
  return entry;
}

// 一个进程一条基线（同一进程重复打开诊断页会覆盖同一条）。
export function newMainEntry(at: number, pid: number): ShareProbeEntry {
  const entry = new ShareProbeEntry();
  entry.id = `main-${pid}`;
  entry.kind = 'main';
  entry.at = at;
  entry.pid = pid;
  return entry;
}

export function encodeEntry(entry: ShareProbeEntry): string {
  return JSON.stringify(entry);
}

export class ParsedEntries {
  entries: ShareProbeEntry[] = [];
  // 无法解析的行数（文件被截断或手工改过），报告里会提示。
  skipped: number = 0;
}

export function parseEntries(text: string): ParsedEntries {
  const result = new ParsedEntries();
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (line.length === 0) continue;
    try {
      const entry = JSON.parse(line) as ShareProbeEntry;
      if (typeof entry.id !== 'string' || entry.id.length === 0 || (entry.kind !== 'share' && entry.kind !== 'main')) {
        result.skipped++;
        continue;
      }
      result.entries.push(entry);
    } catch {
      result.skipped++;
    }
  }
  return result;
}

// 同 id 的条目原地更新（探测项逐步完成时反复保存），否则追加；最后只保留最近 limit 条。
export function upsertEntries(text: string, entry: ShareProbeEntry, limit: number = SHARE_PROBE_LIMIT): string {
  const entries = parseEntries(text).entries;
  let replaced = false;
  for (let i = 0; i < entries.length; i++) {
    if (entries[i].id === entry.id) { entries[i] = entry; replaced = true; }
  }
  if (!replaced) entries.push(entry);
  const kept = entries.length > limit ? entries.slice(entries.length - limit) : entries;
  const lines: string[] = [];
  for (let i = 0; i < kept.length; i++) lines.push(encodeEntry(kept[i]));
  return lines.join('\n') + '\n';
}

function sharesOf(entries: ShareProbeEntry[]): ShareProbeEntry[] {
  return entries.filter((entry: ShareProbeEntry): boolean => entry.kind === 'share');
}

function stepText(step: ProbeStep): string {
  const label = step.state === 'ok' ? '正常' : step.state === 'none' ? '无数据' : step.state === 'fail' ? '失败'
    : step.state === 'skipped' ? '跳过' : '未完成';
  return step.detail ? `${label}（${step.detail}）` : label;
}

// 技术侧结论：ready 表示扩展内能读取配对凭证且能访问网络；blocked 表示其中一项已确认不可用；unknown 表示证据不足。
// 判据与计划一致，只看“读凭证”和“访问网络”：扩展只需要读取凭证再发请求，配对时的写入发生在主应用里，
// 所以 Asset Store 读写失败只作为参考写进原因，不改变结论。
// “是否出现在分享面板”只能人工确认，不在这里判断。
export class Verdict {
  state: string = 'unknown';
  reasons: string[] = [];
}

export function technicalVerdict(entries: ShareProbeEntry[]): Verdict {
  const verdict = new Verdict();
  const shares = sharesOf(entries);
  if (shares.length === 0) {
    verdict.reasons.push('尚无分享记录：请从浏览器等 App 分享一次到「归知收集 Dev」');
    return verdict;
  }
  const last = shares[shares.length - 1];
  const steps: ProbeStep[] = [last.credential, last.network];
  const names: string[] = ['扩展内读取配对凭证', '扩展内访问中转'];
  let blocked = false;
  let unknown = false;
  for (let i = 0; i < steps.length; i++) {
    const state = steps[i].state;
    if (state === 'fail') { blocked = true; verdict.reasons.push(`${names[i]}失败：${steps[i].detail || '无详情'}`); }
    else if (state === 'ok') continue;
    else if (state === 'none') { unknown = true; verdict.reasons.push('主应用尚未配对，无法验证凭证读取；请先在 Dev 包完成配对再分享'); }
    else { unknown = true; verdict.reasons.push(`${names[i]}尚未得出结果（${state}）`); }
  }
  // 参考项放在判据原因之后：让阅读者知道 Asset Store 整体是否异常，但不改变结论。
  if (last.assetRw.state === 'fail') verdict.reasons.push(`参考：扩展内 Asset Store 读写失败：${last.assetRw.detail || '无详情'}；判据只看凭证读取与网络`);
  verdict.state = blocked ? 'blocked' : unknown ? 'unknown' : 'ready';
  return verdict;
}

function verdictLabel(state: string): string {
  return state === 'ready' ? '技术侧可行（扩展内可读取配对凭证并访问中转）'
    : state === 'blocked' ? '技术侧受阻（见下方原因）' : '证据不足（见下方原因）';
}

// 扩展是否独立进程：把最近一次分享的 pid 与主进程基线比较。
export function processNote(entries: ShareProbeEntry[]): string {
  const shares = sharesOf(entries);
  if (shares.length === 0) return '尚无分享记录';
  const mains = entries.filter((entry: ShareProbeEntry): boolean => entry.kind === 'main');
  if (mains.length === 0) return '缺少主进程基线：请先打开一次「分享诊断」再分享';
  const last = shares[shares.length - 1];
  const pids: string[] = [];
  for (let i = 0; i < mains.length; i++) {
    if (mains[i].pid === last.pid) return `与主进程相同（pid ${last.pid}）`;
    pids.push(`${mains[i].pid}`);
  }
  return `独立进程（扩展 pid ${last.pid}，主进程基线 pid ${pids.join('、')}）`;
}

class SourceSummary {
  caller: string = '';
  shares: number = 0;
  records: number = 0;
  withContent: number = 0;
  withTitle: number = 0;
  withDescription: number = 0;
  withUri: number = 0;
  withThumbnail: number = 0;
  utds: string[] = [];
  picks: string[] = [];
  parses: string[] = [];
}

function addUnique(values: string[], value: string): void {
  if (!values.includes(value)) values.push(value);
}

function summarizeSources(entries: ShareProbeEntry[]): SourceSummary[] {
  const sources: SourceSummary[] = [];
  const shares = sharesOf(entries);
  for (let i = 0; i < shares.length; i++) {
    const entry = shares[i];
    let summary: SourceSummary | undefined = undefined;
    for (let j = 0; j < sources.length; j++) if (sources[j].caller === entry.caller) summary = sources[j];
    if (summary === undefined) {
      summary = new SourceSummary();
      summary.caller = entry.caller;
      sources.push(summary);
    }
    summary.shares++;
    for (let j = 0; j < entry.records.length; j++) {
      const record = entry.records[j];
      summary.records++;
      if (record.contentLength > 0) summary.withContent++;
      if (record.titleLength > 0) summary.withTitle++;
      if (record.descriptionLength > 0) summary.withDescription++;
      if (record.uriLength > 0) summary.withUri++;
      if (record.thumbnailBytes > 0 || record.thumbnailUriLength > 0) summary.withThumbnail++;
      addUnique(summary.utds, record.utd);
    }
    addUnique(summary.picks, entry.pick);
    addUnique(summary.parses, entry.parse);
  }
  return sources;
}

function formatSource(source: SourceSummary): string[] {
  const lines: string[] = [];
  lines.push(`${source.caller}：${source.shares} 次分享，${source.records} 条记录`);
  lines.push(`  字段出现：content ${source.withContent} · title ${source.withTitle} · description ${source.withDescription} · uri ${source.withUri} · 缩略图 ${source.withThumbnail}`);
  lines.push(`  类型：${source.utds.join('、') || '无'}`);
  lines.push(`  现有逻辑会：${source.picks.join('；')}${source.parses.length > 0 ? ` → ${source.parses.join('；')}` : ''}`);
  return lines;
}

export function formatReport(device: string, entries: ShareProbeEntry[], skipped: number, now: number): string {
  const shares = sharesOf(entries);
  const lines: string[] = [];
  lines.push('# 归知 分享诊断报告');
  lines.push(`生成时间：${new Date(now).toISOString()}`);
  lines.push(`设备：${device}`);
  lines.push(`记录：分享 ${shares.length} 条，主进程基线 ${entries.length - shares.length} 条${skipped > 0 ? `，${skipped} 行无法解析已跳过` : ''}`);
  lines.push('');
  lines.push('## 判据速览（技术侧；“是否出现在分享面板”需人工确认）');
  const verdict = technicalVerdict(entries);
  lines.push(`- 结论：${verdictLabel(verdict.state)}`);
  for (let i = 0; i < verdict.reasons.length; i++) lines.push(`  · ${verdict.reasons[i]}`);
  lines.push(`- 扩展进程：${processNote(entries)}`);
  if (shares.length > 0) {
    const last = shares[shares.length - 1];
    lines.push(`- 最近一次分享：Asset Store ${stepText(last.assetRw)}；凭证 ${stepText(last.credential)}；网络 ${stepText(last.network)}；用时 ${last.elapsedMs} ms`);
  }
  for (let i = entries.length - 1; i >= 0; i--) {
    if (entries[i].kind !== 'main') continue;
    lines.push(`- 主进程对照：Asset Store ${stepText(entries[i].assetRw)}；凭证 ${stepText(entries[i].credential)}；网络 ${stepText(entries[i].network)}`);
    break;
  }
  lines.push('');
  lines.push('## 来源明细');
  const sources = summarizeSources(entries);
  if (sources.length === 0) lines.push('（无）');
  for (let i = 0; i < sources.length; i++) {
    const block = formatSource(sources[i]);
    for (let j = 0; j < block.length; j++) lines.push(block[j]);
  }
  lines.push('');
  lines.push('## 原始记录（JSON Lines，仅含长度与类别）');
  for (let i = 0; i < entries.length; i++) lines.push(encodeEntry(entries[i]));
  return lines.join('\n');
}

// 扩展面板上的几行摘要（最近一次分享）。
export function panelLines(entry: ShareProbeEntry): string[] {
  const lines: string[] = [];
  lines.push(`来源：${entry.caller}　记录 ${entry.records.length} 条`);
  for (let i = 0; i < entry.records.length && i < 6; i++) {
    const record = entry.records[i];
    lines.push(`${i + 1}. ${record.utd} · content ${record.contentLength} · title ${record.titleLength} · description ${record.descriptionLength} · uri ${record.uriLength}(${record.uriScheme})`);
  }
  lines.push(`现有逻辑会：${entry.pick}`);
  lines.push(`Asset Store：${stepText(entry.assetRw)}`);
  lines.push(`配对凭证：${stepText(entry.credential)}`);
  lines.push(`访问中转：${stepText(entry.network)}`);
  return lines;
}
