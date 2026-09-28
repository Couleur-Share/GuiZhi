import { PageHeader } from '../components/PageHeader';
import { ThemePalette, themePalette } from '../services/ThemePalette';
import { common } from '@kit.AbilityKit';
import { util } from '@kit.ArkTS';
import { pasteboard } from '@kit.BasicServicesKit';
import { scanBarcode, scanCore } from '@kit.ScanKit';
import { KeyboardAvoidMode } from '@kit.ArkUI';
import { CaptureEditor } from '../components/CaptureEditor';
import { DraftCard } from '../components/DraftCard';
import { ReceiptCard } from '../components/ReceiptCard';
import { SettingsPage } from '../components/SettingsPage';
import { AppPreferences, AppPreferencesStore, applyColorMode } from '../services/AppPreferences';
import { CaptureTabsAppearance, panelMaterial } from '../services/ImmersiveAppearance';
import { animateInterface, revealTransition, pressFeedback, motionDuration } from '../services/InterfaceMotion';
import { HoldingHandObserver, HoldingHandState } from '../services/HoldingHandObserver';
import { CaptureRuntime } from '../services/CaptureRuntime';
import { ConnectionCheck, RelayRequestError } from '../core/ConnectionCheck';
import { Draft, CaptureMode, Receipt, PairingLink, parsePairingLink } from '../core/Protocol';

@Entry
@Component
struct Index {
  @Provide('capturePalette') palette: ThemePalette = themePalette('blue');
  @State ready: boolean = false;
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
  private tabsController: TabsController = new TabsController();
  private holdingObserver: HoldingHandObserver = new HoldingHandObserver((state: HoldingHandState) => {
    this.animate(() => { this.holdingSide = state.side; this.holdingMessage = state.message; });
  });
  private settingsStore: AppPreferencesStore | undefined;
  private editRevision: number = 0;
  @StorageLink('captureSafeTop') safeTop: number = 0;
  @StorageLink('captureSafeBottom') safeBottom: number = 0;
  @StorageLink('captureSafeLeft') safeLeft: number = 0;
  @StorageLink('captureSafeRight') safeRight: number = 0;
  @StorageLink('captureKeyboardVisible') keyboardVisible: boolean = false;
  @StorageLink('captureForeground') @Watch('foregroundChanged') foreground: boolean = false;
  @StorageLink('captureRevision') @Watch('reloadDrafts') revision: number = 0;
  @StorageLink('captureError') incomingError: string = '';
  @StorageLink('captureNotice') incomingNotice: string = '';
  private runtime: CaptureRuntime | undefined;
  private pairing: PairingLink | undefined;
  private timer: number = -1;
  private visible: boolean = true;
  private connectionCheck: ConnectionCheck = new ConnectionCheck();
  private checkRevision: number = 0;

