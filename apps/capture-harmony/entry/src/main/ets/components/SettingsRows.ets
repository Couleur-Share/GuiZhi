import { SymbolGlyphModifier } from '@kit.ArkUI';
import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { pressFeedback } from '../services/InterfaceMotion';
import { panelMaterial } from '../services/ImmersiveAppearance';

@Component
export struct SettingChoiceRow {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop title: string = '';
  @Prop value: string = '';
  @Prop icon: Resource = $r('sys.symbol.paintpalette_fill');
  @Prop interactive: boolean = true;
  items: MenuElement[] = [];

  @Builder
  private ChoiceMenu() {
    Menu() {
      ForEach(this.items, (item: MenuElement) => {
        MenuItem({ content: item.value,
          symbolEndIcon: item.value === this.value ? new SymbolGlyphModifier($r('sys.symbol.checkmark')) : undefined })
          .selected(item.value === this.value).enabled(this.interactive)
          .onClick(() => { if (this.interactive && item.action) item.action(); })
      }, (item: MenuElement) => JSON.stringify(item.value))
    }
  }

  build() {
    Button({ type: ButtonType.Normal }) {
      Row({ space: 12 }) {
        SymbolGlyph(this.icon).fontSize(22).fontColor([this.palette.accent])
        Text(this.title).fontSize(16).fontColor($r('app.color.text_primary')).layoutWeight(1)
          .textAlign(TextAlign.Start)
        Text(this.value).fontSize(13).fontColor($r('app.color.text_secondary')).constraintSize({ maxWidth: '42%' })
        SymbolGlyph($r('sys.symbol.chevron_down')).fontSize(12).fontColor([$r('app.color.text_tertiary')])
      }.width('100%').constraintSize({ minHeight: 60 }).padding({ left: 16, right: 16, top: 8, bottom: 8 })
    }.width('100%').padding(0).backgroundColor(Color.Transparent).borderRadius(16)
      .enabled(this.interactive).opacity(this.interactive ? 1 : 0.5).clickEffect(pressFeedback(this.settings))
      .accessibilityText(`${this.title}，${this.value}`).bindMenu(this.ChoiceMenu(), { systemMaterial: panelMaterial(this.settings) })
  }
}

@Component
export struct SettingSwitchRow {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop title: string = '';
  @Prop value: boolean = false;
  @Prop icon: Resource = $r('sys.symbol.livephoto');
  @Prop interactive: boolean = true;
  onChange: (value: boolean) => void = () => {};
  build() {
    Row({ space: 12 }) {
      SymbolGlyph(this.icon).fontSize(22).fontColor([this.palette.accent])
      Text(this.title).fontSize(16).fontColor($r('app.color.text_primary')).layoutWeight(1)
      Toggle({ type: ToggleType.Switch, isOn: this.value }).selectedColor(this.palette.accent)
        .enabled(this.interactive).accessibilityText(this.title)
        .onChange((checked: boolean) => { if (checked !== this.value) this.onChange(checked); })
    }.width('100%').constraintSize({ minHeight: 60 }).padding({ left: 16, right: 16, top: 8, bottom: 8 })
      .opacity(this.interactive ? 1 : 0.5)
  }
}

@Component
export struct SettingsRouteRow {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop title: string = '';
  @Prop detail: string = '';
  @Prop icon: Resource = $r('sys.symbol.paintpalette_fill');
  onOpen: () => void = () => {};
  build() {
    Button({ type: ButtonType.Normal }) {
      Row({ space: 14 }) {
        SymbolGlyph(this.icon).fontSize(24).fontColor([this.palette.accent])
        Column({ space: 4 }) {
          Text(this.title).fontSize(16).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
          Text(this.detail).fontSize(13).fontColor($r('app.color.text_secondary')).maxLines(2)
        }.layoutWeight(1).alignItems(HorizontalAlign.Start)
        SymbolGlyph($r('sys.symbol.chevron_right')).fontSize(14).fontColor([$r('app.color.text_tertiary')])
      }.padding(18).width('100%').constraintSize({ minHeight: 80 })
    }.width('100%').padding(0).backgroundColor($r('app.color.surface')).borderRadius(20)
      .clickEffect(pressFeedback(this.settings)).accessibilityText(`${this.title}，${this.detail}`).onClick(this.onOpen)
  }
}
