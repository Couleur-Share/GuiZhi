import { pasteboard } from '@kit.BasicServicesKit';
import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { animateInterface, pressFeedback } from '../services/InterfaceMotion';
import { CaptureMode } from '../core/Protocol';
import { CaptureHint, HintTone, captureHint } from '../core/CaptureHint';
import { parseCapture } from '../core/CaptureParser';
import { EDITOR_MAX_CHARS, pasteInto, PasteOutcome } from '../core/EditorPaste';

const INPUT_ID: string = 'capture-editor-input';
// 半模态面板过渡期间焦点可能被容器抢走：defaultFocus 之外，过渡结束后再请求一次。
const FOCUS_DELAY_MS: number = 250;

@Component
export struct CaptureEditor {
  @Consume('capturePalette') palette: ThemePalette;
  @Link @Watch('refreshHint') input: string;
  @Link @Watch('refreshHint') mode: CaptureMode;
  @Prop busy: boolean = false;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop saving: boolean = false;
  @Prop error: string = '';
  // 已连接且已配对：主按钮为“发送”、次按钮为“存草稿”；否则只有“保存”。
  @Prop canSend: boolean = false;
  // 识别提示：由当前输入与模式推导（见 core/CaptureHint）。
  @State hint: CaptureHint = new CaptureHint();
  // 粘贴未成功的原因；输入内容再次变化后清除。
  @State pasteMessage: string = '';
  private focusTimer: number = -1;
  onEdit: () => void = () => {};
  onSave: () => void = () => {};
  onSend: () => void = () => {};
  onSplit: () => void = () => {};
  onClose: () => void = () => {};

  aboutToAppear(): void {
    this.refreshHint();
    this.focusTimer = setTimeout(() => { focusControl.requestFocus(INPUT_ID); }, FOCUS_DELAY_MS);
  }

  aboutToDisappear(): void {
    if (this.focusTimer !== -1) { clearTimeout(this.focusTimer); this.focusTimer = -1; }
  }

  private refreshHint(): void {
    this.hint = captureHint(parseCapture(this.input, this.mode), this.mode);
  }

  private chooseMode(mode: CaptureMode): void {
    if (this.busy || this.mode === mode) return;
    animateInterface(this.getUIContext(), this.settings, () => { this.mode = mode; });
    this.onEdit();
  }

  // 系统安全控件“粘贴”：点击即获得一次性读取授权，无需申请剪贴板权限，也不弹出“已读取剪贴板”提示。
  private async pasteFromClipboard(result: PasteButtonOnClickResult): Promise<void> {
    if (this.busy) return;
    if (result !== PasteButtonOnClickResult.SUCCESS) {
      this.pasteMessage = '没有获得读取剪贴板的授权，请重试，或长按输入框选择粘贴';
      return;
    }
    try {
      const data = await pasteboard.getSystemPasteboard().getData();
      const outcome: PasteOutcome = pasteInto(this.input, data.getPrimaryText() ?? '');
      this.pasteMessage = outcome.message;
      if (outcome.changed) { this.input = outcome.text; this.onEdit(); }
    } catch (error) {
      this.pasteMessage = `读取剪贴板失败：${(error as Error).message || '未知原因'}。可长按输入框选择粘贴`;
    }
  }

  private hintMessage(): string { return this.pasteMessage ? this.pasteMessage : this.hint.text; }

  private hintTone(): HintTone { return this.pasteMessage ? 'warning' : this.hint.tone; }

  private hintColor(): Resource {
    const tone = this.hintTone();
    if (tone === 'error') return $r('app.color.error');
    if (tone === 'warning') return $r('app.color.warning');
    return $r('app.color.text_secondary');
  }

  private hintIcon(): Resource {
    const tone = this.hintTone();
    if (tone === 'error') return $r('sys.symbol.xmark_circle');
    if (tone === 'warning') return $r('sys.symbol.exclamationmark_circle');
    return $r('sys.symbol.info_circle');
  }

  private sendable(): boolean { return !this.busy && this.hint.canSend; }

  @Builder
  private ModeOption(value: CaptureMode, label: string) {
    Button(label, { type: ButtonType.Normal }).layoutWeight(1).height(44).fontSize(14).borderRadius(14)
      .fontColor(this.mode === value ? this.palette.accent : $r('app.color.text_secondary'))
      .backgroundColor(this.mode === value ? $r('app.color.surface') : Color.Transparent)
      .fontWeight(this.mode === value ? FontWeight.Medium : FontWeight.Regular)
      .enabled(!this.busy).clickEffect(pressFeedback(this.settings)).accessibilitySelected(this.mode === value)
      .accessibilityText(`${label}${this.mode === value ? '，已选中' : ''}`).onClick(() => this.chooseMode(value))
  }

  // 识别提示下方的显式选项（混合内容二选一、超过 20 个链接时拆分）。
  @Builder
  private HintAction(label: string, action: () => void) {
    Button(label, { type: ButtonType.Normal }).layoutWeight(1).height(40).fontSize(14).borderRadius(12)
      .backgroundColor(this.palette.soft).fontColor(this.palette.accent)
      .enabled(!this.busy).clickEffect(pressFeedback(this.settings)).onClick(action)
  }

