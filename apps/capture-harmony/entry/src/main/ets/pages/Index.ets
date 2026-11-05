import { PageHeader } from '../components/PageHeader';
import { ReadingTab } from '../components/ReadingTab';
import { readingTrialEnabled } from '../services/ReadingRuntime';
import { ThemePalette, themePalette } from '../services/ThemePalette';
import { common } from '@kit.AbilityKit';
import { util } from '@kit.ArkTS';
import { pasteboard } from '@kit.BasicServicesKit';
import { scanBarcode, scanCore } from '@kit.ScanKit';
import { KeyboardAvoidMode, SymbolGlyphModifier } from '@kit.ArkUI';
import { SecondaryAction } from '../components/Actions';
import { CaptureEditor } from '../components/CaptureEditor';
import { CaptureTab } from '../components/CaptureTab';
import { ConnectionTab } from '../components/ConnectionTab';
import { HistoryTab } from '../components/HistoryTab';
import { SelectionBar } from '../components/SelectionBar';
import { SettingsPage } from '../components/SettingsPage';
import { UndoBar } from '../components/UndoBar';
import { AppPreferences, AppPreferencesStore, applyColorMode } from '../services/AppPreferences';
import { CaptureTabsAppearance, panelMaterial } from '../services/ImmersiveAppearance';
import { hapticSuccess } from '../services/Haptics';
import { animateInterface, revealTransition, motionDuration } from '../services/InterfaceMotion';
import { CONTENT_END_OFFSET, CONTENT_END_OFFSET_SELECTING, CONTENT_END_OFFSET_UNDO, CONTENT_MAX_WIDTH } from '../services/Layout';
import { HoldingHandObserver, HoldingHandState } from '../services/HoldingHandObserver';
import { CaptureRuntime } from '../services/CaptureRuntime';
import { ConnectionCheck, RelayRequestError } from '../core/ConnectionCheck';
import { EditorAutosave } from '../core/EditorAutosave';
import { CaptureParse, MAX_ITEMS, parseCapture, splitUrlChunks } from '../core/CaptureParser';
import { BatchHooks, BatchResult, sendBatch, batchHeadline, batchReport } from '../core/BatchSend';
import { DesktopPoll, DesktopStatus, desktopStatusOf, waitingCount } from '../core/DesktopStatus';
import { HistoryItem, firstPage, historyCursor, mergeHistory, nextPage } from '../core/HistoryView';
import { DeleteFailure, DeleteHooks, PendingDeletes } from '../core/PendingDelete';
import { SentEntry, buildSentEntry } from '../core/SentLog';
import { allSelected, keepExisting, toggleId } from '../core/Selection';
import { SendingEntry, sendingBegin, sendingEnd, sendingFind, sendingTick } from '../core/SendingState';
import { previewText } from '../core/TextPreview';
import { Draft, CaptureMode, Receipt, PairingLink, Session, parsePairingLink } from '../core/Protocol';

@Entry
@Component
struct Index {
  @Provide('capturePalette') palette: ThemePalette = themePalette('blue');
  @State ready: boolean = false;
  @State readingEnabled: boolean = false;
  @State busy: boolean = false;
  @State operation: string = '';
  @State operationSeconds: number = 0;
  @State automaticCheck: boolean = true;
  @State refreshing: boolean = false;
  @State paired: boolean = false;
  @State connected: boolean = false;
  @State origin: string = '';
  @State input: string = '';
  @State mode: CaptureMode = 'auto';
  @State drafts: Draft[] = [];
  @State history: Receipt[] = [];
  @State historyPulling: boolean = false;
  @State historyLoaded: boolean = false;
  // 服务端还有更早的记录可加载；historyLoadingMore 为“加载更多”请求进行中。
  @State historyMore: boolean = false;
  @State historyLoadingMore: boolean = false;
  // 本机发送记录：与回执按请求编号关联，给近期页补上内容预览与来源。
  @State sentEntries: SentEntry[] = [];
  // 发送记录读写出问题时的说明，近期页顶部持续显示，直到下一次成功写入或清除。
  @State sentLogWarning: string = '';
  // 电脑在线状态：会话里的 desktopSeenAgoMs 加上近期记录里等待取件的条数；未知（旧中转、未配对、查询失败）时不显示。
  @State desktop: DesktopStatus = new DesktopStatus();
  @State statusError: string = '';
  @State error: string = '';
  @State notice: string = '';
  @State pairingText: string = '';
  @State pairingOrigin: string = '';
  @State deviceName: string = '我的鸿蒙手机';
  @State selectedTab: number = 0;
  @State showEditor: boolean = false;
  @State showPairingLink: boolean = false;
  @State editorSaving: boolean = false;
  @State editorError: string = '';
  @State settings: AppPreferences = new AppPreferences();
  @State settingsReady: boolean = false;
  @State holdingSide: string = 'right';
  @State holdingMessage: string = '等待握持识别';
  @State settingsDetail: boolean = false;
  // 正在网络发送的草稿：只锁定各自的卡片，页面其他操作（新建记录、编辑其他草稿）不受影响。
  @State sendings: SendingEntry[] = [];
  // “全部发送”的进度；batchTotal > 0 表示批量发送进行中。
  @State batchDone: number = 0;
  @State batchTotal: number = 0;
  // 多选：长按草稿进入；selectedIds 只保存仍在列表里的草稿编号，草稿被发送或删除后自动收缩。
  @State selecting: boolean = false;
  @State selectedIds: string[] = [];
  // 延迟删除中的草稿编号（撤销窗口内，或正在真正删除）：仍保存在本机，只是不在列表里显示。
  @State hiddenIds: string[] = [];
  // 撤销条文案；空字符串表示当前没有可撤销的删除。
  @State undoMessage: string = '';
  private sendTimer: number = -1;
  // 批量发送失败时，界面只内联展示前几条原因，完整原因通过“复制原因”提供；二者以文案相等关联，文案变化后自动失效。
  private errorDetail: string = '';
  private errorDetailFor: string = '';
  private handledShareId: string = '';
  private tabsController: TabsController = new TabsController();
  private holdingObserver: HoldingHandObserver = new HoldingHandObserver((state: HoldingHandState) => {
    this.animate(() => { this.holdingSide = state.side; this.holdingMessage = state.message; });
  });
  private settingsStore: AppPreferencesStore | undefined;
  // 输入框内容的防抖落盘；runtime 在初始化完成后才存在，写入函数按需读取。
  private autosave: EditorAutosave = new EditorAutosave(
    (text: string, mode: CaptureMode): Promise<void> => this.runtime ? this.runtime.queue.edit(text, mode) : Promise.resolve(),
    (action: () => void, delay: number): number => setTimeout(action, delay),
    (id: number): void => { clearTimeout(id); },
    (saving: boolean, error: string): void => { this.editorSaving = saving; this.editorError = error; }
  );
  // 延迟删除：先隐藏并留 8 秒撤销，到点才真正删除；进入后台前立即删除，不把决定留到进程被回收之后。
  private pendingDeletes: PendingDeletes = new PendingDeletes(
    (ids: string[]): Promise<DeleteFailure[]> => this.commitDeletes(ids),
    this.deleteHooks(),
    (action: () => void, delay: number): number => setTimeout(action, delay),
    (id: number): void => { clearTimeout(id); }
  );
  @StorageLink('captureSafeTop') safeTop: number = 0;
  @StorageLink('captureSafeBottom') safeBottom: number = 0;
  @StorageLink('captureSafeLeft') safeLeft: number = 0;
  @StorageLink('captureSafeRight') safeRight: number = 0;
  @StorageLink('captureKeyboardVisible') keyboardVisible: boolean = false;
  @StorageLink('captureForeground') @Watch('foregroundChanged') foreground: boolean = false;
  @StorageLink('captureRevision') @Watch('reloadDrafts') revision: number = 0;
  // 最近一次分享落盘生成的草稿编号；开启“分享后自动发送”时据此发送。
  @StorageLink('captureShared') @Watch('autoSendShared') sharedId: string = '';
  @StorageLink('captureError') incomingError: string = '';
  @StorageLink('captureNotice') incomingNotice: string = '';
  private runtime: CaptureRuntime | undefined;
  private pairing: PairingLink | undefined;
  private timer: number = -1;
  private visible: boolean = true;
  private connectionCheck: ConnectionCheck = new ConnectionCheck();
  private checkRevision: number = 0;
  // 最近一次会话响应里的 desktopSeenAgoMs（undefined 表示中转没有提供）；desktopPoll 控制已配对后每 30 秒查询一次。
  private desktopAgo: number | null | undefined = undefined;
  private desktopPoll: DesktopPoll = new DesktopPoll();
  // 后台状态查询在途标志：不进入界面状态（refreshing 会让标题转圈、按钮变灰），只防止上一次未返回时重叠。
  private silentRefreshing: boolean = false;

