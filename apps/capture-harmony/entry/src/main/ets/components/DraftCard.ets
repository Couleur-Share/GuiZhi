import { ThemePalette } from '../services/ThemePalette';
import { animateInterface, revealTransition, pressFeedback } from '../services/InterfaceMotion';
import { AppPreferences } from '../services/AppPreferences';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { Draft } from '../core/Protocol';
import { relativeTime } from '../core/RelativeTime';
import { sendingLabel, sendingSlowHint } from '../core/SendingState';

@Component
export struct DraftCard {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop @Watch('measureClamped') draft: Draft = new Draft();
  @Prop busy: boolean = false;
  // 这一条正在网络发送：只锁定本卡片的发送与菜单，页面其他操作不受影响。
  @Prop sending: boolean = false;
  @Prop sendingSeconds: number = 0;
  @Prop @Watch('measureClamped') settings: AppPreferences = new AppPreferences();
  @Prop index: number = 0;
  @Prop connected: boolean = false;
  @State expanded: boolean = false;
  // 折叠时文字是否被截断，决定是否显示“展开内容”；尚未测量或测量失败时保守显示按钮，避免长文无法展开。
  @State clamped: boolean = true;
  private textWidth: number = 0;
  onSend: () => void = () => {};
  onEdit: () => void = () => {};
  onRemove: () => void = () => {};
  onConnect: () => void = () => {};

  private source(): string {
    const match = /https?:\/\/([^\s/]+)/i.exec(this.draft.input);
    return match ? match[1] : '文字笔记';
  }

  // 用同一套测量接口比较“不限行数”与“限制预览行数”的高度，只有前者更高才说明文字被截断；
  // 两次测量单位一致，不依赖 px 与 vp 的换算。
  private measureClamped(): void {
    if (this.textWidth <= 0) return;
    try {
      const measure = this.getUIContext().getMeasureUtils();
      const full = measure.measureTextSize({
        textContent: this.draft.input, fontSize: 16, lineHeight: 25, constraintWidth: this.textWidth });
      const limited = measure.measureTextSize({
        textContent: this.draft.input, fontSize: 16, lineHeight: 25, constraintWidth: this.textWidth,
        maxLines: this.settings.previewLines });
      this.clamped = (full.height as number) > (limited.height as number) + 1;
    } catch (error) {
      this.clamped = true;
    }
  }

  private onTextArea(area: Area): void {
    const width = area.width as number;
    if (Math.abs(width - this.textWidth) < 0.5) return;
    this.textWidth = width;
    this.measureClamped();
  }

  private statusText(): string {
    if (this.sending) return '正在发送';
    if (this.draft.attempted) return '待确认结果';
    return this.draft.lastError ? '发送失败，可修改后重试' : '尚未发送';
  }

  private statusColor(): ResourceColor {
    if (this.sending) return this.palette.accent;
    if (this.draft.attempted) return $r('app.color.warning');
    return this.draft.lastError ? $r('app.color.error') : $r('app.color.text_secondary');
  }

  build() {
    Column({ space: 12 }) {
      Row({ space: 10 }) {
        Image(this.draft.mode === 'text' || this.source() === '文字笔记' ? $r('app.media.ic_compose') : $r('app.media.ic_link'))
          .fillColor(this.palette.accent).padding(10).width(40).height(40).backgroundColor(this.palette.soft).borderRadius(14)
        Text(this.source()).fontSize(13).fontColor($r('app.color.text_secondary')).layoutWeight(1)
          .maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.chevron_down')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).clickEffect(pressFeedback(this.settings)).backgroundColor(Color.Transparent)
          .fontColor($r('app.color.text_secondary')).enabled(!this.busy && !this.sending).accessibilityText('草稿操作')
          .bindMenu(this.draft.attempted ? [
            { value: '删除本机草稿', action: this.onRemove }
          ] : [
            { value: '编辑内容', action: this.onEdit },
            { value: '删除本机草稿', action: this.onRemove }
          ], { systemMaterial: panelMaterial(this.settings) })
      }.width('100%')
      if (this.expanded) {
        Text(this.draft.input).fontSize(16).lineHeight(25).fontColor($r('app.color.text_primary'))
          .width('100%').copyOption(CopyOptions.LocalDevice)
          .onAreaChange((_previous: Area, area: Area) => { this.onTextArea(area); })
          .transition(revealTransition(this.settings))
      } else {
        Text(this.draft.input).fontSize(16).lineHeight(25).fontColor($r('app.color.text_primary'))
          .width('100%').maxLines(this.settings.previewLines).textOverflow({ overflow: TextOverflow.Ellipsis })
          .copyOption(CopyOptions.LocalDevice)
          .onAreaChange((_previous: Area, area: Area) => { this.onTextArea(area); })
          .transition(revealTransition(this.settings))
      }
      if (this.clamped || this.expanded) {
        Button(this.expanded ? '收起内容' : '展开内容').height(44).clickEffect(pressFeedback(this.settings)).fontSize(12)
          .backgroundColor(Color.Transparent).fontColor($r('app.color.text_secondary'))
          .onClick(() => { animateInterface(this.getUIContext(), this.settings, () => { this.expanded = !this.expanded; }); })
      }
      Divider().color($r('app.color.border_subtle'))
      Row({ space: 12 }) {
        Column({ space: 4 }) {
          Text(this.statusText()).fontSize(12).fontColor(this.statusColor())
          if (this.sending) {
            if (sendingSlowHint(this.sendingSeconds)) {
              Text(sendingSlowHint(this.sendingSeconds)).fontSize(12).lineHeight(18).fontColor($r('app.color.text_secondary'))
            }
          } else if (this.draft.lastError) {
            Text(`上次发送失败：${this.draft.lastError}`).fontSize(12).lineHeight(18)
              .fontColor($r('app.color.error')).copyOption(CopyOptions.LocalDevice)
          } else if (this.draft.attempted) {
            Text('已发出但未收到回执，重试不会重复保存').fontSize(12).lineHeight(18).fontColor($r('app.color.text_secondary'))
          }
          Text(relativeTime(this.draft.createdAt, Date.now())).fontSize(11).fontColor($r('app.color.text_tertiary'))
        }.alignItems(HorizontalAlign.Start).layoutWeight(1)
        Button(this.sending ? sendingLabel(this.sendingSeconds) : this.busy ? '处理中' : !this.connected ? '连接电脑' : this.draft.attempted ? '重试发送' : '发送')
          .height(44).clickEffect(pressFeedback(this.settings)).fontSize(14).padding({ left: 18, right: 18 })
          .backgroundColor(this.palette.soft).fontColor(this.palette.accent)
          .enabled(!this.busy && !this.sending).onClick(() => { if (this.connected) this.onSend(); else this.onConnect(); })
      }.width('100%')
    }.width('100%').padding({ left: 18, right: 18, top: 12, bottom: 16 })
      .alignItems(HorizontalAlign.Start).backgroundColor($r('app.color.surface')).borderRadius(20)
      .transition(revealTransition(this.settings, this.index))
  }
}
