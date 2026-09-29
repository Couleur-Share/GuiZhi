// 列表分组标题（今天 / 昨天 / 更早）：位于分组卡片顶部，背景与卡片一致。
@Component
export struct GroupHeader {
  @Prop label: string = '';
  @Prop count: number = 0;

  build() {
    Row({ space: 6 }) {
      Text(this.label).fontSize(13).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_secondary'))
      Text(`${this.count}`).fontSize(12).fontColor($r('app.color.text_tertiary'))
    }.width('100%').padding({ left: 16, right: 16, top: 12, bottom: 4 }).backgroundColor($r('app.color.surface'))
      .accessibilityGroup(true).accessibilityText(`${this.label}，${this.count} 条`)
  }
}