  aboutToAppear(): void {
    this.readingEnabled = readingTrialEnabled();
    this.getUIContext().setKeyboardAvoidMode(KeyboardAvoidMode.RESIZE);
    this.initialize();
  }
  aboutToDisappear(): void {
    this.visible = false; this.stopPolling(); this.holdingObserver.stop();
    if (this.sendTimer !== -1) { clearInterval(this.sendTimer); this.sendTimer = -1; }
    this.autosave.flush().catch(() => {});
    this.pendingDeletes.flush().catch(() => {});
  }
  onPageShow(): void { this.visible = true; this.startPolling(); this.syncHolding(); }
  onPageHide(): void {
    this.visible = false; this.stopPolling(); this.holdingObserver.stop();
    this.autosave.flush().catch(() => {});
    this.pendingDeletes.flush().catch(() => {});
  }
  // 系统返回键/手势：多选中先退出多选，不直接退出应用。
  onBackPress(): boolean {
    if (!this.selecting) return false;
    this.exitSelection();
    return true;
  }

  private animate(change: () => void): void {
    animateInterface(this.getUIContext(), this.settings, change);
  }

  private selectTab(index: number): void {
    // 直接改 index 会跳转；使用控制器让程序内导航也执行 Tabs 原生过渡。
    this.tabsController.changeIndex(index);
  }

  private syncHolding(): void {
    if (this.settingsReady && this.settings.handedness && this.visible && this.foreground) this.holdingObserver.start();
    else {
      this.holdingObserver.stop();
      this.animate(() => { this.holdingSide = 'right'; });
    }
  }

  private retryHolding(): void { this.holdingObserver.stop(); this.syncHolding(); }

  private foregroundChanged(): void {
    this.syncHolding();
    if (this.foreground) { this.startPolling(); this.autoSendShared(); return; }
    this.stopPolling();
    // 进入后台前立即落盘：防抖计时器可能随进程被系统挂起而永远不触发。失败原因已在编辑面板显示。
    this.autosave.flush().catch(() => {});
    // 撤销窗口只在前台有意义：离开前直接完成删除，失败的条目回到列表并在回来时提示。
    this.pendingDeletes.flush().catch(() => {});
  }

  private async initialize(): Promise<void> {
    this.error = '';
    try {
      this.runtime = await CaptureRuntime.open(this.getUIContext().getHostContext() as common.UIAbilityContext);
      const snapshot = this.runtime.queue.snapshot();
      this.input = snapshot.editor; this.mode = snapshot.mode; this.drafts = snapshot.drafts;
      this.sentEntries = this.runtime.sentLog.snapshot(); this.sentLogWarning = this.runtime.sentLog.problem;
      this.syncConnection(); this.ready = true; this.startPolling();
      await this.loadSettings();
      if (this.connected) await this.refresh();
      // 冷启动时分享可能早于页面就绪：设置与连接状态都读完后再判断是否自动发送。
      this.autoSendShared();
    } catch { this.error = '本机数据或安全存储读取失败。请解锁后重试；原数据不会被覆盖。'; }
  }

  private syncConnection(): void {
    this.connected = this.runtime?.connection !== undefined;
    this.origin = this.runtime?.connection?.origin ?? '';
  }

  private reloadDrafts(): void {
    if (this.runtime) {
      const rows = this.runtime.queue.snapshot().drafts;
      this.animate(() => { this.drafts = rows; });
      this.pruneSelection(rows);
    }
  }

  // 草稿被发送、删除或隐藏后，多选集合只保留仍可见的条目；没有可选的草稿时自动退出多选。
  private pruneSelection(rows: Draft[]): void {
    if (!this.selecting) return;
    const visible = rows.map((draft: Draft): string => draft.requestId).filter((id: string): boolean => !this.hiddenIds.includes(id));
    this.selectedIds = keepExisting(this.selectedIds, visible);
    if (visible.length === 0) this.exitSelection();
  }

  // 自动检查：等待电脑确认时约每 2 秒一次（受 ConnectionCheck 退避约束）；配对后放慢到每 30 秒一次，只为刷新电脑在线状态。
  private startPolling(): void {
    if (!this.ready || !this.visible || !this.foreground || this.timer !== -1) return;
    this.refresh(false, this.watchingHistory());
    this.timer = setInterval(() => {
      if (this.connected) this.refresh(false, this.watchingHistory());
    }, 2000);
  }

  // 近期记录里还有“等待电脑取件”的条目时，自动检查顺带刷新近期记录，让状态及时从“等待”变为“已接收”；没有等待项就不额外请求。
  private watchingHistory(): boolean { return this.paired && waitingCount(this.history) > 0; }

  // 记录会话检查的结果：配对状态与电脑在线状态一起更新（每次都以本次响应为准，旧中转没有该字段就回到“未知”）。
  private applySession(session: Session): void {
    this.paired = session.paired;
    this.desktopAgo = session.desktopSeenAgoMs;
    this.syncDesktop();
  }

