// 主题色与深浅模式独立；资源由系统随颜色模式解析，避免缓存过期的深色/浅色值。
export class ThemePalette {
  id: string;
  label: string;
  accent: Resource;
  soft: Resource;
  onAccent: Resource;
  constructor(id: string, label: string, accent: Resource, soft: Resource, onAccent: Resource) {
    this.id = id; this.label = label; this.accent = accent; this.soft = soft; this.onAccent = onAccent;
  }
}

export const THEME_COLOR_IDS: string[] = ['blue', 'purple', 'rose', 'amber', 'slate'];

export function themePalette(id: string): ThemePalette {
  switch (id) {
    case 'purple': return new ThemePalette(id, '柔和紫', $r('app.color.purple_accent'), $r('app.color.purple_soft'), $r('app.color.purple_on_accent'));
    case 'rose': return new ThemePalette(id, '玫瑰', $r('app.color.rose_accent'), $r('app.color.rose_soft'), $r('app.color.rose_on_accent'));
    case 'amber': return new ThemePalette(id, '琥珀', $r('app.color.amber_accent'), $r('app.color.amber_soft'), $r('app.color.amber_on_accent'));
    case 'slate': return new ThemePalette(id, '中性灰', $r('app.color.slate_accent'), $r('app.color.slate_soft'), $r('app.color.slate_on_accent'));
    default: return new ThemePalette('blue', '清爽蓝', $r('app.color.accent'), $r('app.color.accent_soft'), $r('app.color.on_accent'));
  }
}