  aboutToAppear(): void {
    this.getUIContext().setKeyboardAvoidMode(KeyboardAvoidMode.RESIZE);
    this.initialize();
  }
  aboutToDisappear(): void { this.visible = false; this.stopPolling(); this.holdingObserver.stop(); }
  onPageShow(): void { this.visible = true; this.startPolling(); this.syncHolding(); }
  onPageHide(): void { this.visible = false; this.stopPolling(); this.holdingObserver.stop(); }

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
    if (this.foreground) this.startPolling(); else this.stopPolling();
  }

  private async initialize(): Promise<void> {
    this.error = '';
    try {
      this.runtime = await CaptureRuntime.open(this.getUIContext().getHostContext() as common.UIAbilityContext);
      const snapshot = this.runtime.queue.snapshot();
      this.input = snapshot.editor; this.mode = snapshot.mode; this.drafts = snapshot.drafts;
      this.syncConnection(); this.ready = true; this.startPolling();
      await this.loadSettings();
      if (this.connected) await this.refresh();
    } catch { this.error = '本机数据或安全存储读取失败。请解锁后重试；原数据不会被覆盖。'; }
  }

  private syncConnection(): void {
    this.connected = this.runtime?.connection !== undefined;
    this.origin = this.runtime?.connection?.origin ?? '';
  }

  private reloadDrafts(): void {
    if (this.runtime) { const rows = this.runtime.queue.snapshot().drafts; this.animate(() => { this.drafts = rows; }); }
  }

  private startPolling(): void {
    if (!this.ready || !this.visible || !this.foreground || this.timer !== -1) return;
    this.refresh(false, false);
    this.timer = setInterval(() => {
      if (this.connected && !this.paired) this.refresh(false, false);
    }, 2000);
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
    if (!manual && (this.paired || !this.visible || !this.foreground)) return;
    if (!this.connectionCheck.canCheck(manual)) return;
    const revision = this.checkRevision;
    this.refreshing = true;
    try {
      const session = await client.session();
      // 解绑或配对期间产生的旧响应不得覆盖新连接状态。
      if (client !== this.runtime?.client || revision !== this.checkRevision) return;
      const newlyPaired = !this.paired && session.paired;
      this.paired = session.paired;
      this.connectionCheck.reset(); this.automaticCheck = true; this.statusError = '';
      if (newlyPaired) { this.error = ''; this.notice = '绑定成功，可以开始收集了'; }
      else if (manual && !this.paired) this.notice = '尚未收到电脑确认，请在电脑「设置 → 手机收集」批准；无需重复提交。';
      // 绑定成功立即显示，自动轮询不再串行等待近期记录。
      if (this.paired && loadHistory) {
        const rows = await client.history();
        if (client !== this.runtime?.client || revision !== this.checkRevision) return;
        this.history = rows; this.historyLoaded = true;
      }
    } catch (error) {
      if (client === this.runtime?.client && revision === this.checkRevision) {
        this.connectionCheck.failed(error as Error); this.automaticCheck = this.connectionCheck.automatic;
        this.statusError = (error as Error).message;
      }
    }
    finally { this.refreshing = false; }
  }

  private async pullHistory(): Promise<void> {
    try { if (this.connected && !this.busy) await this.refresh(true, true); }
    finally { this.historyPulling = false; }
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

  private persistEditor(): void {
    const runtime = this.runtime;
    if (!runtime) return;
    const revision = ++this.editRevision;
    this.editorSaving = true; this.editorError = '';
    runtime.queue.edit(this.input, this.mode).then(() => {
      if (revision === this.editRevision) this.editorSaving = false;
    }).catch(() => {
      if (revision === this.editRevision) {
        this.editorSaving = false; this.editorError = '自动保存失败，请保留输入内容并点击保存重试';
      }
    });
  }

  private closeEditor(): void {
    if (this.busy) return;
    // 明确落盘后才收起；失败时编辑面板保留，错误在面板内可见。
    this.act(async () => {
      await this.runtime?.queue.edit(this.input, this.mode);
      this.editorError = ''; this.showEditor = false;
    });
  }

  private async saveDraft(): Promise<void> {
    const runtime = this.runtime;
    if (!runtime) return;
    await runtime.queue.edit(this.input, this.mode);
    await runtime.queue.enqueue(util.generateRandomUUID(true));
    this.input = ''; this.editorError = ''; this.showEditor = false;
    this.selectTab(0); this.notice = '已保存到收集箱，确认后即可发送';
  }

  private async send(draft: Draft): Promise<void> {
    const runtime = this.runtime;
    if (!runtime?.client || !runtime.connection) throw new Error('请先在“连接”页面配对电脑');
    const session = await runtime.client.session();
    this.paired = session.paired;
    if (!session.paired) throw new Error('请在电脑上的归知确认这台手机');
    await runtime.queue.send(draft.requestId, runtime.connection.pairingId, runtime.client);
    this.notice = '中转已接收，电脑上线后将处理';
    try { this.history = await runtime.client.history(); this.historyLoaded = true; this.statusError = ''; }
    catch { this.statusError = '提交已成功，近期记录刷新失败，请重试刷新'; }
  }

  private async remove(draft: Draft): Promise<void> {
    const result = await this.getUIContext().getPromptAction().showDialog({
      title: '删除本机草稿？', systemMaterial: panelMaterial(this.settings),
      message: draft.attempted ? '此条可能已被服务端接收。删除本机草稿不会撤回投递，请先核对近期记录。' : '这条尚未发送，删除后无法恢复。',
      buttons: [{ text: '保留', color: $r('app.color.text_secondary') }, { text: '删除草稿', color: $r('app.color.error') }]
    });
    if (result.index === 1) await this.runtime?.queue.remove(draft.requestId);
  }

  private async editDraft(draft: Draft): Promise<void> {
    if (!this.runtime) return;
    await this.runtime.queue.edit(this.input, this.mode);
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
    this.paired = false; this.statusError = '';
    this.connectionCheck.reset(); this.automaticCheck = true;
    this.pairing = undefined; this.pairingOrigin = '';
    this.notice = '已发起配对，请在电脑上的归知确认这台手机';
  }

  private async retryPair(): Promise<void> {
    if (!this.runtime?.client) return;
    this.operation = '正在检查电脑确认结果…';
    try {
      const session = await this.runtime.client.session();
      this.paired = session.paired;
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
    this.paired = false; this.history = []; this.historyLoaded = false; this.statusError = '';
    this.connectionCheck.reset(); this.automaticCheck = true;
    this.notice = localOnly ? '本机连接已清除，请在电脑撤销原设备' : '配对已解除';
  }

  private async copyError(): Promise<void> {
    // 不经过 act：它会先清除当前错误，导致复制到空内容。
    const detail = [this.error, this.statusError, this.incomingError].filter((value: string) => value.length > 0).join('\n');
    try {
      await pasteboard.getSystemPasteboard().setData(pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, detail));
      this.notice = '错误信息已复制';
    } catch { this.notice = '复制失败，请长按错误文字复制'; }
  }

  @Builder
  private EmptyState(icon: Resource, title: string, detail: string) {
    Column({ space: 10 }) {
      Image(icon).fillColor(this.palette.accent).padding(18).width(72).height(72)
        .backgroundColor(this.palette.soft).borderRadius(24).margin({ bottom: 6 })
      Text(title).fontSize(16).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
      Text(detail).fontSize(13).lineHeight(21).fontColor($r('app.color.text_secondary')).textAlign(TextAlign.Center)
    }.width('100%').padding({ top: 32, bottom: 32, left: 24, right: 24 })
  }

  @Builder
  private PrimaryAction(label: string, icon: Resource, enabled: boolean, action: () => void) {
    Button({ type: ButtonType.Normal }) {
      Row({ space: 8 }) {
        Image(icon).width(19).height(19)
          .fillColor(enabled ? this.palette.onAccent : $r('app.color.text_tertiary'))
        Text(label).fontSize(16).fontWeight(FontWeight.Medium)
          .fontColor(enabled ? this.palette.onAccent : $r('app.color.text_tertiary'))
      }.justifyContent(FlexAlign.Center).width('100%')
    }.height(50).width('100%').borderRadius(16).enabled(enabled).opacity(1)
      .clickEffect(pressFeedback(this.settings)).backgroundColor(enabled ? this.palette.accent : $r('app.color.surface_muted')).onClick(action)
  }

  @Builder
  private SecondaryAction(label: string, enabled: boolean, action: () => void) {
    Button(label, { type: ButtonType.Normal }).height(44).fontSize(14).borderRadius(12)
      .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
      .enabled(enabled).clickEffect(pressFeedback(this.settings)).onClick(action)
  }

  @Builder
  private EditorSheet() {
    CaptureEditor({ input: $input, mode: $mode, busy: this.busy, saving: this.editorSaving,
      error: this.error || this.editorError, settings: this.settings,
      onEdit: () => this.persistEditor(), onSave: () => { this.act(() => this.saveDraft()); },
      onClose: () => this.closeEditor() })
  }

  @Builder
  private CapturePage() {
    Scroll() {
      Column({ space: 14 }) {
        Text('随手记，稍后在电脑整理。').fontSize(14).fontColor($r('app.color.text_secondary')).width('100%')
        Button({ type: ButtonType.Normal }) {
          Row({ space: 16 }) {
            Column({ space: 8 }) {
              Text(this.input.trim() ? '继续未完成的记录' : '记下一个想法…').fontSize(19).fontWeight(FontWeight.Medium)
                .fontColor($r('app.color.text_primary'))
              Text(this.input.trim() ? this.input : '链接、文字，都可以留在这里').fontSize(13)
                .fontColor($r('app.color.text_secondary')).maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
            }.alignItems(HorizontalAlign.Start).layoutWeight(1)
            Image($r('app.media.ic_compose')).width(26).height(26).fillColor(this.palette.accent)
          }.width('100%').padding(22)
        }.width('100%').constraintSize({ minHeight: 112 }).borderRadius(20)
          .backgroundColor($r('app.color.surface')).enabled(!this.busy)
          .clickEffect(pressFeedback(this.settings)).onClick(() => this.openEditor())
        Row({ space: 8 }) {
          Text('待发送').fontSize(15).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
          Text(`${this.drafts.length}`).fontSize(12).fontColor($r('app.color.text_tertiary'))
          Blank()
          Text('仅保存在本机').fontSize(12).fontColor($r('app.color.text_tertiary'))
        }.width('100%')
        if (this.drafts.length === 0) {
          this.EmptyState($r('app.media.ic_inbox'), '从第一条收集开始', '点击上方记录，或从其他应用\n分享链接与文字到归知。')
        }
        ForEach(this.drafts.slice().reverse(), (draft: Draft, index: number) => {
          DraftCard({ draft: draft, index: index, busy: this.busy, connected: this.connected, settings: this.settings,
            onSend: () => { this.act(() => this.send(draft)); },
            onEdit: () => { this.act(() => this.editDraft(draft)); },
            onRemove: () => { this.act(() => this.remove(draft)); }, onConnect: () => { this.selectTab(2); } })
        }, (draft: Draft) => draft.requestId)
        if (!this.connected) {
          Button({ type: ButtonType.Normal }) {
            Row({ space: 12 }) {
              Image($r('app.media.ic_link')).width(20).height(20).fillColor($r('app.color.text_secondary'))
              Column({ space: 4 }) {
                Text('连接你的电脑').fontSize(14).fontColor($r('app.color.text_primary'))
                Text('随时收集，配对后再发送').fontSize(12).fontColor($r('app.color.text_tertiary'))
              }.layoutWeight(1).alignItems(HorizontalAlign.Start)
              SymbolGlyph($r('sys.symbol.chevron_right')).fontSize(14).fontColor([$r('app.color.text_tertiary')])
            }.padding(16).width('100%')
          }.width('100%').borderRadius(20).backgroundColor($r('app.color.surface')).clickEffect(pressFeedback(this.settings)).onClick(() => { this.selectTab(2); })
        }
      }.padding({ left: 20, right: 20, top: 4, bottom: 112 }).width('100%')
    }.height('100%').width('100%').align(Alignment.TopStart).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
  }

  @Builder
  private HistoryPage() {
    Refresh({ refreshing: $$this.historyPulling }) {
      Scroll() {
        Column({ space: 14 }) {
          Text('追踪接收与处理结果 · 下拉刷新').fontSize(14).fontColor($r('app.color.text_secondary')).width('100%')
          if (!this.connected) {
            this.EmptyState($r('app.media.ic_link'), '连接后查看投递进度', '本机草稿不会自动发送，\n你可以先收集，再连接电脑。')
            this.PrimaryAction('连接电脑', $r('app.media.ic_link'), !this.busy, () => { this.selectTab(2); })
          } else if (!this.historyLoaded && !this.statusError) {
            this.EmptyState($r('app.media.ic_history'), this.paired ? '正在读取记录' : '等待电脑确认', '配对完成后，记录会显示在这里。')
          } else if (this.historyLoaded && this.history.length === 0 && !this.statusError) {
            this.EmptyState($r('app.media.ic_history'), '还没有发送记录', '在收集箱中选一条内容，点击发送。')
          }
          ForEach(this.history, (receipt: Receipt, index: number) => { ReceiptCard({ receipt: receipt, settings: this.settings, index: index }) }, (receipt: Receipt) => receipt.id)
          if (this.historyLoaded && this.history.length > 0) {
            Text('最近 100 条 · 采集失败可在电脑重试').fontSize(12).fontColor($r('app.color.text_tertiary'))
          }
        }.padding({ left: 20, right: 20, top: 4, bottom: 112 }).width('100%')
      }.height('100%').width('100%').align(Alignment.TopStart).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
    }.height('100%').width('100%').pullToRefresh(this.connected && !this.busy && !this.refreshing)
      .onRefreshing(() => { this.pullHistory(); })
  }

  @Builder
  private PairingStep(number: string, title: string, detail: string) {
    Row({ space: 14 }) {
      Text(number).width(30).height(30).textAlign(TextAlign.Center).fontSize(13)
        .fontColor(this.palette.accent).backgroundColor(this.palette.soft).borderRadius(15)
      Column({ space: 5 }) {
        Text(title).fontSize(15).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
        Text(detail).fontSize(13).lineHeight(21).fontColor($r('app.color.text_secondary'))
      }.alignItems(HorizontalAlign.Start).layoutWeight(1)
    }.width('100%').alignItems(VerticalAlign.Top)
  }

  @Builder
  private ConnectionPage() {
    Scroll() {
      Column({ space: 20 }) {
        if (this.connected) {
          Column({ space: 14 }) {
            Image($r('app.media.ic_computer')).width(44).height(44).fillColor(this.palette.accent)
            Text(this.paired ? '已配对归知' : this.statusError || this.error ? '连接需要检查' : '等待电脑确认').fontSize(21).fontWeight(FontWeight.Medium)
              .fontColor($r('app.color.text_primary'))
            Text(this.origin).fontSize(13).fontColor($r('app.color.text_secondary')).copyOption(CopyOptions.LocalDevice)
            Text(this.paired ? '发送后，电脑上线即可接收。' : this.statusError ?
              (this.automaticCheck ? '网络恢复后会自动继续检查，也可以手动重试。' : '请按上方原因处理，再点击重试。') :
              '打开电脑「设置 → 手机收集」确认绑定。此页每 2 秒自动检查，无需重复提交；二维码过期请在电脑重新生成。')
              .fontSize(13).fontColor($r('app.color.text_secondary'))
            this.PrimaryAction(this.refreshing ? '正在检查' : '检查连接', $r('app.media.ic_link'), !this.busy && !this.refreshing,
              () => { this.refresh(); })
            if (!this.paired) {
              this.SecondaryAction(this.operation || '重试配对请求', !this.busy && !this.refreshing,
                () => { this.act(() => this.retryPair(), '正在检查配对…'); })
            }
          }.padding(22).width('100%').backgroundColor($r('app.color.surface')).borderRadius(20)
          Button('管理连接  ▾').height(44).fontSize(14).backgroundColor(Color.Transparent)
            .fontColor($r('app.color.text_secondary')).enabled(!this.busy)
            .bindMenu([
              { value: '解除与电脑的配对', action: () => { this.act(() => this.disconnect(false)); } },
              { value: '无法联网？仅清除本机连接', action: () => { this.act(() => this.disconnect(true)); } }
            ], { systemMaterial: panelMaterial(this.settings) })
        } else {
          Text('让手机里的灵感，回到你的知识库。').fontSize(14).fontColor($r('app.color.text_secondary')).width('100%')
          Column({ space: 24 }) {
            this.PairingStep('1', '在电脑生成二维码', '打开归知 → 设置 → 手机收集')
            this.PairingStep('2', '用手机扫码', '扫描电脑上显示的配对二维码')
            this.PairingStep('3', '回到电脑确认', '批准设备后，即可手动发送收集内容')
          }.width('100%').padding(22).backgroundColor($r('app.color.surface')).borderRadius(20)
          this.PrimaryAction('扫描二维码', $r('app.media.ic_scan'), !this.busy, () => { this.act(() => this.scan()); })
          Button(this.showPairingLink ? '收起链接配对' : '使用配对链接  ›').height(44).fontSize(14)
            .backgroundColor(Color.Transparent).fontColor($r('app.color.text_secondary')).enabled(!this.busy)
            .onClick(() => { this.animate(() => { this.showPairingLink = !this.showPairingLink; }); })
          if (this.showPairingLink) {
            Column({ space: 14 }) {
              Text('粘贴电脑生成的完整链接').fontSize(14).fontColor($r('app.color.text_primary')).width('100%')
              TextInput({ placeholder: 'https://…#pair=…', text: this.pairingText }).height(48).fontSize(14).borderRadius(12)
                .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
                .placeholderColor($r('app.color.text_tertiary')).enabled(!this.busy)
                .onChange((value: string) => { this.pairingText = value; this.pairing = undefined; this.pairingOrigin = ''; })
              this.SecondaryAction('解析链接', !this.busy && this.pairingText.length > 0,
                () => { this.act(async () => { this.inspectPairing(this.pairingText); }); })
              if (this.pairingOrigin) {
                Divider().color($r('app.color.border_subtle'))
                Text(`确认服务地址\n${this.pairingOrigin}`).fontSize(14).lineHeight(22)
                  .fontColor($r('app.color.text_primary')).copyOption(CopyOptions.LocalDevice).width('100%')
                TextInput({ placeholder: '设备名称', text: this.deviceName }).height(48).fontSize(14).borderRadius(12)
                  .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary')).maxLength(60)
                  .enabled(!this.busy).onChange((value: string) => { this.deviceName = value; })
                this.PrimaryAction(this.operation || '确认配对', $r('app.media.ic_link'), !this.busy,
                  () => { this.act(() => this.pair(), '正在提交配对…'); })
              }
            }.padding(18).width('100%').backgroundColor($r('app.color.surface')).borderRadius(18)
              .transition(revealTransition(this.settings))
          }
        }
        Text('收集内容始终由你确认发送。配对与传输使用 HTTPS，设备凭证保存在系统安全存储中。')
          .fontSize(12).lineHeight(21).fontColor($r('app.color.text_tertiary')).width('100%')
      }.padding({ left: 20, right: 20, top: 4, bottom: this.keyboardVisible ? 24 : 112 }).width('100%')
    }.height('100%').width('100%').align(Alignment.TopStart).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
  }

  private tabStyle(icon: Resource, label: string): BottomTabBarStyle {
    return BottomTabBarStyle.of(icon, label)
      .labelStyle({ selectedColor: this.palette.accent, unselectedColor: $r('app.color.text_secondary'),
        font: { size: 12, weight: FontWeight.Medium } })
      .iconStyle({ selectedColor: this.palette.accent, unselectedColor: $r('app.color.text_secondary') });
  }

  build() {
    Column() {
      if (this.selectedTab !== 3) {
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
      }
      if (this.error || this.statusError || this.incomingError) {
        Column({ space: 8 }) {
          Text([this.error, this.statusError, this.incomingError].filter((value: string) => value.length > 0).join('\n'))
            .fontSize(13).lineHeight(20).fontColor($r('app.color.error')).copyOption(CopyOptions.LocalDevice)
          Row({ space: 6 }) {
            this.SecondaryAction('复制原因', !this.busy, () => { this.copyError(); })
            this.SecondaryAction('重试', !this.busy && !this.refreshing, () => { if (!this.ready) this.initialize(); else if (!this.settingsReady) this.loadSettings(); else this.refresh(); })
            if (this.incomingError) {
              this.SecondaryAction('关闭', !this.busy, () => { this.incomingError = ''; })
            }
          }
        }.padding(14).margin({ left: 20, right: 20, bottom: 8 }).borderRadius(16).backgroundColor($r('app.color.surface'))
          .transition(revealTransition(this.settings))
      }
      if (this.notice || this.incomingNotice) {
        Row({ space: 8 }) {
          Text(this.notice || this.incomingNotice).fontSize(13).fontColor(this.palette.accent).layoutWeight(1)
          Button('关闭').height(44).fontSize(12).fontColor($r('app.color.text_secondary')).backgroundColor(Color.Transparent)
            .onClick(() => { this.animate(() => { this.notice = ''; this.incomingNotice = ''; }); })
        }.padding({ left: 16, right: 4 }).margin({ left: 20, right: 20, bottom: 12 })
          .backgroundColor(this.palette.soft).borderRadius(14)
          .transition(revealTransition(this.settings))
      }
      Column() {
        if (this.ready) {
          Tabs({ barPosition: BarPosition.End, index: this.selectedTab, controller: this.tabsController }) {
            TabContent() { this.CapturePage() }
              .tabBar(this.tabStyle($r('app.media.ic_inbox'), '收集'))
            TabContent() { this.HistoryPage() }
              .tabBar(this.tabStyle($r('app.media.ic_history'), '近期'))
            TabContent() { this.ConnectionPage() }
              .tabBar(this.tabStyle($r('app.media.ic_computer'), '连接'))
            TabContent() {
              SettingsPage({ settings: this.settings, busy: this.busy || !this.settingsReady,
                holdingMessage: this.holdingMessage, onRetryHolding: () => this.retryHolding(), onDetail: (visible: boolean) => { this.settingsDetail = visible; },
                onChange: (value: AppPreferences) => { this.act(() => this.updateSettings(value)); },
                onReset: () => { this.act(() => this.resetSettings()); }, onConnect: () => { this.selectTab(2); } })
            }.tabBar(this.tabStyle($r('app.media.ic_settings'), '设置'))
          }.width('100%').height('100%').vertical(false).barMode(BarMode.Fixed)
            .barOverlap(true).barHeight(this.keyboardVisible || (this.selectedTab === 3 && this.settingsDetail) ? 0 : 64, true)
            .scrollable(false).animationDuration(motionDuration(this.settings))
            .animationMode(AnimationMode.CONTENT_FIRST_WITH_JUMP)
            .attributeModifier(new CaptureTabsAppearance(this.keyboardVisible || (this.selectedTab === 3 && this.settingsDetail), this.settings))
            .onChange((index: number) => { this.selectedTab = index; if (index === 1 || index === 2) this.refresh(true, index === 1); })
        } else if (!this.error) {
          LoadingProgress().width(28).height(28).color(this.palette.accent).margin({ top: 40 })
          Text('正在读取本机草稿…').fontSize(14).fontColor($r('app.color.text_secondary')).padding(16)
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
