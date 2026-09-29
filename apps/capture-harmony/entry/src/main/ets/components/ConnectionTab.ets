import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { revealTransition } from '../services/InterfaceMotion';
import { CONTENT_MAX_WIDTH } from '../services/Layout';
import { DesktopStatus } from '../core/DesktopStatus';
import { PrimaryAction, SecondaryAction } from './Actions';
import { DesktopStatusLine } from './DesktopStatusLine';
import { PairingStep } from './PairingStep';

// 连接页：已配对时是状态卡（检查连接、管理连接），未配对时是三步说明、扫码与备用链接配对。
// 状态与动作都由 Index 持有；输入框的内容通过回调回写，页面锁（busy）由调用方统一控制。
@Component
export struct ConnectionTab {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop connected: boolean = false;
  @Prop paired: boolean = false;
  @Prop origin: string = '';
  @Prop desktop: DesktopStatus = new DesktopStatus();
  @Prop statusError: string = '';
  // 页面级错误或连接检查错误：任何一个存在都表示“连接需要检查”。
  @Prop hasProblem: boolean = false;
  @Prop automaticCheck: boolean = true;
  @Prop refreshing: boolean = false;
  @Prop busy: boolean = false;
  @Prop operation: string = '';
  @Prop showPairingLink: boolean = false;
  @Prop pairingText: string = '';
  @Prop pairingOrigin: string = '';
  @Prop deviceName: string = '';
  @Prop keyboardVisible: boolean = false;
  @Prop settings: AppPreferences = new AppPreferences();
  onRefresh: () => void = () => {};
  onRetryPair: () => void = () => {};
  onDisconnect: (localOnly: boolean) => void = (localOnly: boolean): void => {};
  onScan: () => void = () => {};
  onTogglePairingLink: () => void = () => {};
  onPairingText: (value: string) => void = (value: string): void => {};
  onParseLink: () => void = () => {};
  onDeviceName: (value: string) => void = (value: string): void => {};
  onPair: () => void = () => {};

  private title(): string {
    if (this.paired) return '已配对归知';
    return this.hasProblem ? '连接需要检查' : '等待电脑确认';
  }

  private guidance(): string {
    if (this.paired) return '发送后，电脑上线即可接收。';
    if (this.statusError) {
      return this.automaticCheck ? '网络恢复后会自动继续检查，也可以手动重试。' : '请按上方原因处理，再点击重试。';
    }
    return '打开电脑「设置 → 手机收集」确认绑定。此页每 2 秒自动检查，无需重复提交；二维码过期请在电脑重新生成。';
  }

  @Builder
  private ConnectedCard() {
    Column({ space: 14 }) {
      SymbolGlyph($r('sys.symbol.computer')).fontSize(44).fontColor([this.palette.accent]).accessibilityLevel('no')
      Text(this.title()).fontSize(21).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
      Text(this.origin).fontSize(13).fontColor($r('app.color.text_secondary')).copyOption(CopyOptions.LocalDevice)
      if (this.paired && this.desktop.visible) {
        DesktopStatusLine({ desktop: this.desktop, center: true })
        if (this.desktop.detail !== '') {
          Text(this.desktop.detail).fontSize(13).lineHeight(21).fontColor($r('app.color.text_secondary'))
            .width('100%').textAlign(TextAlign.Center)
        }
      } else {
        Text(this.guidance()).fontSize(13).fontColor($r('app.color.text_secondary'))
      }
      PrimaryAction({ label: this.refreshing ? '正在检查' : '检查连接', icon: $r('sys.symbol.link'),
        active: !this.busy && !this.refreshing, settings: this.settings, onAction: this.onRefresh })
      if (!this.paired) {
        SecondaryAction({ label: this.operation || '重试配对请求', active: !this.busy && !this.refreshing,
          settings: this.settings, onAction: this.onRetryPair })
      }
    }.padding(22).width('100%').backgroundColor($r('app.color.surface')).borderRadius(20)
  }

  @Builder
  private PairingLinkForm() {
    Column({ space: 14 }) {
      Text('粘贴电脑生成的完整链接').fontSize(14).fontColor($r('app.color.text_primary')).width('100%')
      TextInput({ placeholder: 'https://…#pair=…', text: this.pairingText }).height(48).fontSize(14).borderRadius(12)
        .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
        .placeholderColor($r('app.color.text_tertiary')).enabled(!this.busy)
        .onChange((value: string) => { this.onPairingText(value); })
      SecondaryAction({ label: '解析链接', active: !this.busy && this.pairingText.length > 0,
        settings: this.settings, onAction: this.onParseLink })
      if (this.pairingOrigin) {
        Divider().color($r('app.color.border_subtle'))
        Text(`确认服务地址\n${this.pairingOrigin}`).fontSize(14).lineHeight(22)
          .fontColor($r('app.color.text_primary')).copyOption(CopyOptions.LocalDevice).width('100%')
        TextInput({ placeholder: '设备名称', text: this.deviceName }).height(48).fontSize(14).borderRadius(12)
          .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary')).maxLength(60)
          .enabled(!this.busy).onChange((value: string) => { this.onDeviceName(value); })
        PrimaryAction({ label: this.operation || '确认配对', icon: $r('sys.symbol.link'), active: !this.busy,
          settings: this.settings, onAction: this.onPair })
      }
    }.padding(18).width('100%').backgroundColor($r('app.color.surface')).borderRadius(18)
      .transition(revealTransition(this.settings))
  }

  build() {
    Scroll() {
      Column({ space: 20 }) {
        if (this.connected) {
          this.ConnectedCard()
          Button('管理连接  ▾').constraintSize({ minHeight: 44 }).fontSize(14).backgroundColor(Color.Transparent)
            .fontColor($r('app.color.text_secondary')).enabled(!this.busy)
            .bindMenu([
              { value: '解除与电脑的配对', action: () => { this.onDisconnect(false); } },
              { value: '无法联网？仅清除本机连接', action: () => { this.onDisconnect(true); } }
            ], { systemMaterial: panelMaterial(this.settings) })
        } else {
          Text('让手机里的灵感，回到你的知识库。').fontSize(14).fontColor($r('app.color.text_secondary')).width('100%')
          Column({ space: 24 }) {
            PairingStep({ step: '1', title: '在电脑生成二维码', detail: '打开归知 → 设置 → 手机收集' })
            PairingStep({ step: '2', title: '用手机扫码', detail: '扫描电脑上显示的配对二维码' })
            PairingStep({ step: '3', title: '回到电脑确认', detail: '批准设备后，即可手动发送收集内容' })
          }.width('100%').padding(22).backgroundColor($r('app.color.surface')).borderRadius(20)
          PrimaryAction({ label: '扫描二维码', icon: $r('sys.symbol.qrcode'), active: !this.busy,
            settings: this.settings, onAction: this.onScan })
          Button(this.showPairingLink ? '收起链接配对' : '使用配对链接  ›').constraintSize({ minHeight: 44 }).fontSize(14)
            .backgroundColor(Color.Transparent).fontColor($r('app.color.text_secondary')).enabled(!this.busy)
            .onClick(this.onTogglePairingLink)
          if (this.showPairingLink) {
            this.PairingLinkForm()
          }
        }
        Text('收集内容始终由你确认发送。配对与传输使用 HTTPS，设备凭证保存在系统安全存储中。')
          .fontSize(12).lineHeight(21).fontColor($r('app.color.text_tertiary')).width('100%')
      }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
        .padding({ left: 20, right: 20, top: 4, bottom: this.keyboardVisible ? 24 : 112 })
    }.height('100%').width('100%').align(Alignment.Top).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
  }
}
