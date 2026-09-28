import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { pressFeedback } from '../services/InterfaceMotion';

@Component
export struct PageHeader {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop tab: number = 0;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop leftHand: boolean = false;
  @Prop loading: boolean = false;
  @Prop interactive: boolean = true;
  onAction: () => void = () => {};

  @Builder
  private Action() {
    Button({ type: ButtonType.Circle }) {
      if (this.tab === 0) {
        Image($r('app.media.ic_plus')).width(22).height(22).fillColor(this.palette.accent)
      } else {
        SymbolGlyph($r('sys.symbol.arrow_clockwise')).fontSize(22).fontColor([this.palette.accent])
      }
    }.width(44).height(44).backgroundColor($r('app.color.surface')).enabled(this.interactive)
      .clickEffect(pressFeedback(this.settings)).accessibilityText(this.tab === 0 ? '新建收集' : '刷新近期记录')
      .onClick(this.onAction)
  }

  build() {
    Row({ space: 12 }) {
      if (this.tab === 0 && this.leftHand) { this.Action() }
      Text(['收集箱', '近期', '连接电脑'][this.tab]).fontSize(30).fontWeight(FontWeight.Bold)
        .fontColor($r('app.color.text_primary')).layoutWeight(1).maxLines(1)
        .minFontSize(22).maxFontSize(30).heightAdaptivePolicy(TextHeightAdaptivePolicy.MIN_FONT_SIZE_FIRST)
      if (this.loading) { LoadingProgress().width(20).height(20).color(this.palette.accent) }
      if (this.tab === 1 || (this.tab === 0 && !this.leftHand)) { this.Action() }
    }.width('100%').constraintSize({ minHeight: 76 }).padding({ left: 24, right: 24, top: 12, bottom: 16 })
  }
}