  // 重新计算电脑在线状态行（会话读数或近期记录变化后调用）。只有已配对时才有意义：待确认、已撤销的设备一律按未知处理。
  private syncDesktop(): void {
    this.desktop = desktopStatusOf(this.paired ? this.desktopAgo : undefined, this.paired ? waitingCount(this.history) : 0);
  }

  // 连不上中转时电脑状态未知：不显示过期的“在线”。
  private forgetDesktop(): void {
    this.desktopAgo = undefined; this.syncDesktop();
  }

  // 解除配对或重新配对：忘掉旧读数，并让下一次自动检查立即到期。
  private clearDesktop(): void {
    this.desktopPoll.reset(); this.forgetDesktop();
  }

  private stopPolling(): void { if (this.timer !== -1) clearInterval(this.timer); this.timer = -1; }

  private async act(action: () => Promise<void>, message: string = ''): Promise<void> {
    if (this.busy) return;
    // 点击反馈立即生效，不等网络，也不随页面过渡延后。
    this.busy = true; this.error = ''; this.notice = ''; this.operation = message; this.operationSeconds = 0;
    this.checkRevision++;
    const startedAt = Date.now();
    const timer = message ? setInterval(() => { this.operationSeconds = Math.floor((Date.now() - startedAt) / 1000); }, 1000) : -1;
    try { await action(); }
    catch (error) { this.animate(() => { this.error = (error as Error).message || '操作失败，请重试'; }); }
    finally {
      clearInterval(timer); this.operation = ''; this.busy = false; this.syncConnection(); this.reloadDrafts();
      if (message && !this.error && this.foreground && this.visible) this.refresh(false, false);
    }
  }

  private async refresh(manual: boolean = true, loadHistory: boolean = true): Promise<void> {
    const client = this.runtime?.client;
    if (!client || this.refreshing || this.busy) return;
    if (!manual && (!this.visible || !this.foreground)) return;
    // 已配对后的自动检查只为刷新电脑在线状态：每 30 秒一次，并保持安静（不转圈、不弹红色横幅）；等待确认时沿用连接检查自身的节奏。
    const silent = !manual && this.paired;
    if (silent && (this.silentRefreshing || !this.desktopPoll.due(Date.now()))) return;
    if (!this.connectionCheck.canCheck(manual)) return;
    const revision = this.checkRevision;
    if (silent) this.silentRefreshing = true; else this.refreshing = true;
    this.desktopPoll.started(Date.now());
    // 会话检查已成功：之后近期记录的失败不应清掉电脑状态。
    let sessionChecked = false;
    try {
      const session = await client.session();
      // 解绑或配对期间产生的旧响应不得覆盖新连接状态。
      if (client !== this.runtime?.client || revision !== this.checkRevision) return;
      sessionChecked = true;
      const newlyPaired = !this.paired && session.paired;
      this.applySession(session);
      this.connectionCheck.reset(); this.automaticCheck = true;
      // 后台检查不擦掉用户可见的提示（如发送后记录刷新失败）：等近期记录重新读取成功才撤掉；配对已失效时旧提示不再适用。
      if (!silent || !this.paired) this.statusError = '';
      if (newlyPaired) { this.error = ''; this.notice = '绑定成功，可以开始收集了'; }
      else if (manual && !this.paired) this.notice = '尚未收到电脑确认，请在电脑「设置 → 手机收集」批准；无需重复提交。';
      // 后台检查发现已配对的设备被电脑撤销：明确告知，而不是悄悄回到“等待确认”。
      else if (silent && !this.paired) this.notice = '与电脑的配对已失效，请在电脑「设置 → 手机收集」重新确认，或重新扫码配对。';
      // 绑定成功立即显示，自动轮询不再串行等待近期记录；后台检查遇到未消除的提示时，顺带重读一次近期记录。
      if (this.paired && (loadHistory || (silent && this.statusError !== ''))) {
        const rows = await client.history();
        if (client !== this.runtime?.client || revision !== this.checkRevision) return;
        this.applyHistory(rows);
        this.statusError = '';
      }
    } catch (error) {
      if (client === this.runtime?.client && revision === this.checkRevision) {
        if (!sessionChecked) this.forgetDesktop();
        this.connectionCheck.failed(error as Error); this.automaticCheck = this.connectionCheck.automatic;
        // 后台状态查询失败只让电脑状态回到“未知”：发送与手动检查仍会给出明确原因，不用红色横幅打扰。
        if (!silent) this.statusError = (error as Error).message;
      }
    }
    finally { if (silent) this.silentRefreshing = false; else this.refreshing = false; }
  }

  private async pullHistory(): Promise<void> {
    try { if (this.connected && !this.busy) await this.refresh(true, true); }
    finally { this.historyPulling = false; }
  }

  // 收到最新一页记录：替换最新的部分，保留用户已经“加载更多”翻出的更早记录。
  private applyHistory(rows: Receipt[]): void {
    const page = firstPage(this.history, this.historyMore, rows);
    this.history = page.receipts; this.historyMore = page.more; this.historyLoaded = true;
    this.syncDesktop();
  }

  // 追加更早的一页记录。失败在页面顶部提示并保留已加载的内容，可再次点击重试。
  private async loadMoreHistory(): Promise<void> {
    const client = this.runtime?.client;
    if (!client || !this.historyMore || this.historyLoadingMore || this.history.length === 0) return;
    const revision = this.checkRevision;
    this.historyLoadingMore = true;
    try {
      const older = await client.history(historyCursor(this.history));
      // 解绑或重新配对期间返回的旧响应不能写进新连接的列表。
      if (client !== this.runtime?.client || revision !== this.checkRevision) return;
      const page = nextPage(this.history, older);
      this.history = page.receipts; this.historyMore = page.more;
      this.syncDesktop();
    } catch (error) {
      this.animate(() => { this.error = `加载更多记录失败：${(error as Error).message || '请稍后重试'}`; });
    } finally {
      this.historyLoadingMore = false;
    }
  }