  @Builder
  private HintBlock() {
    Column({ space: 8 }) {
      Row({ space: 8 }) {
        SymbolGlyph(this.hintIcon()).fontSize(16).fontColor([this.hintColor()]).accessibilityLevel('no')
        Text(this.hintMessage()).fontSize(13).lineHeight(20).fontColor(this.hintColor()).layoutWeight(1)
      }.width('100%').alignItems(VerticalAlign.Top)
      if (this.hint.offerModes) {
        Row({ space: 8 }) {
          this.HintAction('按链接采集', () => this.chooseMode('urls'))
          this.HintAction('存为文字', () => this.chooseMode('text'))
        }.width('100%')
      }
      if (this.hint.splitParts > 0) {
        Row({ space: 8 }) {
          this.HintAction(`按 20 条拆分保存（${this.hint.splitParts} 条草稿）`, this.onSplit)
        }.width('100%')
      }
    }.width('100%')
  }

  @Builder
  private Actions() {
    if (this.canSend) {
      Row({ space: 12 }) {
        Button('存草稿', { type: ButtonType.Normal }).layoutWeight(2).height(48).fontSize(16).borderRadius(16)
          .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
          .enabled(!this.busy && this.hint.canSave).clickEffect(pressFeedback(this.settings)).onClick(this.onSave)
        Button({ type: ButtonType.Normal }) {
          Row({ space: 8 }) {
            SymbolGlyph($r('sys.symbol.paperplane')).fontSize(19)
              .fontColor([this.sendable() ? this.palette.onAccent : $r('app.color.text_tertiary')])
            Text(this.busy ? '处理中' : '发送').fontSize(16).fontWeight(FontWeight.Medium)
              .fontColor(this.sendable() ? this.palette.onAccent : $r('app.color.text_tertiary'))
          }.justifyContent(FlexAlign.Center)
        }.layoutWeight(3).height(48).borderRadius(16)
          .backgroundColor(this.sendable() ? this.palette.accent : $r('app.color.surface_muted'))
          .enabled(this.sendable()).clickEffect(pressFeedback(this.settings)).onClick(this.onSend)
      }.width('100%')
    } else {
      Button(this.busy ? '保存中' : '保存', { type: ButtonType.Normal }).width('100%').height(48).fontSize(16).borderRadius(16)
        .backgroundColor(this.hint.canSave ? this.palette.accent : $r('app.color.surface_muted'))
        .fontColor(this.hint.canSave ? this.palette.onAccent : $r('app.color.text_tertiary'))
        .enabled(!this.busy && this.hint.canSave).clickEffect(pressFeedback(this.settings)).onClick(this.onSave)
    }
  }

  build() {
    Column({ space: 12 }) {
      Row({ space: 12 }) {
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.chevron_down')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).backgroundColor($r('app.color.surface_muted'))
          .enabled(!this.busy).clickEffect(pressFeedback(this.settings)).accessibilityText('收起，保留未完成输入').onClick(this.onClose)
        Text('记录内容').fontSize(20).fontWeight(FontWeight.Bold).fontColor($r('app.color.text_primary')).layoutWeight(1)
        PasteButton({ icon: PasteIconStyle.LINES, text: PasteDescription.PASTE, buttonType: ButtonType.Capsule })
          .height(44).padding({ left: 16, right: 16 }).fontSize(14)
          .backgroundColor(this.palette.soft).fontColor(this.palette.accent).iconColor(this.palette.accent)
          .onClick((event: ClickEvent, result: PasteButtonOnClickResult) => { this.pasteFromClipboard(result); })
      }.width('100%')
      Row({ space: 4 }) {
        this.ModeOption('auto', '自动识别')
        this.ModeOption('urls', '链接')
        this.ModeOption('text', '文字')
      }.width('100%').padding(4).backgroundColor($r('app.color.surface_muted')).borderRadius(18)
      TextArea({ text: this.input, placeholder: '一个链接，或一个值得留下的想法…' })
        .id(INPUT_ID).defaultFocus(true)
        .layoutWeight(1).width('100%').fontSize(18).lineHeight(29).padding(8)
        .backgroundColor(Color.Transparent).fontColor($r('app.color.text_primary'))
        .caretColor(this.palette.accent).selectedBackgroundColor(this.palette.soft)
        .placeholderColor($r('app.color.text_tertiary')).borderRadius(0).maxLength(EDITOR_MAX_CHARS)
        .enabled(!this.busy).accessibilityText('收集内容')
        .onChange((value: string) => { this.pasteMessage = ''; this.input = value; this.onEdit(); })
      if (this.error) {
        Text(this.error).fontSize(13).lineHeight(20).fontColor($r('app.color.error')).width('100%').copyOption(CopyOptions.LocalDevice)
      }
      if (this.hintMessage()) {
        this.HintBlock()
      }
      Row({ space: 8 }) {
        if (this.saving) { LoadingProgress().width(16).height(16).color(this.palette.accent) }
        Text(this.saving ? '正在保留输入…' : (this.canSend ? '收起可继续 · 存为草稿或直接发送' : '收起可继续 · 保存后再确认发送'))
          .fontSize(12).fontColor($r('app.color.text_secondary')).layoutWeight(1)
        if (this.settings.showCount) { Text(`${this.input.length} 字`).fontSize(12).fontColor($r('app.color.text_tertiary')) }
      }.width('100%')
      this.Actions()
    }.padding({ left: 20, right: 20, top: 8, bottom: 24 }).width('100%').height('100%')
  }
}
