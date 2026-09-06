import { Check, Loader2, QrCode, RefreshCw, Smartphone } from "lucide-react";
import type { CapturePairing } from "@guizhi/shared/types/mobile-capture";
import { Button } from "../ui/Button";

interface Props {
  action: "generate" | "confirm" | "";
  success: string;
  qr: string;
  expires: number;
  now: number;
  pairings: CapturePairing[];
  busy: boolean;
  text: (zh: string, en: string) => string;
  onGenerate: () => void;
  onConfirm: (pairing: CapturePairing) => void;
}

export function MobileCapturePairing({
  action,
  success,
  qr,
  expires,
  now,
  pairings,
  busy,
  text,
  onGenerate,
  onConfirm,
}: Props) {
  const seconds = Math.max(0, Math.ceil((expires - now) / 1000));
  const activeQr = qr && seconds > 0;
  const pending = pairings.filter(
    (pairing) => pairing.deviceId && pairing.expiresAt > now,
  );
  const steps = [
    [
      text("扫码打开收集页", "Scan to open capture"),
      text(
        "用手机扫描二维码，在浏览器中打开。",
        "Scan the QR code with your phone and open it in a browser.",
      ),
    ],
    [
      text("回到电脑确认绑定", "Confirm on this desktop"),
      text(
        "手机提交设备名称后，在这里核对并确认。",
        "Name your device on the phone, then review and confirm it here.",
      ),
    ],
    [
      text("随时提交链接或文字", "Send links or text anytime"),
      text(
        "在手机收集页粘贴内容并提交，电脑上线后自动取回。",
        "Paste content into the mobile capture page and submit. Your desktop retrieves it when online.",
      ),
    ],
  ];

  return (
    <section className="app-settings-card overflow-hidden">
      {pending.length > 0 && (
        <div className="space-y-3 border-b border-primary/20 bg-primary/5 p-5">
          <p role="status" className="text-sm font-medium text-primary">
            {text(
              "有设备正在等待你的确认",
              "A device is waiting for confirmation",
            )}
          </p>
          {pending.map((pairing) => (
            <div
              key={pairing.id}
              className="flex flex-wrap items-center justify-between gap-3"
            >
              <p className="min-w-0 break-words text-sm">
                {pairing.name || text("未命名设备", "Unnamed device")}
              </p>
              <Button
                size="sm"
                disabled={busy}
                aria-busy={action === "confirm"}
                onClick={() => onConfirm(pairing)}
              >
                {action === "confirm" ? (
                  <Loader2
                    aria-hidden="true"
                    className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                  />
                ) : (
                  <Check className="h-3.5 w-3.5" />
                )}
                {action === "confirm"
                  ? text("正在确认绑定…", "Confirming pairing…")
                  : text("确认绑定此设备", "Confirm this device")}
              </Button>
            </div>
          ))}
        </div>
      )}
      <div className="grid lg:grid-cols-[minmax(0,1fr)_280px]">
        <div className="p-5 sm:p-6">
          <h3 className="flex items-center gap-2 font-semibold">
            <Smartphone className="h-4 w-4 text-primary" />
            {text("绑定手机，开始收集", "Pair a phone to get started")}
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {text(
              "只需绑定一次，之后直接打开手机收集页即可。",
              "Pair once, then return to the mobile capture page whenever you need it.",
            )}
          </p>
          <ol className="mt-6 space-y-5">
            {steps.map(([title, description], index) => (
              <li key={index} className="flex gap-3">
                <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
                  {index + 1}
                </span>
                <div>
                  <p className="text-sm font-medium">{title}</p>
                  <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                    {description}
                  </p>
                </div>
              </li>
            ))}
          </ol>
        </div>
        <div className="flex flex-col items-center justify-center gap-3 border-t border-border/70 bg-muted/20 p-5 lg:border-l lg:border-t-0">
          {activeQr ? (
            <>
              <img
                className="rounded-xl"
                src={qr}
                alt={text("手机配对二维码", "Phone pairing QR code")}
                width={180}
                height={180}
              />
              <p className="text-xs tabular-nums text-muted-foreground">
                {text("有效期剩余", "Expires in")} {Math.floor(seconds / 60)}:
                {String(seconds % 60).padStart(2, "0")}
              </p>
              <p
                role="status"
                className="flex items-center gap-2 text-xs text-primary"
              >
                <Loader2
                  aria-hidden="true"
                  className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                />
                {text(
                  pending.length
                    ? "已收到手机请求，请核对并确认"
                    : "等待手机提交绑定请求…",
                  pending.length
                    ? "Phone request received. Review and confirm."
                    : "Waiting for a pairing request from your phone…",
                )}
              </p>
              {!pending.length && (
                <p className="text-center text-xs leading-relaxed text-muted-foreground">
                  {text(
                    "手机填写设备名称并提交后，这里会自动显示，无需刷新。",
                    "After you submit your device name on the phone, the request appears here automatically.",
                  )}
                </p>
              )}
            </>
          ) : (
            <>
              <div className="flex h-28 w-28 items-center justify-center rounded-2xl border border-dashed border-primary/25 bg-background/70">
                <QrCode
                  className="h-12 w-12 text-primary/60"
                  strokeWidth={1.25}
                  aria-hidden="true"
                />
              </div>
              <p role="status" className="text-xs text-muted-foreground">
                {expires
                  ? text(
                      "二维码已过期，请重新生成",
                      "QR code expired. Generate a new one.",
                    )
                  : text(
                      "二维码 5 分钟内有效",
                      "QR code is valid for 5 minutes",
                    )}
              </p>
            </>
          )}
          <Button
            variant={activeQr ? "ghost" : "primary"}
            size="sm"
            disabled={busy}
            aria-busy={action === "generate"}
            onClick={onGenerate}
          >
            {action === "generate" ? (
              <Loader2
                aria-hidden="true"
                className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
              />
            ) : activeQr || expires ? (
              <RefreshCw className="h-3.5 w-3.5" />
            ) : (
              <QrCode className="h-3.5 w-3.5" />
            )}
            {action === "generate"
              ? text("正在生成二维码…", "Generating QR code…")
              : activeQr || expires
                ? text("重新生成二维码", "Generate a new QR code")
                : text("生成配对二维码", "Generate pairing QR")}
          </Button>
        </div>
      </div>
      {success && (
        <p
          role="status"
          className="flex items-center gap-2 border-t border-primary/20 bg-primary/5 p-5 text-sm text-primary"
        >
          <Check aria-hidden="true" className="h-4 w-4 shrink-0" />
          {success}
        </p>
      )}

    </section>
  );
}
