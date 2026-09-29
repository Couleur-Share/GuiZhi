// 分享入口 spike（仅 dev 包，由 hvigorfile.ts 注册）：最小 ShareExtensionAbility。
// 收到分享后只做三件事：把记录整理成“形状”落盘、在当前进程实测 Asset Store / 凭证 / 网络、在面板上给出摘要。
// 不保存分享正文，也不投递任何内容到中转；结论见 docs/share-spike.md。
import { AbilityConstant, ShareExtensionAbility, UIExtensionContentSession, Want } from '@kit.AbilityKit';
import { systemShare } from '@kit.ShareKit';
import { process } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { ShareDiagStore } from './ShareDiagStore';
import { ShareProbeEntry, ShareRecordInput, newShareEntry, panelLines } from './ShareProbe';
import { runProbes } from './ShareRuntimeProbe';

const CALLER_KEY: string = 'ohos.aafwk.param.callerBundleName';

// 把系统记录映射为不含任何 SDK 类型的输入；缺失字段一律为空串。
async function readInputs(want: Want): Promise<ShareRecordInput[]> {
  const data = await systemShare.getSharedData(want);
  const shared = data.getRecords();
  const inputs: ShareRecordInput[] = [];
  for (let i = 0; i < shared.length; i++) {
    const record = shared[i];
    const input = new ShareRecordInput();
    input.utd = record.utd;
    input.content = typeof record.content === 'string' ? record.content : '';
    input.uri = typeof record.uri === 'string' ? record.uri : '';
    input.title = typeof record.title === 'string' ? record.title : '';
    input.description = typeof record.description === 'string' ? record.description : '';
    input.thumbnailBytes = record.thumbnail ? record.thumbnail.length : 0;
    input.thumbnailUri = typeof record.thumbnailUri === 'string' ? record.thumbnailUri : '';
    inputs.push(input);
  }
  return inputs;
}

export default class ShareDiagExtAbility extends ShareExtensionAbility {
  private reason: string = '';

  onCreate(launchParam: AbilityConstant.LaunchParam): void {
    this.reason = launchParam.launchReasonMessage ? launchParam.launchReasonMessage : '';
  }

  onSessionCreate(want: Want, session: UIExtensionContentSession): void {
    const storage = new LocalStorage();
    storage.setOrCreate('session', session);
    storage.setOrCreate('lines', ['正在读取分享数据…']);
    storage.setOrCreate('report', '');
    try {
      session.loadContent('sharediag/ShareDiagPanel', storage);
    } catch {
      // 面板加载不了就没有任何可见反馈，直接结束会话，避免留下一个空白面板。
      session.terminateSelf().catch(() => {});
      return;
    }
    this.collect(want, storage).catch((error: Error) => {
      storage.setOrCreate('lines', [`诊断过程出错：${error.message}`]);
    });
  }

  private async collect(want: Want, storage: LocalStorage): Promise<void> {
    const store = new ShareDiagStore(this.context);
    const parameters = want.parameters;
    // 只取参数名，值不记录（只有来源包名单独读取）。
    const keys: string[] = parameters ? Object.keys(parameters) : [];
    const callerValue = parameters ? parameters[CALLER_KEY] : undefined;
    let inputs: ShareRecordInput[] = [];
    let readError = '';
    try { inputs = await readInputs(want); }
    catch (error) { readError = `getSharedData 错误码 ${(error as BusinessError).code}`; }
    const entry = newShareEntry(Date.now(), process.pid, typeof callerValue === 'string' ? callerValue : '', this.reason, keys, inputs);
    if (readError) entry.pick = `error ${readError}`;
    // 形状先落盘：即使后面的网络探测卡住或面板被划走，分享记录的字段信息也不会丢。
    const persist = (): void => {
      try { store.upsert(entry); }
      catch (error) { entry.pick = `${entry.pick}；写入诊断文件失败 ${(error as BusinessError).code}`; }
      storage.setOrCreate('lines', panelLines(entry));
    };
    persist();
    await runProbes(entry, persist);
    storage.setOrCreate('report', store.report());
  }
}
