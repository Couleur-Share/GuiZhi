/**
 * 取色器用的颜色换算。
 *
 * 单独成文件是为了让 HSV/HEX 的边界（三位简写、越界分量、round-trip 漂移）
 * 能在不启 DOM 的情况下直接单测。
 */

export interface Hsv {
  /** 色相，0 ~ 360 */
  h: number;
  /** 饱和度，0 ~ 1 */
  s: number;
  /** 明度，0 ~ 1 */
  v: number;
}

export interface Rgb {
  r: number;
  g: number;
  b: number;
}

const HEX_PATTERN = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i;

export function clamp(value: number, min: number, max: number): number {
  if (Number.isNaN(value)) {
    return min;
  }
  return Math.min(Math.max(value, min), max);
}

/** 把用户输入的十六进制色统一成 `#rrggbb`；无法识别时返回 null */
export function normalizeHex(input: string): string | null {
  const match = HEX_PATTERN.exec(input.trim());
  if (!match) {
    return null;
  }

  const body = match[1].toLowerCase();
  const expanded =
    body.length === 3
      ? body
          .split("")
          .map((char) => `${char}${char}`)
          .join("")
      : body;

  return `#${expanded}`;
}

export function rgbToHex({ r, g, b }: Rgb): string {
  const channel = (value: number) =>
    Math.round(clamp(value, 0, 255))
      .toString(16)
      .padStart(2, "0");
  return `#${channel(r)}${channel(g)}${channel(b)}`;
}

export function hexToRgb(hex: string): Rgb | null {
  const normalized = normalizeHex(hex);
  if (!normalized) {
    return null;
  }

  return {
    r: Number.parseInt(normalized.slice(1, 3), 16),
    g: Number.parseInt(normalized.slice(3, 5), 16),
    b: Number.parseInt(normalized.slice(5, 7), 16),
  };
}

export function rgbToHsv({ r, g, b }: Rgb): Hsv {
  const red = clamp(r, 0, 255) / 255;
  const green = clamp(g, 0, 255) / 255;
  const blue = clamp(b, 0, 255) / 255;

  const max = Math.max(red, green, blue);
  const min = Math.min(red, green, blue);
  const delta = max - min;

  let h = 0;
  if (delta !== 0) {
    if (max === red) {
      h = 60 * (((green - blue) / delta + 6) % 6);
    } else if (max === green) {
      h = 60 * ((blue - red) / delta + 2);
    } else {
      h = 60 * ((red - green) / delta + 4);
    }
  }

  return {
    h,
    s: max === 0 ? 0 : delta / max,
    v: max,
  };
}

export function hsvToRgb({ h, s, v }: Hsv): Rgb {
  const hue = ((h % 360) + 360) % 360;
  const saturation = clamp(s, 0, 1);
  const value = clamp(v, 0, 1);

  const chroma = value * saturation;
  const secondary = chroma * (1 - Math.abs(((hue / 60) % 2) - 1));
  const offset = value - chroma;

  const sector = Math.floor(hue / 60) % 6;
  const table: Rgb[] = [
    { r: chroma, g: secondary, b: 0 },
    { r: secondary, g: chroma, b: 0 },
    { r: 0, g: chroma, b: secondary },
    { r: 0, g: secondary, b: chroma },
    { r: secondary, g: 0, b: chroma },
    { r: chroma, g: 0, b: secondary },
  ];
  const base = table[sector];

  return {
    r: (base.r + offset) * 255,
    g: (base.g + offset) * 255,
    b: (base.b + offset) * 255,
  };
}

export function hexToHsv(hex: string): Hsv | null {
  const rgb = hexToRgb(hex);
  return rgb ? rgbToHsv(rgb) : null;
}

export function hsvToHex(hsv: Hsv): string {
  return rgbToHex(hsvToRgb(hsv));
}
