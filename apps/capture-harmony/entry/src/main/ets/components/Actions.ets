import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { pressFeedback } from '../services/InterfaceMotion';

// 主操作按钮：整宽、强调色；不可用时退成灰底灰字。高度只设下限，大字体换行时随内容增高而不是被截断。
// 属性名用 active 而不是 enabled：enabled 是 CustomComponent 自带的通用属性，同名会被编译器拒绝。
@Component
export struct PrimaryAction {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop label: string = '';
  @Prop icon: Resource = $r('sys.symbol.link');
  @Prop active: boolean = true;
  @Prop settings: AppPreferences = new AppPreferences();
  onAction: () => void = () => {};

  build() {
    Button({ type: ButtonType.Normal }) {
      Row({ space: 8 }) {
        SymbolGlyph(this.icon).fontSize(19).accessibilityLevel('no')
          .fontColor([this.active ? this.palette.onAccent : $r('app.color.text_tertiary')])
        Text(this.label).fontSize(16).fontWeight(FontWeight.Medium)
          .fontColor(this.active ? this.palette.onAccent : $r('app.color.text_tertiary'))
      }.justifyContent(FlexAlign.Center).width('100%')
    }.constraintSize({ minHeight: 50 }).width('100%').borderRadius(16).enabled(this.active).opacity(1)
      .clickEffect(pressFeedback(this.settings)).backgroundColor(this.active ? this.palette.accent : $r('app.color.surface_muted'))
      .onClick(this.onAction)
  }
}

// 次要操作按钮：灰底文字。
@Component
export struct SecondaryAction {
  @Prop label: string = '';
  @Prop active: boolean = true;
  @Prop settings: AppPreferences = new AppPreferences();
  onAction: () => void = () => {};

  build() {
    Button(this.label, { type: ButtonType.Normal }).constraintSize({ minHeight: 44 }).fontSize(14).borderRadius(12)
      .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
      .enabled(this.active).clickEffect(pressFeedback(this.settings)).onClick(this.onAction)
  }
}