  private async loadSettings(): Promise<void> {
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      const store = await AppPreferencesStore.open(context);
      const value = await store.load();
      applyColorMode(context, value);
      this.settings = value; this.palette = themePalette(value.themeColor); this.settingsStore = store; this.settingsReady = true; this.syncHolding();
    } catch { this.error = '设置读取失败，原设置未覆盖。请点击重试；本机收集仍可使用。'; }
  }

  private async updateSettings(value: AppPreferences): Promise<void> {
    if (!this.settingsStore || !this.settingsReady) throw new Error('请先重试读取设置');
    await this.settingsStore.save(value);
    this.settings = value; this.palette = themePalette(value.themeColor); this.syncHolding();
    applyColorMode(this.getUIContext().getHostContext() as common.UIAbilityContext, value);
  }

  private async resetSettings(): Promise<void> {
    const result = await this.getUIContext().getPromptAction().showDialog({
      title: '恢复默认设置？', message: '仅重置外观和收集偏好。草稿与电脑连接会保留。',
      buttons: [{ text: '取消', color: $r('app.color.text_secondary') }, { text: '恢复默认', color: this.palette.accent }],
      systemMaterial: panelMaterial(this.settings)
    });
    if (result.index === 1) { await this.updateSettings(new AppPreferences()); this.notice = '已恢复默认设置'; }
  }

  private openEditor(): void {
    if (this.busy) return;
    if (!this.input.trim()) this.mode = this.settings.defaultMode;
    this.error = ''; this.showEditor = true;
  }

  // 输入变化时只登记；连续输入停顿约 400ms 后合并成一次落盘。
  private persistEditor(): void {
    if (!this.runtime) return;
    this.autosave.schedule(this.input, this.mode);
  }

  // 立即以输入框当前内容落盘并等待完成，失败则抛出。
  // 关闭、保存、编辑草稿前必须调用：既写出待落盘输入，也避免过期计时器之后覆盖新的编辑内容。
  private async flushEditor(): Promise<void> {
    this.autosave.schedule(this.input, this.mode);
    await this.autosave.flush();
  }

  private closeEditor(): void {
    if (this.busy) return;
    // 明确落盘后才收起；失败时编辑面板保留，错误在面板内可见。
    this.act(async () => {
      await this.flushEditor();
      this.editorError = ''; this.showEditor = false;
    });
  }

  private async saveDraft(): Promise<void> {
    const runtime = this.runtime;
    if (!runtime) return;
    await this.flushEditor();
    await runtime.queue.enqueue(util.generateRandomUUID(true));
    this.input = ''; this.editorError = ''; this.showEditor = false;
    this.selectTab(0); this.notice = '已保存到收集箱，确认后即可发送';
  }

  // 保存并发送：先把输入框内容落成草稿（先落盘），关闭编辑面板，再在后台发送这一条。
  // 只有本地保存这一步占用页面锁；网络发送的进度显示在草稿卡片上。
  private async saveAndSend(): Promise<void> {
    const runtime = this.runtime;
    if (!runtime) return;
    await this.flushEditor();
    const requestId = util.generateRandomUUID(true);
    await runtime.queue.enqueue(requestId);
    this.input = ''; this.editorError = ''; this.showEditor = false;
    this.selectTab(0);
    this.reloadDrafts();
    const draft = runtime.queue.snapshot().drafts.find((item: Draft) => item.requestId === requestId);
    if (draft) this.sendDraft(draft);
  }

  // 输入超过 20 个链接时，按每份 20 个拆成多条草稿并一次落盘：全部保存或一条都不保存，不会只留下前几份。
  // 拆分后的草稿都是“链接”模式；链接模式本来就只采集链接，链接以外的文字不会保留，与直接发送的语义一致。
  private async saveSplit(): Promise<void> {
    const runtime = this.runtime;
    if (!runtime) return;
    const parse: CaptureParse = parseCapture(this.input, this.mode);
    if (parse.code !== 'too_many_links') throw new Error('当前内容不需要拆分，请直接保存');
    // 先落盘并取消待触发的防抖写入，否则它稍后可能把已拆分的原文重新写回输入框。
    await this.flushEditor();
    const chunks: string[][] = splitUrlChunks(parse.urls, MAX_ITEMS);
    const ids: string[] = [];
    const inputs: string[] = [];
    for (let i = 0; i < chunks.length; i++) {
      ids.push(util.generateRandomUUID(true));
      inputs.push(chunks[i].join('\n'));
    }
    await runtime.queue.enqueueMany(ids, inputs, 'urls');
    this.input = ''; this.editorError = ''; this.showEditor = false;
    this.selectTab(0);
    this.notice = `已按每份 ${MAX_ITEMS} 个链接拆成 ${chunks.length} 条草稿（共 ${parse.linkCount} 个链接），确认后即可发送`;
  }

  // 登记一条开始发送并按需启动每秒刷新的计时器；已在发送中返回 false。
  private beginSending(requestId: string): boolean {
    if (sendingFind(this.sendings, requestId) !== undefined) return false;
    this.sendings = sendingBegin(this.sendings, requestId, Date.now());
    if (this.sendTimer === -1) {
      this.sendTimer = setInterval(() => { this.sendings = sendingTick(this.sendings, Date.now()); }, 1000);
    }
    return true;
  }

  private endSending(requestId: string): void {
    this.sendings = sendingEnd(this.sendings, requestId);
    if (this.sendings.length === 0 && this.sendTimer !== -1) { clearInterval(this.sendTimer); this.sendTimer = -1; }
  }

  private sendingSecondsOf(requestId: string): number {
    const entry = sendingFind(this.sendings, requestId);
    return entry === undefined ? 0 : entry.seconds;
  }

  // 单条发送：不经过 act()，不占用页面锁，只登记这一条的发送状态。失败原因既显示在卡片上，也在页面顶部提示。
  private async sendDraft(draft: Draft): Promise<boolean> {
    if (!this.beginSending(draft.requestId)) return false;
    this.error = ''; this.notice = '';
    try {
      await this.sendOne(draft);
      hapticSuccess(this.settings);
      return true;
    } catch (error) {
      this.animate(() => { this.error = (error as Error).message || '发送失败，请重试'; });
      return false;
    } finally {
      this.endSending(draft.requestId);
      this.syncConnection(); this.reloadDrafts();
    }
  }

  private async sendOne(draft: Draft): Promise<void> {
    const runtime = this.runtime;
    if (!runtime?.client || !runtime.connection) throw new Error('请先在“连接”页面配对电脑');
    // 已确认配对时直接发送：凭证失效会得到明确的 401 提示，也省掉一次往返；尚未确认时先查会话，给出更准确的原因。
    if (!this.paired) {
      const session = await runtime.client.session();
      this.applySession(session);
      if (!session.paired) throw new Error('请在电脑上的归知确认这台手机');
    }
    const receipt = await runtime.queue.send(draft.requestId, runtime.connection.pairingId, runtime.client);
    await this.recordSent(draft, receipt);
    this.notice = '中转已接收，电脑上线后将处理';
    await this.refreshHistoryAfterSend();
  }

  // 发送成功后在本机留一条记录（内容预览与来源），近期页据此显示回执对应的是什么内容。
  // 记录只是辅助信息：写入失败不影响这次发送，只在近期页提示；本方法不会抛出。
  private async recordSent(draft: Draft, receipt: Receipt): Promise<void> {
    const log = this.runtime?.sentLog;
    if (!log) return;
    try {
      await log.append(buildSentEntry(draft.input, draft.mode, receipt, Date.now()));
      this.sentLogWarning = '';
    } catch (error) {
      this.sentLogWarning = `发送记录未能保存：${(error as Error).message || '未知原因'}。近期页可能不显示这条内容的预览`;
    }
    this.sentEntries = log.snapshot();
  }

  // 清除本机发送记录：只影响近期页的内容预览，草稿、电脑上已收集的内容与中转回执都不受影响。
  private async clearSentLog(): Promise<void> {
    const log = this.runtime?.sentLog;
    if (!log) return;
    const result = await this.getUIContext().getPromptAction().showDialog({
      title: '清除发送记录？', systemMaterial: panelMaterial(this.settings),
      message: '只清除本机保存的内容预览与来源，近期页里的旧回执会只显示条数。草稿与电脑上已收集的内容不受影响。',
      buttons: [{ text: '取消', color: $r('app.color.text_secondary') }, { text: '清除', color: $r('app.color.error') }]
    });
    if (result.index !== 1) return;
    await log.clear();
    this.sentEntries = log.snapshot(); this.sentLogWarning = ''; this.notice = '已清除本机发送记录';
  }

  // 在系统浏览器打开来源链接；打不开时说明原因，并提示可改用复制。
  private async openUrl(url: string): Promise<void> {
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      await context.openLink(url, { appLinkingOnly: false });
    } catch (error) {
      this.animate(() => { this.error = `无法打开链接：${(error as Error).message || '未知原因'}。可改用“复制链接”`; });
    }
  }

  private async refreshHistoryAfterSend(): Promise<void> {
    const client = this.runtime?.client;
    if (!client) return;
    try { this.applyHistory(await client.history()); this.statusError = ''; }
    catch { this.statusError = '提交已成功，近期记录刷新失败，请重试刷新'; }
  }

  // 列表里可见的草稿编号（本机队列里的草稿，去掉延迟删除中的）。
  private visibleIds(): string[] {
    const rows: Draft[] = this.runtime ? this.runtime.queue.snapshot().drafts : [];
    return rows.map((draft: Draft): string => draft.requestId).filter((id: string): boolean => !this.hiddenIds.includes(id));
  }

  // 全部发送：所有可见草稿；多选发送只发选中的几条，二者共用同一套发送与汇总流程。
  private async sendAll(): Promise<void> { await this.runBatch(this.visibleIds()); }

  private async sendSelected(): Promise<void> {
    if (this.batchTotal > 0 || this.busy) return;
    const ids = keepExisting(this.selectedIds, this.visibleIds());
    this.exitSelection();
    await this.runBatch(ids);
  }

  // 逐条串行，预检会话与结束后的近期记录刷新各只做一次；界面在整个过程中保持可用。
  // 配对、解绑、编辑草稿等占用页面锁的操作进行中不启动：它们会改变连接或草稿本身。
  private async runBatch(ids: string[]): Promise<void> {
    const runtime = this.runtime;
    if (!runtime || this.batchTotal > 0 || this.busy || ids.length === 0) return;
    this.error = ''; this.notice = '';
    this.batchDone = 0; this.batchTotal = ids.length;
    try {
      if (!runtime.client || !runtime.connection) throw new Error('请先在“连接”页面配对电脑');
      const session = await runtime.client.session();
      this.applySession(session);
      if (!session.paired) throw new Error('请在电脑上的归知确认这台手机');
      const hooks = new BatchHooks();
      hooks.started = (requestId: string): void => { this.beginSending(requestId); };
      hooks.finished = (requestId: string, done: number, _total: number): void => {
        this.endSending(requestId); this.batchDone = done; this.reloadDrafts();
      };
      hooks.sent = (draft: Draft, receipt: Receipt): Promise<void> => this.recordSent(draft, receipt);
      const result = await sendBatch(runtime.queue, runtime.connection.pairingId, runtime.client, hooks, ids);
      if (result.sent.length > 0) await this.refreshHistoryAfterSend();
      this.reportBatch(result);
    } catch (error) {
      this.animate(() => { this.error = (error as Error).message || '发送失败，请重试'; });
    } finally {
      this.batchTotal = 0; this.batchDone = 0;
      this.syncConnection(); this.reloadDrafts();
    }
  }

  // 只有全部成功才提示成功；否则汇总数量与原因，界面内联展示前几条，完整原因可复制。
  private reportBatch(result: BatchResult): void {
    const headline = batchHeadline(result);
    if (result.ok) { this.notice = headline; hapticSuccess(this.settings); return; }
    const inline = batchReport(result, 3);
    const message = inline ? `${headline}\n${inline}` : headline;
    this.errorDetail = `${headline}\n${batchReport(result, 0)}`;
    this.errorDetailFor = message;
    this.animate(() => { this.error = message; });
  }

  // 开启“分享后自动发送”时，把刚落盘的分享草稿交给发送流程。
  // 条件不满足（设置未读完、未连接、不在前台）时不标记已处理，之后条件满足再判断；始终不影响草稿本身，用户可手动发送。
  private autoSendShared(): void {
    const requestId = this.sharedId;
    if (!requestId || requestId === this.handledShareId) return;
    // 页面、设置或前台状态暂时不具备判断条件：保留待办，条件满足后再决定。
    if (!this.ready || !this.settingsReady || !this.foreground || !this.visible) return;
    // 一次分享只做一次决定：之后才开启设置或才连接电脑，都不会回头发送这条旧分享。
    this.handledShareId = requestId;
    if (!this.settings.autoSendShare || !this.connected) return;
    const draft = this.runtime?.queue.snapshot().drafts.find((item: Draft) => item.requestId === requestId);
    if (draft) this.sendDraft(draft);
  }

  // 删除草稿（单条滑动删除、多选删除共用）：先隐藏并给 8 秒撤销窗口，到点才真正删除。
  // 曾尝试发送过的草稿仍先确认：它们可能已被服务端接收，删除本机草稿不会撤回投递；选择“保留”时不改动任何东西（含多选状态）。
  // leaveSelection：多选删除成功进入撤销窗口后退出多选。
  private async requestDelete(drafts: Draft[], leaveSelection: boolean = false): Promise<void> {
    const runtime = this.runtime;
    if (!runtime || drafts.length === 0) return;
    // 正在发送的草稿不能删除：发送结果回来前删除会破坏它与回执的对应关系。
    const removable = drafts.filter((draft: Draft): boolean =>
      !runtime.queue.isSending(draft.requestId) && sendingFind(this.sendings, draft.requestId) === undefined);
    const blocked = drafts.length - removable.length;
    if (removable.length === 0) {
      this.animate(() => { this.error = '正在发送的草稿不能删除，请等待发送结果'; });
      return;
    }
    const attempted = removable.filter((draft: Draft): boolean => draft.attempted).length;
    if (attempted > 0) {
      const result = await this.getUIContext().getPromptAction().showDialog({
        title: '删除已尝试发送的草稿？', systemMaterial: panelMaterial(this.settings),
        message: (attempted === 1 && removable.length === 1 ? '此条' : `其中 ${attempted} 条`) +
          '可能已被服务端接收。删除本机草稿不会撤回投递，请先核对近期记录。',
        buttons: [{ text: '保留', color: $r('app.color.text_secondary') }, { text: '删除草稿', color: $r('app.color.error') }]
      });
      if (result.index !== 1) return;
    }
    const scheduled = this.pendingDeletes.schedule(removable.map((draft: Draft): string => draft.requestId));
    this.undoMessage = removable.length === 1 ? '已删除 1 条草稿' : `已删除 ${removable.length} 条草稿`;
    if (blocked > 0) this.notice = `${blocked} 条正在发送，未删除`;
    if (leaveSelection) this.exitSelection();
    await scheduled;
  }

  private async deleteSelected(): Promise<void> { await this.requestDelete(this.selectedDrafts(), true); }

  // 撤销窗口内撤销：草稿恢复显示，本机数据从未被改动。撤销条随窗口关闭由 deleteHooks().changed 收起。
  private undoDelete(): void {
    const restored = this.pendingDeletes.undo();
    if (restored.length > 0) this.notice = restored.length === 1 ? '已撤销删除' : `已撤销删除 ${restored.length} 条`;
  }

  // 真正删除：逐条记账，单条失败不影响其他条，失败原因交给 deleteHooks().failed 汇总展示。
  private async commitDeletes(ids: string[]): Promise<DeleteFailure[]> {
    const queue = this.runtime?.queue;
    const failures: DeleteFailure[] = [];
    for (let i = 0; i < ids.length; i++) {
      try {
        if (queue === undefined) throw new Error('本机数据尚未就绪');
        await queue.remove(ids[i]);
      } catch (error) {
        const failure = new DeleteFailure();
        failure.requestId = ids[i]; failure.message = (error as Error).message || '删除失败';
        failures.push(failure);
      }
    }
    return failures;
  }

  private deleteHooks(): DeleteHooks {
    const hooks = new DeleteHooks();
    hooks.changed = (hidden: string[], undoable: boolean): void => {
      this.hiddenIds = hidden;
      if (!undoable) this.undoMessage = '';
      // 隐藏集合变化后重新读取本机草稿：删除完成的条目消失，撤销或删除失败的条目重新出现。
      this.reloadDrafts();
    };
    hooks.failed = (failures: DeleteFailure[]): void => { this.reportDeleteFailures(failures); };
    return hooks;
  }

  // 删除失败的草稿已经恢复显示：分别说明每一条的原因，界面内联展示前几条，完整原因可复制。
  private reportDeleteFailures(failures: DeleteFailure[]): void {
    const lines: string[] = failures.map((failure: DeleteFailure): string => {
      const draft = this.drafts.find((item: Draft): boolean => item.requestId === failure.requestId);
      return `· ${draft === undefined ? '一条草稿' : previewText(draft.input, 28)} — ${failure.message}`;
    });
    const headline = `${failures.length} 条草稿没能删除，已恢复显示`;
    const inline = lines.slice(0, 3);
    if (lines.length > 3) inline.push(`· 另有 ${lines.length - 3} 条，可点“复制原因”查看全部`);
    const message = `${headline}\n${inline.join('\n')}`;
    this.errorDetail = `${headline}\n${lines.join('\n')}`;
    this.errorDetailFor = message;
    this.animate(() => { this.error = message; });
  }

  // 长按草稿进入多选，并选中这一条；批量发送或删除进行中不进入。
  private enterSelection(requestId: string): void {
    if (this.busy || this.batchTotal > 0 || this.hiddenIds.includes(requestId)) return;
    this.animate(() => { this.selecting = true; this.selectedIds = [requestId]; });
  }

  private toggleSelection(requestId: string): void {
    if (this.selecting) this.selectedIds = toggleId(this.selectedIds, requestId);
  }

  private exitSelection(): void {
    if (!this.selecting && this.selectedIds.length === 0) return;
    this.animate(() => { this.selecting = false; this.selectedIds = []; });
  }

  // 全选与取消全选：只针对当前可见的草稿。
  private toggleSelectAll(): void {
    const ids = this.visibleIds();
    this.selectedIds = allSelected(this.selectedIds, ids) ? [] : ids;
  }

  private selectedDrafts(): Draft[] {
    return this.drafts.filter((draft: Draft): boolean => this.selectedIds.includes(draft.requestId) && !this.hiddenIds.includes(draft.requestId));
  }

  // 收集页展示的草稿：最新在前，去掉延迟删除中的。
  private visibleDrafts(): Draft[] {
    return this.drafts.filter((draft: Draft): boolean => !this.hiddenIds.includes(draft.requestId)).reverse();
  }

  // 页签栏是否收起：键盘弹出、多选中、设置详情页里都收起。
  private barHidden(): boolean {
    return this.keyboardVisible || this.selecting || (this.selectedTab === 3 && this.settingsDetail);
  }

  // 列表末尾为页签栏、多选栏、撤销条预留的空间，避免最后一条被浮层挡住。
  private contentEndOffset(): number {
    if (this.selecting) return CONTENT_END_OFFSET_SELECTING;
    return this.undoMessage !== '' ? CONTENT_END_OFFSET_UNDO : CONTENT_END_OFFSET;
  }

  // 配对链接输入变化后，之前解析出的服务地址不再可信，需要重新解析。
  private editPairingText(value: string): void {
    this.pairingText = value; this.pairing = undefined; this.pairingOrigin = '';
  }

  private editDeviceName(value: string): void { this.deviceName = value; }

  private togglePairingLink(): void { this.animate(() => { this.showPairingLink = !this.showPairingLink; }); }

  private async editDraft(draft: Draft): Promise<void> {
    if (!this.runtime) return;
    await this.flushEditor();
    await this.runtime.queue.restoreForEdit(draft.requestId);
    const state = this.runtime.queue.snapshot(); this.input = state.editor; this.mode = state.mode;
    this.editorError = ''; this.showEditor = true; this.notice = '';
  }

  private async scan(): Promise<void> {
    const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
    const result = await scanBarcode.startScanForResult(context, {
      scanTypes: [scanCore.ScanType.QR_CODE], enableMultiMode: false, enableAlbum: true
    });
    this.inspectPairing(result.originalValue);
  }

  private inspectPairing(raw: string): void {
    this.pairing = parsePairingLink(raw); this.pairingOrigin = this.pairing.origin; this.pairingText = '';
    this.showPairingLink = true; this.notice = '请核对服务地址，再确认配对';
  }

  private async pair(): Promise<void> {
    if (!this.runtime || !this.pairing) throw new Error('请先扫描或解析配对二维码');
    await this.runtime.pair(this.pairing, this.deviceName, (message: string) => { this.operation = message; });
    this.paired = false; this.clearDesktop(); this.statusError = '';
    this.connectionCheck.reset(); this.automaticCheck = true;
    this.pairing = undefined; this.pairingOrigin = '';
    this.notice = '已发起配对，请在电脑上的归知确认这台手机';
  }

  private async retryPair(): Promise<void> {
    if (!this.runtime?.client) return;
    this.operation = '正在检查电脑确认结果…';
    try {
      const session = await this.runtime.client.session();
      this.applySession(session);
      this.notice = session.paired ? '绑定成功，可以开始收集了' : '请求已送达，请在电脑「设置 → 手机收集」确认；无需重复提交。';
    } catch (error) {
      // 只有服务端明确尚未识别凭证才补发；超时不能当作未提交。
      if (!(error instanceof RelayRequestError) || error.status !== 401) throw error as Error;
      this.operation = '正在重新提交配对请求…';
      await this.runtime.client.claim(); this.notice = '请求已提交，请在电脑确认';
    }
    this.statusError = ''; this.connectionCheck.reset(); this.automaticCheck = true;
  }

  private async disconnect(localOnly: boolean): Promise<void> {
    const result = await this.getUIContext().getPromptAction().showDialog({
      title: localOnly ? '仅清除本机连接？' : '解除与电脑的配对？', systemMaterial: panelMaterial(this.settings),
      message: localOnly ? '无法联网时可清除本机凭证，但仍需在电脑的“手机收集”中撤销此设备。草稿会保留。' : '服务端将撤销此设备的权限。本机草稿保留，已投递内容不会撤回。',
      buttons: [{ text: '取消', color: $r('app.color.text_secondary') }, { text: '确认解除', color: $r('app.color.error') }]
    });
    if (result.index !== 1 || !this.runtime) return;
    if (localOnly) await this.runtime.forget(); else await this.runtime.disconnect();
    this.paired = false; this.history = []; this.historyLoaded = false; this.historyMore = false; this.statusError = '';
    this.clearDesktop();
    this.connectionCheck.reset(); this.automaticCheck = true;
    this.notice = localOnly ? '本机连接已清除，请在电脑撤销原设备' : '配对已解除';
  }

  // 复制到系统剪贴板；成功与失败都给出可见反馈，失败时提示改用长按复制。
  private async copyText(text: string, done: string): Promise<void> {
    try {
      await pasteboard.getSystemPasteboard().setData(pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, text));
      this.notice = done;
    } catch { this.notice = '复制失败，请长按文字复制'; }
  }

  private async copyError(): Promise<void> {
    // 不经过 act：它会先清除当前错误，导致复制到空内容。
    // 批量发送的完整逐条原因只在文案未变化时使用，避免复制到已过期的内容。
    const primary = this.error && this.error === this.errorDetailFor ? this.errorDetail : this.error;
    const detail = [primary, this.statusError, this.incomingError].filter((value: string) => value.length > 0).join('\n');
    await this.copyText(detail, '错误信息已复制');
  }

  // @test-boundary 此行之前是页面控制逻辑，Node 测试会真实执行；之后是 ArkUI Builder，只由 Hvigor 编译验证。
  @Builder
  private EditorSheet() {
    CaptureEditor({ input: $input, mode: $mode, busy: this.busy, saving: this.editorSaving,
      error: this.error || this.editorError, settings: this.settings, canSend: this.connected && this.paired,
      onEdit: () => this.persistEditor(), onSave: () => { this.act(() => this.saveDraft()); },
      onSend: () => { this.act(() => this.saveAndSend()); },
      onSplit: () => { this.act(() => this.saveSplit()); },
      onClose: () => this.closeEditor() })
  }

  // 页签图标使用系统符号：选中态用实心变体与强调色，未选中态用次要文字色。
  private tabStyle(icon: Resource, selectedIcon: Resource, label: string): BottomTabBarStyle {
    const normal = new SymbolGlyphModifier(icon);
    normal.fontColor([$r('app.color.text_secondary')]);
    const selected = new SymbolGlyphModifier(selectedIcon);
    selected.fontColor([this.palette.accent]);
    const symbol: TabBarSymbol = { normal: normal, selected: selected };
    return BottomTabBarStyle.of(symbol, label)
      .labelStyle({ selectedColor: this.palette.accent, unselectedColor: $r('app.color.text_secondary'),
        font: { size: 12, weight: FontWeight.Medium } });
  }

  build() {
    Column() {
      if (this.selectedTab !== 3 && this.selectedTab !== 4) {
        PageHeader({ tab: this.selectedTab, settings: this.settings, leftHand: this.holdingSide === 'left',
          loading: this.busy || this.refreshing,
          interactive: this.selectedTab === 0 ? this.ready && !this.busy : this.connected && !this.busy && !this.refreshing,
          onAction: () => { if (this.selectedTab === 0) this.openEditor(); else this.refresh(); } })
      }
      if (this.operation) {
        Row({ space: 10 }) {
          LoadingProgress().width(20).height(20).color(this.palette.accent)
          Text(`${this.operation} 已等待 ${this.operationSeconds} 秒${this.operationSeconds >= 5 ? '\n网络响应较慢，请勿重复点击；结束后会显示结果或重试原因。' : ''}`)
            .fontSize(13).lineHeight(20).fontColor(this.palette.accent).layoutWeight(1)
        }.padding(14).margin({ left: 20, right: 20, bottom: 8 }).borderRadius(14).backgroundColor(this.palette.soft)
          .constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
      }
      if (this.error || this.statusError || this.incomingError) {
        Column({ space: 8 }) {
          Text([this.error, this.statusError, this.incomingError].filter((value: string) => value.length > 0).join('\n'))
            .fontSize(13).lineHeight(20).fontColor($r('app.color.error')).copyOption(CopyOptions.LocalDevice)
          Row({ space: 6 }) {
            SecondaryAction({ label: '复制原因', active: !this.busy, settings: this.settings,
              onAction: () => { this.copyError(); } })
            SecondaryAction({ label: '重试', active: !this.busy && !this.refreshing, settings: this.settings,
              onAction: () => { if (!this.ready) this.initialize(); else if (!this.settingsReady) this.loadSettings(); else this.refresh(); } })
            if (this.incomingError) {
              SecondaryAction({ label: '关闭', active: !this.busy, settings: this.settings,
                onAction: () => { this.incomingError = ''; } })
            }
          }
        }.padding(14).margin({ left: 20, right: 20, bottom: 8 }).borderRadius(16).backgroundColor($r('app.color.surface'))
          .constraintSize({ maxWidth: CONTENT_MAX_WIDTH }).transition(revealTransition(this.settings))
      }
      if (this.notice || this.incomingNotice) {
        Row({ space: 8 }) {
          Text(this.notice || this.incomingNotice).fontSize(13).fontColor(this.palette.accent).layoutWeight(1)
          Button('关闭').height(44).fontSize(12).fontColor($r('app.color.text_secondary')).backgroundColor(Color.Transparent)
            .onClick(() => { this.animate(() => { this.notice = ''; this.incomingNotice = ''; }); })
        }.padding({ left: 16, right: 4 }).margin({ left: 20, right: 20, bottom: 12 })
          .backgroundColor(this.palette.soft).borderRadius(14).constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
          .transition(revealTransition(this.settings))
      }
      // 页签内容与浮在其上的多选栏、撤销条：二者都贴底，不参与页面滚动。
      Stack({ alignContent: Alignment.Bottom }) {
        if (this.ready) {
          Tabs({ barPosition: BarPosition.End, index: this.selectedTab, controller: this.tabsController }) {
            TabContent() {
              CaptureTab({ input: this.input, drafts: this.visibleDrafts(), connected: this.connected, paired: this.paired,
                desktop: this.desktop, busy: this.busy, batchDone: this.batchDone, batchTotal: this.batchTotal,
                sendings: this.sendings, selecting: this.selecting, selectedIds: this.selectedIds,
                settings: this.settings, endOffset: this.contentEndOffset(),
                onCompose: () => { this.openEditor(); },
                onSendAll: () => { this.sendAll(); },
                onSend: (draft: Draft) => { this.sendDraft(draft); },
                onEdit: (draft: Draft) => { this.act(() => this.editDraft(draft)); },
                onDelete: (draft: Draft) => { this.requestDelete([draft]); },
                onEnterSelect: (draft: Draft) => { this.enterSelection(draft.requestId); },
                onToggleSelect: (draft: Draft) => { this.toggleSelection(draft.requestId); },
                onConnect: () => { this.selectTab(2); } })
            }.tabBar(this.tabStyle($r('sys.symbol.inbox'), $r('sys.symbol.inbox_fill'), '收集'))
            TabContent() {
              HistoryTab({ pulling: $historyPulling, items: mergeHistory(this.history, this.sentEntries),
                receiptCount: this.history.length, connected: this.connected, paired: this.paired,
                loaded: this.historyLoaded, statusError: this.statusError, more: this.historyMore,
                loadingMore: this.historyLoadingMore, refreshing: this.refreshing, busy: this.busy,
                sentLogWarning: this.sentLogWarning, settings: this.settings, endOffset: this.contentEndOffset(),
                onConnect: () => { this.selectTab(2); },
                onPull: () => { this.pullHistory(); },
                onLoadMore: () => { this.loadMoreHistory(); },
                onCopyId: (item: HistoryItem) => { this.copyText(item.receipt.requestId, '请求编号已复制'); },
                onCopyUrl: (url: string) => { this.copyText(url, '链接已复制'); },
                onOpenUrl: (url: string) => { this.openUrl(url); },
                onCopyReason: (summary: string) => { this.copyText(summary, '失败原因已复制'); } })
            }.tabBar(this.tabStyle($r('sys.symbol.clock'), $r('sys.symbol.clock_fill'), '近期'))
            TabContent() {
              ConnectionTab({ connected: this.connected, paired: this.paired, origin: this.origin, desktop: this.desktop,
                statusError: this.statusError, hasProblem: this.statusError !== '' || this.error !== '',
                automaticCheck: this.automaticCheck, refreshing: this.refreshing, busy: this.busy,
                operation: this.operation, showPairingLink: this.showPairingLink, pairingText: this.pairingText,
                pairingOrigin: this.pairingOrigin, deviceName: this.deviceName, keyboardVisible: this.keyboardVisible,
                settings: this.settings,
                onRefresh: () => { this.refresh(); },
                onRetryPair: () => { this.act(() => this.retryPair(), '正在检查配对…'); },
                onDisconnect: (localOnly: boolean) => { this.act(() => this.disconnect(localOnly)); },
                onScan: () => { this.act(() => this.scan()); },
                onTogglePairingLink: () => { this.togglePairingLink(); },
                onPairingText: (value: string) => { this.editPairingText(value); },
                onParseLink: () => { this.act(async () => { this.inspectPairing(this.pairingText); }); },
                onDeviceName: (value: string) => { this.editDeviceName(value); },
                onPair: () => { this.act(() => this.pair(), '正在提交配对…'); } })
            }.tabBar(this.tabStyle($r('sys.symbol.computer'), $r('sys.symbol.computer_fill'), '连接'))
            TabContent() {
              SettingsPage({ settings: this.settings, busy: this.busy || !this.settingsReady,
                holdingMessage: this.holdingMessage, onRetryHolding: () => this.retryHolding(), onDetail: (visible: boolean) => { this.settingsDetail = visible; },
                onChange: (value: AppPreferences) => { this.act(() => this.updateSettings(value)); },
                onReset: () => { this.act(() => this.resetSettings()); }, onConnect: () => { this.selectTab(2); },
                sentCount: this.sentEntries.length, onClearSentLog: () => { this.act(() => this.clearSentLog()); } })
            }.tabBar(this.tabStyle($r('sys.symbol.gearshape'), $r('sys.symbol.gearshape_fill'), '设置'))
            if (this.readingEnabled) {
              TabContent() {
                ReadingTab({ settings: this.settings, active: this.selectedTab === 4 })
              }.tabBar(this.tabStyle($r('sys.symbol.book'), $r('sys.symbol.book'), '阅读'))
            }
          }.width('100%').height('100%').vertical(false).barMode(BarMode.Fixed)
            .barOverlap(true).barHeight(this.barHidden() ? 0 : 64, true)
            .scrollable(false).animationDuration(motionDuration(this.settings))
            .animationMode(AnimationMode.CONTENT_FIRST_WITH_JUMP)
            .attributeModifier(new CaptureTabsAppearance(this.barHidden(), this.settings))
            .onChange((index: number) => {
              this.exitSelection();
              this.selectedTab = index;
              if (index === 1 || index === 2) this.refresh(true, index === 1);
            })
        } else if (!this.error) {
          Column() {
            LoadingProgress().width(28).height(28).color(this.palette.accent).margin({ top: 40 })
            Text('正在读取本机草稿…').fontSize(14).fontColor($r('app.color.text_secondary')).padding(16)
          }.width('100%').height('100%')
        }
        if (this.selecting) {
          SelectionBar({ count: this.selectedIds.length, allSelected: allSelected(this.selectedIds, this.visibleIds()),
            connected: this.connected, busy: this.busy || this.batchTotal > 0, settings: this.settings,
            onExit: () => { this.exitSelection(); },
            onToggleAll: () => { this.toggleSelectAll(); },
            onSend: () => { this.sendSelected(); },
            onDelete: () => { this.deleteSelected(); },
            onConnect: () => { this.exitSelection(); this.selectTab(2); } })
            .margin({ left: 16, right: 16, bottom: 12 }).constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
        } else if (this.undoMessage !== '') {
          // 撤销条叠在悬浮页签栏上方；页签栏收起（键盘、设置详情）时贴底。
          UndoBar({ message: this.undoMessage, settings: this.settings, onUndo: () => { this.undoDelete(); } })
            .margin({ left: 16, right: 16, bottom: this.barHidden() ? 12 : 88 })
            .constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
        }
      }.layoutWeight(1).width('100%')
    }.width('100%').height('100%').backgroundColor($r('app.color.page_background'))
      .bindSheet($$this.showEditor, this.EditorSheet(), {
        height: '85%', dragBar: true, showClose: false,
        systemMaterial: panelMaterial(this.settings),
        onWillDismiss: () => { this.closeEditor(); }
      })
      .padding({
        top: this.getUIContext().px2vp(this.safeTop),
        bottom: this.keyboardVisible ? 0 : this.getUIContext().px2vp(this.safeBottom),
        left: this.getUIContext().px2vp(this.safeLeft),
        right: this.getUIContext().px2vp(this.safeRight)
      })
  }
}
