import { common } from '@kit.AbilityKit';
import { scanBarcode, scanCore } from '@kit.ScanKit';
import { ReadingArticle, ReadingInvite, findArticles } from '../core/ReadingModel';
import { ReadingProgress } from '../core/ReadingLibrary';
import { parseReadingInvite } from '../core/ReadingPairing';
import { ReadingCrypto } from '../services/ReadingCrypto';
import { ReadingRuntime } from '../services/ReadingRuntime';
import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { CONTENT_END_OFFSET, CONTENT_MAX_WIDTH } from '../services/Layout';
import { PrimaryAction, SecondaryAction } from './Actions';
import { ReadingArticleView } from './ReadingArticleView';

@Component
export struct ReadingTab {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop @Watch('visibilityChanged') active: boolean = false;
  @StorageLink('captureForeground') @Watch('visibilityChanged') foreground: boolean = false;
  @State ready: boolean = false;
  @State busy: boolean = false;
  @State error: string = '';
  @State notice: string = '';
  @State origin: string = '';
  @State pending: boolean = false;
  @State paired: boolean = false;
  @State invitationOrigin: string = '';
  @State pairingText: string = '';
  @State articles: ReadingArticle[] = [];
  @State results: ReadingArticle[] = [];
  @State query: string = '';
  @State collection: string = '';
  @State tag: string = '';
  @State collections: string[] = [];
  @State tags: string[] = [];
  @State shown: number = 30;
  @State syncedAt: number = 0;
  @State sequence: number = 0;
  @State progress: string = '';
  @State current: ReadingArticle | null = null;
  @State imageRevision: number = 0;
  private runtime: ReadingRuntime | null = null;
  private invitation: ReadingInvite | null = null;
  private epoch: number = 0;
  private searchTimer: number = -1;
  private scanning: boolean = false;
  aboutToAppear(): void { this.visibilityChanged(); }
  aboutToDisappear(): void { this.lock(); }
  private visibilityChanged(): void {
    if (this.scanning && this.active) return;
    if (this.active && this.foreground) { if (!this.runtime) this.initialize(); }
    else this.lock();
  }
  private lock(): void {
    this.epoch++; clearTimeout(this.searchTimer); this.runtime?.lock(); this.runtime = null;
    this.articles = []; this.results = []; this.collections = []; this.tags = []; this.current = null;
    this.invitation = null; this.invitationOrigin = ''; this.pairingText = ''; this.query = ''; this.collection = ''; this.tag = '';
    this.error = ''; this.notice = ''; this.progress = ''; this.busy = false; this.ready = false;
  }
  private async initialize(): Promise<void> {
    const epoch = this.epoch; this.busy = true; this.error = '';
    try {
      this.runtime = new ReadingRuntime(this.getUIContext().getHostContext() as common.UIAbilityContext);
      await this.runtime.open();
      if (epoch === this.epoch) { this.ready = true; this.refresh(); }
    } catch (error) { if (epoch === this.epoch) { this.error = (error as Error).message; this.refresh(); } }
    finally { if (epoch === this.epoch) this.busy = false; }
  }
  private refresh(): void {
    this.origin = this.runtime?.connection?.origin || '';
    this.paired = this.runtime?.connection?.anchor !== null && this.runtime?.connection?.anchor !== undefined;
    this.pending = this.origin !== '' && !this.paired;
    this.articles = this.runtime?.library?.articles.slice() || [];
    this.sequence = this.runtime?.library?.current?.head.sequence || 0;
    this.syncedAt = this.runtime?.library?.current?.syncedAt || 0;
    this.collections = Array.from(new Set<string>(this.articles.map((article: ReadingArticle): string => article.collectionName)));
    const tags: string[] = []; this.articles.forEach((article: ReadingArticle) => article.tags.forEach((tag: string) => { if (!tags.includes(tag)) tags.push(tag); }));
    this.tags = tags; this.filter();
  }
  private filter(): void { this.results = findArticles(this.articles, this.query, this.collection, this.tag); this.shown = 30; }
  private search(value: string): void {
    this.query = value; clearTimeout(this.searchTimer);
    this.searchTimer = setTimeout(() => { this.filter(); }, 200);
  }
  private async act(action: () => Promise<void>): Promise<void> {
    if (this.busy) return;
    const epoch = this.epoch; this.busy = true; this.error = ''; this.notice = '';
    try { await action(); if (epoch === this.epoch) { this.ready = true; this.refresh(); } }
    catch (error) { if (epoch === this.epoch) { this.error = (error as Error).message; this.refresh(); } }
    finally { if (epoch === this.epoch) { this.busy = false; this.progress = ''; } }
  }
  private inspect(raw: string): void {
    this.invitation = parseReadingInvite(raw, new ReadingCrypto()); this.invitationOrigin = this.invitation.origin; this.pairingText = '';
  }
  private async scan(): Promise<void> {
    const epoch = this.epoch;
    this.scanning = true;
    try {
      const result = await scanBarcode.startScanForResult(this.getUIContext().getHostContext() as common.UIAbilityContext,
        { scanTypes: [scanCore.ScanType.QR_CODE], enableMultiMode: false, enableAlbum: true });
      if (epoch === this.epoch) this.inspect(result.originalValue);
    } finally { this.scanning = false; }
  }
  private onProgress(value: ReadingProgress): void { this.progress = `${value.message} ${value.completed} / ${value.total}`; }
  private async sync(): Promise<void> {
    if (!this.runtime?.library) throw new Error('请先完成阅读配对');
    const epoch = this.epoch;
    await this.runtime.library.sync((value: ReadingProgress) => { if (epoch === this.epoch) this.onProgress(value); });
    if (epoch === this.epoch) { this.current = null; this.notice = '正文同步完成，已可离线搜索和阅读；图片需要单独保存。'; }
  }
  private async saveImages(): Promise<void> {
    if (!this.runtime?.library || !this.current) return;
    const epoch = this.epoch;
    const result = await this.runtime.library.saveImages(this.current.id, (value: ReadingProgress) => { if (epoch === this.epoch) this.onProgress(value); });
    if (epoch !== this.epoch) return;
    this.imageRevision++;
    if (result.failed.length) throw new Error(`已保存 ${result.saved} 张，${result.failed.length} 张失败：\n${result.failed.join('\n')}`);
    this.notice = `已保存 ${result.saved} 张本地图片。远程资源不在离线范围内。`;
  }
  private async forget(): Promise<void> {
    const result = await this.getUIContext().getPromptAction().showDialog({ title: '清除本机阅读连接？',
      message: '本机阅读正文和图片缓存将删除；手机收集草稿不受影响。云端设备权限仍需在电脑撤销。',
      buttons: [{ text: '取消', color: $r('app.color.text_secondary') }, { text: '清除', color: $r('app.color.error') }] });
    if (result.index !== 1 || !this.runtime) return;
    await this.runtime.forget(); this.current = null; this.notice = '本机阅读连接已清除';
  }
  // @test-boundary 阅读控制与渲染分离；下方由 SDK 编译与真机验证。
  @Builder
  private Pairing() {
    Text('扫描电脑“手机阅读同步”的配对码。阅读授权与手机收集独立，默认不会上传或下载资料。').fontSize(14).fontColor($r('app.color.text_secondary'))
    PrimaryAction({ label: '扫描阅读配对码', active: !this.busy, settings: this.settings, onAction: () => { this.act(() => this.scan()); } })
    TextInput({ text: this.pairingText, placeholder: '也可粘贴完整阅读配对链接' }).maxLength(8192).onChange((value: string) => {
      this.pairingText = value; this.invitation = null; this.invitationOrigin = '';
    }).enabled(!this.busy)
    SecondaryAction({ label: '解析链接', active: !this.busy && this.pairingText.length > 0, settings: this.settings,
      onAction: () => { this.act(async () => { this.inspect(this.pairingText); }); } })
    if (this.invitationOrigin) {
      Text(`确认连接：${this.invitationOrigin}`).fontSize(14).fontColor($r('app.color.text_primary'))
      PrimaryAction({ label: '确认并请求电脑批准', active: !this.busy, settings: this.settings, onAction: () => { this.act(async () => {
        if (this.runtime && this.invitation) { await this.runtime.pair(this.invitation); this.invitation = null; this.invitationOrigin = ''; this.notice = '请求已提交，请在电脑确认这台手机'; }
      }); } })
    }
  }
  build() {
    Scroll() {
      Column({ space: 16 }) {
        Text('知识库 · 阅读试用').fontSize(28).fontWeight(FontWeight.Bold).fontColor($r('app.color.text_primary')).width('100%')
        if (this.busy) {
          Row({ space: 8 }) { LoadingProgress().width(20).height(20).color(this.palette.accent); Text(this.progress || '正在处理…').fontColor(this.palette.accent) }.width('100%')
        }
        if (this.error) {
          Text(this.error).fontSize(14).fontColor($r('app.color.error')).width('100%').copyOption(CopyOptions.LocalDevice)
          SecondaryAction({ label: '重试', active: !this.busy, settings: this.settings, onAction: () => {
            if (this.paired && this.runtime?.library) this.act(() => this.sync()); else this.initialize();
          } })
        }
        if (this.notice) { Text(this.notice).fontSize(14).fontColor(this.palette.accent).width('100%').copyOption(CopyOptions.LocalDevice) }
        if (!this.ready && !this.paired) {
          if (!this.busy && !this.error) { SecondaryAction({ label: '读取本机阅读库', settings: this.settings, onAction: () => { this.initialize(); } }) }
        } else if (!this.paired) {
          if (this.pending) {
            Text(`等待电脑确认 · ${this.origin}`).fontColor($r('app.color.text_primary'))
            PrimaryAction({ label: '检查电脑确认', active: !this.busy, settings: this.settings, onAction: () => { this.act(async () => {
              if (this.runtime) this.notice = await this.runtime.retryPair() ? '阅读配对成功，请下载正文' : '尚未确认，请在电脑点击确认这台手机';
            }); } })
            SecondaryAction({ label: '清除待确认连接', active: !this.busy, settings: this.settings, onAction: () => { this.act(() => this.forget()); } })
          } else { this.Pairing() }
        } else if (this.current) {
          ReadingArticleView({ article: this.current, library: this.runtime?.library || null, busy: this.busy, imageRevision: this.imageRevision,
            settings: this.settings, onBack: () => { this.current = null; }, onSaveImages: () => { this.act(() => this.saveImages()); } })
        } else {
          Text(this.syncedAt ? `版本 ${this.sequence} · 最近同步 ${new Date(this.syncedAt).toLocaleString()}` : '尚未下载正文')
            .fontSize(13).fontColor($r('app.color.text_secondary')).width('100%')
          PrimaryAction({ label: '同步正文', active: !this.busy, settings: this.settings, onAction: () => { this.act(() => this.sync()); } })
          TextInput({ text: this.query, placeholder: '搜索已下载的标题、正文和标签' }).maxLength(200).onChange((value: string) => this.search(value))
          Scroll() { Row({ space: 8 }) {
            SecondaryAction({ label: '全部知识库', settings: this.settings, onAction: () => { this.collection = ''; this.filter(); } })
            ForEach(this.collections, (name: string) => { SecondaryAction({ label: (name === this.collection ? '✓ ' : '') + name, settings: this.settings,
              onAction: () => { this.collection = name; this.filter(); } }) }, (name: string): string => name)
          } }.scrollable(ScrollDirection.Horizontal).width('100%')
          if (this.tags.length > 0) {
            TextInput({ text: this.tag, placeholder: '按完整标签筛选（留空为全部）' }).maxLength(1024).onChange((value: string) => { this.tag = value; this.filter(); })
          }
          Text(`已下载 ${this.articles.length} 篇 · 当前结果 ${this.results.length} 篇；搜索仅覆盖已下载内容`).fontSize(12).fontColor($r('app.color.text_secondary')).width('100%')
          if (this.results.length === 0 && !this.error) {
            Text(this.sequence === 0 ? '请先同步正文。电脑关闭后，也可下载此前已上传的内容。' : this.articles.length === 0 ? '当前同步范围内没有文章，请在电脑选择知识库并上传。' : '没有匹配结果，请调整搜索或筛选。')
              .fontColor($r('app.color.text_secondary')).width('100%')
          }
          ForEach(this.results.slice(0, this.shown), (article: ReadingArticle) => {
            Column({ space: 6 }) {
              Text(article.title || '无标题').fontSize(17).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary')).width('100%')
              Text(article.summary || article.content.slice(0, 160)).fontSize(13).fontColor($r('app.color.text_secondary')).maxLines(3).width('100%')
              Text(article.collectionName).fontSize(12).fontColor(this.palette.accent).width('100%')
            }.padding(16).borderRadius(16).backgroundColor($r('app.color.surface')).width('100%').onClick(() => { this.current = article; })
          }, (article: ReadingArticle): string => article.id)
          if (this.shown < this.results.length) { SecondaryAction({ label: '显示更多', settings: this.settings, onAction: () => { this.shown += 30; } }) }
          Row({ space: 8 }) {
            SecondaryAction({ label: '清理图片缓存', active: !this.busy, settings: this.settings, onAction: () => { this.act(async () => {
              await this.runtime?.library?.clearImages(); this.imageRevision++; this.notice = '图片缓存已清理，正文仍可离线阅读';
            }); } })
            SecondaryAction({ label: '清除阅读连接', active: !this.busy, settings: this.settings, onAction: () => { this.act(() => this.forget()); } })
          }.width('100%')
        }
      }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH }).alignItems(HorizontalAlign.Start).padding({ left: 20, right: 20, top: 20, bottom: CONTENT_END_OFFSET })
    }.width('100%').height('100%').align(Alignment.Top).scrollBar(BarState.Auto)
  }
}
