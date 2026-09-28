import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { animateInterface, pressFeedback } from '../services/InterfaceMotion';
import { CaptureMode } from '../core/Protocol';

@Component
export struct CaptureEditor {
  @Consume('capturePalette') palette: ThemePalette;
  @Link input: string;
  @Link mode: CaptureMode;
  @Prop busy: boolean = false;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop saving: boolean = false;
  @Prop error: string = '';
  onEdit: () => void = () => {};
  onSave: () => void = () => {};
  onClose: () => void = () => {};

  private chooseMode(mode: CaptureMode): void {
    if (this.busy || this.mode === mode) return;
    animateInterface(this.getUIContext(), this.settings, () => { this.mode = mode; });
    this.onEdit();
  }

  @Builder
  private ModeOption(value: CaptureMode, label: string) {
    Button(label, { type: ButtonType.Normal }).layoutWeight(1).height(44).fontSize(14).borderRadius(14)
      .fontColor(this.mode === value ? this.palette.accent : $r('app.color.text_secondary'))
      .backgroundColor(this.mode === value ? $r('app.color.surface') : Color.Transparent)
      .fontWeight(this.mode === value ? FontWeight.Medium : FontWeight.Regular)
      .enabled(!this.busy).clickEffect(pressFeedback(this.settings)).accessibilitySelected(this.mode === value)
      .accessibilityText(`${label}${this.mode === value ? '，已选中' : ''}`).onClick(() => this.chooseMode(value))
  }

  build() {
    Column({ space: 16 }) {
      Row({ space: 12 }) {
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.chevron_down')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).backgroundColor($r('app.color.surface_muted'))
          .enabled(!this.busy).clickEffect(pressFeedback(this.settings)).accessibilityText('收起，保留未完成输入').onClick(this.onClose)
        Text('记录内容').fontSize(20).fontWeight(FontWeight.Bold).fontColor($r('app.color.text_primary')).layoutWeight(1)
        Button(this.busy ? '保存中' : '保存').height(44).fontSize(15).padding({ left: 18, right: 18 })
          .backgroundColor(this.input.trim() ? this.palette.accent : $r('app.color.surface_muted'))
          .fontColor(this.input.trim() ? this.palette.onAccent : $r('app.color.text_tertiary'))
          .enabled(!this.busy && this.input.trim().length > 0).clickEffect(pressFeedback(this.settings)).onClick(this.onSave)
      }.width('100%')
      Row({ space: 4 }) {
        this.ModeOption('auto', '自动识别')
        this.ModeOption('urls', '链接')
        this.ModeOption('text', '文字')
      }.width('100%').padding(4).backgroundColor($r('app.color.surface_muted')).borderRadius(18)
      TextArea({ text: this.input, placeholder: '一个链接，或一个值得留下的想法…' })
        .layoutWeight(1).width('100%').fontSize(18).lineHeight(29).padding(8)
        .backgroundColor(Color.Transparent).fontColor($r('app.color.text_primary'))
        .caretColor(this.palette.accent).selectedBackgroundColor(this.palette.soft)
        .placeholderColor($r('app.color.text_tertiary')).borderRadius(0).maxLength(32768)
        .enabled(!this.busy).accessibilityText('收集内容').onChange((value: string) => { this.input = value; this.onEdit(); })
      if (this.error) {
        Text(this.error).fontSize(13).lineHeight(20).fontColor($r('app.color.error')).width('100%').copyOption(CopyOptions.LocalDevice)
      }
      Row({ space: 8 }) {
        if (this.saving) { LoadingProgress().width(16).height(16).color(this.palette.accent) }
        Text(this.saving ? '正在保留输入…' : '收起可继续 · 保存后再确认发送')
          .fontSize(12).fontColor($r('app.color.text_secondary')).layoutWeight(1)
        if (this.settings.showCount) { Text(`${this.input.length} 字`).fontSize(12).fontColor($r('app.color.text_tertiary')) }
      }.width('100%')
    }.padding({ left: 20, right: 20, top: 8, bottom: 24 }).width('100%').height('100%')
  }
}
