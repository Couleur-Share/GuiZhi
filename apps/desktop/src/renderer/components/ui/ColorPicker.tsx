import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { clamp, hexToHsv, hsvToHex, normalizeHex, type Hsv } from "./color-utils";

const PANEL_WIDTH = 232;
const PANEL_HEIGHT = 248;
const FALLBACK_HSV: Hsv = { h: 217, s: 0.76, v: 0.96 };

const HUE_TRACK_BACKGROUND =
  "linear-gradient(to right, #ff0000 0%, #ffff00 17%, #00ff00 33%, #00ffff 50%, #0000ff 67%, #ff00ff 83%, #ff0000 100%)";

interface ColorPickerProps {
  /** 十六进制颜色，形如 `#3b82f6` */
  value: string;
  onChange: (hex: string) => void;
  ariaLabel: string;
  className?: string;
}

/**
 * 应用内取色器。
 *
 * 不用 `<input type="color">`：Windows 上它会拉起系统取色对话框，
 * 和应用的玻璃拟态外观完全脱节，且无法做任何样式约束。
 */
export function ColorPicker({
  value,
  onChange,
  ariaLabel,
  className = "",
}: ColorPickerProps) {
  const { t } = useTranslation();
  const panelId = useId();
  const [isOpen, setIsOpen] = useState(false);
  const [hsv, setHsv] = useState<Hsv>(() => hexToHsv(value) ?? FALLBACK_HSV);
  const [hexDraft, setHexDraft] = useState(() => normalizeHex(value) ?? "");
  const [panelPosition, setPanelPosition] = useState<{
    top?: number;
    bottom?: number;
    left: number;
  } | null>(null);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  // 自己发出去的值会绕一圈从 props 回来，记下来才能区分「外部改色」和「回声」
  const lastEmittedRef = useRef<string | null>(null);

  const normalizedValue = normalizeHex(value);
  const swatchColor = normalizedValue ?? hsvToHex(hsv);

  useEffect(() => {
    if (!normalizedValue || normalizedValue === lastEmittedRef.current) {
      return;
    }
    const next = hexToHsv(normalizedValue);
    if (next) {
      setHsv(next);
    }
    setHexDraft(normalizedValue);
  }, [normalizedValue]);

  const emit = useCallback(
    (hex: string) => {
      lastEmittedRef.current = hex;
      setHexDraft(hex);
      onChange(hex);
    },
    [onChange],
  );

  const updateFromHsv = useCallback(
    (next: Hsv) => {
      setHsv(next);
      emit(hsvToHex(next));
    },
    [emit],
  );

  useLayoutEffect(() => {
    if (!isOpen) {
      return;
    }

    const updatePosition = () => {
      const rect = triggerRef.current?.getBoundingClientRect();
      if (!rect) {
        return;
      }

      const spaceBelow = window.innerHeight - rect.bottom - 12;
      const openUpwards = spaceBelow < PANEL_HEIGHT && rect.top > spaceBelow;
      const maxLeft = Math.max(12, window.innerWidth - PANEL_WIDTH - 12);

      // PANEL_HEIGHT 只用来判断往哪边翻；真正定位锚的是紧挨触发器的那条边，
      // 免得估高与实际高度对不上时面板悬空
      setPanelPosition({
        top: openUpwards ? undefined : rect.bottom + 8,
        bottom: openUpwards ? window.innerHeight - rect.top + 8 : undefined,
        left: Math.min(Math.max(12, rect.right - PANEL_WIDTH), maxLeft),
      });
    };

    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) {
      return;
    }

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (
        triggerRef.current?.contains(target) ||
        panelRef.current?.contains(target)
      ) {
        return;
      }
      setIsOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setIsOpen(false);
        triggerRef.current?.focus();
      }
    };

    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [isOpen]);

  const handlePadPointer = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.type === "pointermove" && event.buttons !== 1) {
      return;
    }
    if (event.type === "pointerdown") {
      event.currentTarget.setPointerCapture(event.pointerId);
    }

    const rect = event.currentTarget.getBoundingClientRect();
    updateFromHsv({
      h: hsv.h,
      s: clamp((event.clientX - rect.left) / rect.width, 0, 1),
      v: 1 - clamp((event.clientY - rect.top) / rect.height, 0, 1),
    });
  };

  const handleHuePointer = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.type === "pointermove" && event.buttons !== 1) {
      return;
    }
    if (event.type === "pointerdown") {
      event.currentTarget.setPointerCapture(event.pointerId);
    }

    const rect = event.currentTarget.getBoundingClientRect();
    updateFromHsv({
      ...hsv,
      h: clamp((event.clientX - rect.left) / rect.width, 0, 1) * 360,
    });
  };

  const nudge = (deltaS: number, deltaV: number) => {
    updateFromHsv({
      h: hsv.h,
      s: clamp(hsv.s + deltaS, 0, 1),
      v: clamp(hsv.v + deltaV, 0, 1),
    });
  };

  const commitHexDraft = () => {
    const normalized = normalizeHex(hexDraft);
    if (!normalized) {
      setHexDraft(normalizedValue ?? hsvToHex(hsv));
      return;
    }
    const next = hexToHsv(normalized);
    if (next) {
      setHsv(next);
    }
    emit(normalized);
  };

  const panel =
    isOpen && panelPosition && typeof document !== "undefined"
      ? createPortal(
          <div
            id={panelId}
            ref={panelRef}
            role="dialog"
            aria-label={ariaLabel}
            className="fixed z-[9999] rounded-xl border border-border bg-popover p-3 shadow-lg animate-in fade-in-0 zoom-in-95 duration-quick ease-enter"
            style={{
              top: panelPosition.top,
              bottom: panelPosition.bottom,
              left: panelPosition.left,
              width: PANEL_WIDTH,
            }}
          >
            <div
              role="application"
              aria-label={t("common.colorSaturationBrightness")}
              tabIndex={0}
              onPointerDown={handlePadPointer}
              onPointerMove={handlePadPointer}
              onKeyDown={(event) => {
                const step = event.shiftKey ? 0.1 : 0.02;
                if (event.key === "ArrowLeft") nudge(-step, 0);
                else if (event.key === "ArrowRight") nudge(step, 0);
                else if (event.key === "ArrowUp") nudge(0, step);
                else if (event.key === "ArrowDown") nudge(0, -step);
                else return;
                event.preventDefault();
              }}
              className="relative h-[136px] w-full cursor-crosshair touch-none rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
              style={{
                backgroundColor: `hsl(${hsv.h} 100% 50%)`,
                backgroundImage:
                  "linear-gradient(to top, #000000, rgba(0,0,0,0)), linear-gradient(to right, #ffffff, rgba(255,255,255,0))",
              }}
            >
              <span
                aria-hidden="true"
                className="pointer-events-none absolute h-3.5 w-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white shadow ring-1 ring-black/30"
                style={{
                  left: `${hsv.s * 100}%`,
                  top: `${(1 - hsv.v) * 100}%`,
                  backgroundColor: swatchColor,
                }}
              />
            </div>

            <div
              role="slider"
              aria-label={t("common.colorHue")}
              aria-valuemin={0}
              aria-valuemax={360}
              aria-valuenow={Math.round(hsv.h)}
              tabIndex={0}
              onPointerDown={handleHuePointer}
              onPointerMove={handleHuePointer}
              onKeyDown={(event) => {
                const step = event.shiftKey ? 10 : 2;
                if (event.key === "ArrowLeft" || event.key === "ArrowDown") {
                  updateFromHsv({ ...hsv, h: (hsv.h - step + 360) % 360 });
                } else if (
                  event.key === "ArrowRight" ||
                  event.key === "ArrowUp"
                ) {
                  updateFromHsv({ ...hsv, h: (hsv.h + step) % 360 });
                } else {
                  return;
                }
                event.preventDefault();
              }}
              className="relative mt-3 h-3 w-full cursor-pointer touch-none rounded-full focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
              style={{ backgroundImage: HUE_TRACK_BACKGROUND }}
            >
              <span
                aria-hidden="true"
                className="pointer-events-none absolute top-1/2 h-4 w-4 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white shadow ring-1 ring-black/30"
                style={{
                  left: `${(hsv.h / 360) * 100}%`,
                  backgroundColor: `hsl(${hsv.h} 100% 50%)`,
                }}
              />
            </div>

            <div className="mt-3 flex items-center gap-2">
              <span
                aria-hidden="true"
                className="h-7 w-7 shrink-0 rounded-md border border-border"
                style={{ backgroundColor: swatchColor }}
              />
              <input
                type="text"
                value={hexDraft}
                spellCheck={false}
                aria-label={t("common.colorHex")}
                onChange={(event) => setHexDraft(event.target.value)}
                onBlur={commitHexDraft}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    commitHexDraft();
                  }
                }}
                className="h-8 w-full min-w-0 rounded-md border border-border bg-background px-2 font-mono text-xs uppercase outline-none focus:ring-2 focus:ring-primary/30"
              />
            </div>
          </div>,
          document.body,
        )
      : null;

  return (
    <div className={`relative ${className}`}>
      <button
        ref={triggerRef}
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        onClick={() => setIsOpen((open) => !open)}
        className="h-9 w-10 rounded-lg border border-border p-1 transition-colors hover:border-primary/50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
      >
        <span
          aria-hidden="true"
          className="block h-full w-full rounded-md"
          style={{ backgroundColor: swatchColor }}
        />
      </button>
      {panel}
    </div>
  );
}

export default ColorPicker;
