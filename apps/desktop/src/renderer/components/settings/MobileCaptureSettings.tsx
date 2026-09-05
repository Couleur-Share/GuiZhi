import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";
import type { CaptureDevice, CapturePairing, MobileCaptureSettings as Settings } from "@guizhi/shared/types/mobile-capture";
import { Select } from "../ui/Select";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { useToast } from "../ui/Toast";
const button = "px-3 py-2 rounded-lg border border-border hover:bg-muted disabled:opacity-50 text-sm";
export function MobileCaptureSettings() {
  const { i18n } = useTranslation(), { showToast } = useToast();
  const en = i18n.language.startsWith("en");
  const text = (zh: string, english: string) => en ? english : zh;
  const [settings, setSettings] = useState<Settings | null>(null), [error, setError] = useState("");
  const [origin, setOrigin] = useState(""), [invite, setInvite] = useState("");
  const [devices, setDevices] = useState<CaptureDevice[]>([]), [pairings, setPairings] = useState<CapturePairing[]>([]);
  const [collections, setCollections] = useState<{ id: string; name: string }[]>([]);
  const [qr, setQr] = useState(""), [expires, setExpires] = useState(0), [busy, setBusy] = useState(false);
  const [destructive, setDestructive] = useState<string | null>(null);
  async function refresh() {
    const state = await window.api.mobileCapture.status(); setSettings(state);
    setCollections(await window.api.collection.list());
    if (state.connected) {
      const [nextDevices, nextPairs] = await Promise.all([window.api.mobileCapture.devices(), window.api.mobileCapture.pairings()]);
      setDevices(nextDevices); setPairings(nextPairs);
    }
    setError("");
  }
  async function perform(fn: () => Promise<unknown>) {
    setBusy(true);
    try { await fn(); await refresh(); }
    catch (e) { const message = e instanceof Error ? e.message : String(e); setError(message); showToast(text("手机收集操作失败", "Mobile capture failed"), "error", { detail: message }); }
    finally { setBusy(false); }
  }
  useEffect(() => {
    void refresh().catch(e => setError(String(e)));
    const timer = setInterval(() => { void refresh().catch(e => setError(String(e))); if (expires && Date.now() >= expires) setQr(""); }, 10000);
    const online = () => { void window.api.mobileCapture.fetch().then(setSettings).catch(e => setError(String(e))); };
    window.addEventListener("online", online);
    return () => { clearInterval(timer); window.removeEventListener("online", online); };
  }, [expires]);
  return <div className="space-y-5 text-foreground">
    <p className="text-sm text-muted-foreground">{text("手机分享链接或文字，电脑上线后自动取回，使用本机的采集配置。", "Share links or text from your phone. This desktop collects them when online, using its own capture settings.")}</p>
    {error && <div role="alert" className="p-3 rounded-lg bg-muted"><p>{error}</p><button className={button} onClick={() => void perform(refresh)}>{text("重试", "Retry")}</button></div>}
    {settings && !settings.persistent && <p role="status" className="text-sm">{text("系统安全存储不可用。凭证只保留到本次退出，重启后需要新建收件箱并重新配对。", "Secure storage is unavailable. Credentials last for this session only; create a new inbox and pair again after restarting.")}</p>}
    {settings && !settings.connected && <div className="rounded-xl border border-border p-4 space-y-3">
      <label className="block text-sm">{text("公共服务地址", "Service URL")}<input className="mt-1 w-full rounded-lg border border-border bg-background p-2" type="url" placeholder="https://…" value={origin} onChange={e => setOrigin(e.target.value)} /></label>
      <label className="block text-sm">{text("邀请码", "Invitation code")}<input type="password" autoComplete="off" className="mt-1 w-full rounded-lg border border-border bg-background p-2" value={invite} onChange={e => setInvite(e.target.value)} /></label>
      <button className={button} disabled={busy || !origin || !invite} onClick={() => void perform(async () => { setSettings(await window.api.mobileCapture.activate(origin, invite)); setInvite(""); })}>{text("激活手机收集", "Activate")}</button>
    </div>}
    {settings?.connected && <>
      <div className="rounded-xl border border-border p-4 space-y-3"><p className="text-sm break-all">{settings.origin}</p>
        <p role="status">{settings.paused ? text("取件已暂停", "Collection paused") : text("自动取件已开启", "Automatic collection enabled")}</p>
        {settings.error && <p role="status" className="text-sm">{settings.error}</p>}
        {settings.lastReceivedAt && <p className="text-xs text-muted-foreground">{text("上次取件", "Last received")}: {new Date(settings.lastReceivedAt).toLocaleString()}</p>}
        <div className="flex flex-wrap gap-2"><button disabled={busy} className={button} onClick={() => void perform(() => window.api.mobileCapture.configure(!settings.paused, settings.collectionId))}>{settings.paused ? text("检查连接并恢复", "Check and resume") : text("暂停取件", "Pause")}</button>
          <button disabled={busy || settings.paused} className={button} onClick={() => void perform(() => window.api.mobileCapture.fetch())}>{text("立即取件", "Collect now")}</button></div>
        <label className="block text-sm">{text("默认目标知识库", "Default collection")}</label><Select ariaLabel={text("默认目标知识库", "Default collection")} value={settings.collectionId ?? ""} disabled={busy} options={[{ value: "", label: text("未分类", "Unfiled") }, ...collections.map(c => ({ value: c.id, label: c.name }))]} onChange={id => void perform(() => window.api.mobileCapture.configure(settings.paused, id || null))} />
      </div>
      <div className="rounded-xl border border-border p-4 space-y-3"><h2 className="font-medium">{text("绑定手机", "Pair a phone")}</h2>
        <button disabled={busy} className={button} onClick={() => void perform(async () => { const pairing = await window.api.mobileCapture.pair(); setQr(await QRCode.toDataURL(pairing.url, { width: 240, margin: 2 })); setExpires(pairing.expiresAt); })}>{text("生成配对二维码", "Generate pairing QR")}</button>
        {qr && <div><img src={qr} alt={text("手机配对二维码", "Phone pairing QR code")} width={240} height={240} /><p className="text-xs text-muted-foreground">{text("5 分钟有效。扫码后需在此确认设备。", "Valid for 5 minutes. Confirm the device here after scanning.")}</p></div>}
        {pairings.filter(p => p.deviceId).map(p => <div key={p.id} className="flex items-center justify-between gap-2"><span>{p.name}</span><button className={button} disabled={busy} onClick={() => void perform(async () => { await window.api.mobileCapture.confirm(p.id, p.deviceId!); setQr(""); })}>{text("确认绑定此设备", "Confirm this device")}</button></div>)}
      </div>
      <div className="rounded-xl border border-border p-4 space-y-3"><h2 className="font-medium">{text("已绑定设备", "Paired devices")}</h2>{devices.filter(d => d.active).map(device => <div key={device.id} className="flex justify-between gap-2 items-center"><span>{device.name}</span><button className={button} disabled={busy} onClick={() => setDestructive(device.id)}>{text("撤销", "Revoke")}</button></div>)}</div>
      <button className={button} disabled={busy} onClick={() => setDestructive("mailbox")}>{text("停用此收件箱", "Disable this inbox")}</button>
    </>}
    <p className="text-xs text-muted-foreground">{text("公共服务能读取暂存内容；电脑确认后清除主库原文。此功能不会同步本地知识库。恢复备份后自动暂停取件，请检查连接再恢复。", "The public service can read queued content and clears payloads after desktop acknowledgement. Your knowledge library is not synchronized. Restoring a backup pauses collection until you review and resume.")}</p>
    <ConfirmDialog isOpen={!!destructive} onClose={() => setDestructive(null)} title={text("撤销连接", "Revoke connection")} message={text("此操作会阻止相应设备继续投递。停用收件箱后需要新邀请码重新激活。", "This prevents the device from submitting. Disabling the inbox requires a new invitation to reactivate.")} confirmText={text("确认撤销", "Revoke")} cancelText={text("取消", "Cancel")} isLoading={busy} onConfirm={() => void perform(async () => { if (destructive === "mailbox") { setSettings(await window.api.mobileCapture.disable()); setDevices([]); setPairings([]); setQr(""); } else if (destructive) await window.api.mobileCapture.revoke(destructive); setDestructive(null); })} />
  </div>;
}
