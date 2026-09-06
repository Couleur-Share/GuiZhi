import { useCallback, useEffect, useRef, useState } from "react";
import QRCode from "qrcode";
import type {
  CaptureDevice,
  CapturePairing,
  MobileCaptureSettings,
} from "@guizhi/shared/types/mobile-capture";
import { useToast } from "../ui/Toast";

export function useMobileCaptureSettings(
  text: (zh: string, en: string) => string,
) {
  const { showToast } = useToast();
  const [settings, setSettings] = useState<MobileCaptureSettings | null>(null);
  const [error, setError] = useState("");
  const [origin, setOrigin] = useState("");
  const [invite, setInvite] = useState("");
  const [devices, setDevices] = useState<CaptureDevice[]>([]);
  const [pairings, setPairings] = useState<CapturePairing[]>([]);
  const [collections, setCollections] = useState<
    { id: string; name: string }[]
  >([]);
  const [qr, setQr] = useState("");
  const [expires, setExpires] = useState(0);
  const [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState(false);
  const [pairingAction, setPairingAction] = useState<
    "generate" | "confirm" | ""
  >("");
  const [pairingSuccess, setPairingSuccess] = useState("");
  const awaitingPairing = expires > now;
  const [destructive, setDestructive] = useState<string | null>(null);
  const working = useRef(false);
  const revision = useRef(0);

  const refresh = useCallback(async () => {
    const request = ++revision.current;
    try {
      const next = await window.api.mobileCapture.status();
      const [nextCollections, nextDevices, nextPairings] = await Promise.all([
        window.api.collection.list(),
        next.connected
          ? window.api.mobileCapture.devices()
          : Promise.resolve([]),
        next.connected
          ? window.api.mobileCapture.pairings()
          : Promise.resolve([]),
      ]);
      // 仅提交完整快照，避免设备加载失败显示成空态，或旧请求覆盖新设置。
      if (request !== revision.current) return;
      setSettings(next);
      setCollections(nextCollections);
      setDevices(nextDevices);
      setPairings(nextPairings);
      setError("");
    } catch (cause) {
      if (request !== revision.current) return;
      throw cause;
    }
  }, []);

  async function perform(
    fn?: () => Promise<unknown>,
    action: "generate" | "confirm" | "" = "",
  ) {
    if (working.current) return;
    working.current = true;
    revision.current += 1;
    setBusy(true);
    setPairingAction(action);
    if (action) setPairingSuccess("");
    try {
      await fn?.();
      await refresh();
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause);
      setError(message);
      showToast(text("手机收集操作失败", "Mobile capture failed"), "error", {
        detail: message,
      });
    } finally {
      working.current = false;
      setBusy(false);
      setPairingAction("");
    }
  }

  useEffect(() => {
    let disposed = false;
    let polling = false;
    const poll = () => {
      if (working.current || polling || document.visibilityState !== "visible")
        return;
      polling = true;
      void refresh()
        .catch((cause) => {
          if (!disposed)
            setError(cause instanceof Error ? cause.message : String(cause));
        })
        .finally(() => {
          polling = false;
        });
    };
    const online = () => {
      if (working.current) return;
      void window.api.mobileCapture
        .fetch()
        .then(poll)
        .catch((cause) => {
          if (!disposed)
            setError(cause instanceof Error ? cause.message : String(cause));
        });
    };
    poll();
    // 配对时缩短等待；切回窗口立即检查，慢请求不重叠。
    const timer = setInterval(poll, awaitingPairing ? 2000 : 10000);
    window.addEventListener("online", online);
    window.addEventListener("focus", poll);
    document.addEventListener("visibilitychange", poll);
    return () => {
      disposed = true;
      revision.current += 1;
      clearInterval(timer);
      window.removeEventListener("online", online);
      window.removeEventListener("focus", poll);
      document.removeEventListener("visibilitychange", poll);
    };
  }, [refresh, awaitingPairing]);

  useEffect(() => {
    if (!expires && !pairings.length) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [expires, pairings.length]);

  const generatePairing = () =>
    void perform(async () => {
      const pairing = await window.api.mobileCapture.pair();
      setQr(await QRCode.toDataURL(pairing.url, { width: 240, margin: 2 }));
      setExpires(pairing.expiresAt);
      setNow(Date.now());
    }, "generate");
  const confirmPairing = (pairing: CapturePairing) =>
    void perform(async () => {
      await window.api.mobileCapture.confirm(pairing.id, pairing.deviceId!);
      setQr("");
      setExpires(0);
      setPairingSuccess(
        text(
          "绑定成功，手机页面会自动更新，现在可以开始收集。",
          "Phone paired. The mobile page updates automatically and is ready to capture.",
        ),
      );
    }, "confirm");
  const revokeConnection = () =>
    void perform(async () => {
      if (destructive === "mailbox") {
        await window.api.mobileCapture.disable();
        setQr("");
        setExpires(0);
      } else if (destructive)
        await window.api.mobileCapture.revoke(destructive);
      setDestructive(null);
    });
  return {
    settings,
    error,
    origin,
    setOrigin,
    invite,
    setInvite,
    devices,
    pairings,
    collections,
    qr,
    expires,
    now,
    busy,
    pairingAction,
    pairingSuccess,
    destructive,
    setDestructive,
    perform,
    refresh,
    generatePairing,
    confirmPairing,
    revokeConnection,
  };
}
