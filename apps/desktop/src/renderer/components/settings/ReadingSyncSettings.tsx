import { useCallback, useEffect, useRef, useState } from 'react';
import QRCode from 'qrcode';
import type { ReadingDesktopStatus } from '@guizhi/shared/types/reading-sync';
import { Button } from '../ui/Button';
import { Checkbox } from '../ui/Checkbox';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { LoadErrorState } from '../ui/LoadErrorState';
import { useToast } from '../ui/Toast';

// 默认入口关闭；主进程开放实验能力后才呈现，配合独立鸿蒙开发包试用。
export function ReadingSyncSettings() {
  const [status, setStatus] = useState<ReadingDesktopStatus | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [origin, setOrigin] = useState('');
  const [invite, setInvite] = useState('');
  const [collections, setCollections] = useState<{ id: string; name: string }[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [qr, setQr] = useState('');
  const [device, setDevice] = useState<string | null>(null);
  const [recovery, setRecovery] = useState('');
  const [confirm, setConfirm] = useState<'clear' | 'revoke' | null>(null);
  const working = useRef(false);
  const { showToast } = useToast();
  const refresh = useCallback(async () => {
    const next = await window.api.readingSync.status();
    setStatus(next); setSelected(next.collectionIds);
    if (next.available) setCollections(await window.api.collection.list());
    setError('');
  }, []);
  useEffect(() => { void refresh().catch(error => setError(String(error.message ?? error))); }, [refresh]);
  async function perform(action: () => Promise<unknown>) {
    if (working.current) return;
    working.current = true; setBusy(true);
    try { await action(); await refresh(); }
    catch (error) {
      const detail = error instanceof Error ? error.message : '操作失败';
      setError(detail); showToast('阅读同步未完成', 'error', { detail });
    } finally { working.current = false; setBusy(false); }
  }
  if (status && !status.available) return null;
  if (!status && !error) return null;
  if (!status) return <LoadErrorState message={error} onRetry={() => { void perform(refresh); }} />;
  const selectionChanged = [...selected].sort().join(',') !== [...status.collectionIds].sort().join(',');
  return <section className="space-y-4 rounded-2xl border border-border bg-card p-6" data-testid="reading-sync-settings">
    <div><h3 className="text-base font-semibold">手机阅读同步 · 试用</h3>
      <p className="mt-1 text-sm text-muted-foreground">将所选合集的加密阅读副本保存到你的同步服务。手机阅读仅在独立鸿蒙开发包中试用，支持下载正文、离线搜索和按需保存图片。</p></div>
    {error && <LoadErrorState message={error} onRetry={() => { void perform(refresh); }} />}
    {status.error && <p role="status" className="text-sm text-destructive">{status.error}</p>}
    {!status.configured ? <div className="space-y-3">
      <label className="block text-sm">服务地址<input className="mt-1 w-full rounded-lg border border-input bg-background p-2" value={origin} disabled={busy}
        placeholder="https://reading.example.com" onChange={event => setOrigin(event.target.value)} /></label>
      <label className="block text-sm">开通凭据<input type="password" autoComplete="off" className="mt-1 w-full rounded-lg border border-input bg-background p-2"
        value={invite} disabled={busy} onChange={event => setInvite(event.target.value)} /></label>
      <Button disabled={busy || !origin || !invite} onClick={() => { void perform(async () => { await window.api.readingSync.activate(origin, invite); setInvite(''); }); }}>开启阅读同步</Button>
    </div> : <>
      <p className="text-sm text-muted-foreground">{status.origin} · 已同步版本 {status.sequence}{status.pending ? ' · 有待重试快照' : ''}</p>
      <fieldset className="space-y-2" disabled={busy || status.pending}><legend className="mb-2 text-sm font-medium">选择阅读合集</legend>
        {collections.length === 0 && <p className="text-sm text-muted-foreground">还没有合集，请先创建。</p>}
        {collections.map(collection => <Checkbox key={collection.id} label={collection.name} checked={selected.includes(collection.id)}
          disabled={busy || status.pending} onChange={checked => setSelected(ids => checked ? [...ids, collection.id] : ids.filter(id => id !== collection.id))} />)}
      </fieldset>
      {selected.length === 0 && status.sequence > 0 && <p className="text-sm text-destructive">保存空选择并同步，会撤回云端全部阅读内容；PC 主库保留。</p>}
      {selectionChanged && <p className="text-sm text-muted-foreground">阅读范围已修改，请先保存选择再同步。</p>}
      <div className="flex flex-wrap gap-2">
        <Button disabled={busy || status.pending} onClick={() => { void perform(() => window.api.readingSync.configure(selected)); }}>保存选择</Button>
        <Button disabled={busy || selectionChanged} onClick={() => {
          if (status.collectionIds.length === 0 && status.sequence > 0 && !status.pending) setConfirm('clear');
          else void perform(() => window.api.readingSync.sync());
        }}>{busy ? '正在处理…' : status.pending ? '重试上传' : '立即同步'}</Button>
        <Button variant="secondary" disabled={busy || status.sequence === 0} onClick={() => { void perform(async () => {
          const pair = await window.api.readingSync.pair(); setQr(await QRCode.toDataURL(pair.url));
        }); }}>生成阅读配对码</Button>
        <Button variant="secondary" disabled={busy} onClick={() => { void perform(async () => {
          setRecovery(JSON.stringify(await window.api.readingSync.recovery(), null, 2));
        }); }}>显示恢复资料</Button>
        <Button variant="secondary" disabled={busy || status.sequence === 0} onClick={() => setConfirm('revoke')}>撤销阅读设备</Button>
      </div>
      {qr && <div className="space-y-2"><img src={qr} alt="手机阅读配对二维码，五分钟内有效" className="h-48 w-48 rounded-lg" />
        <p className="text-sm text-muted-foreground">仅让自己的手机扫描，二维码含一次性配对密钥。</p>
        <Button variant="secondary" disabled={busy} onClick={() => { void perform(async () => {
          const pair = await window.api.readingSync.pairStatus(); setDevice(pair?.deviceId ?? null);
          if (pair?.approved) { setQr(''); setDevice(null); }
        }); }}>刷新待确认设备</Button>
        {device && <p className="break-all text-sm">待确认设备：{device}</p>}
        {device && <Button disabled={busy} onClick={() => { void perform(async () => {
          await window.api.readingSync.approve(device); setQr(''); setDevice(null);
        }); }}>确认这台手机</Button>}
      </div>}
      {recovery && <div className="space-y-2"><p className="text-sm">请将下方恢复密钥与封装文件保存到安全位置。不要发送给他人。</p>
        <textarea aria-label="阅读密钥恢复资料" readOnly value={recovery} className="h-36 w-full rounded-lg border border-input bg-background p-2 font-mono text-xs" />
        <Button variant="secondary" onClick={() => setRecovery('')}>隐藏恢复资料</Button></div>}
    </>}
    <ConfirmDialog isOpen={confirm !== null} onClose={() => setConfirm(null)} title={confirm === 'revoke' ? '撤销阅读设备？' : '撤回全部阅读内容？'}
      message={confirm === 'revoke' ? '手机将无法继续下载，阅读同步也会冻结。密钥轮换将在后续阶段提供；本次试用将不能继续上传。已下载内容无法远程收回。' : '云端当前阅读清单将清空。PC 主库保留，已离线下载的内容需等手机同步后才会移除。'}
      confirmText={confirm === 'revoke' ? '撤销并冻结同步' : '撤回云端内容'} cancelText="取消" variant="destructive" isLoading={busy}
      onConfirm={() => { const action = confirm; setConfirm(null); void perform(async () => {
        if (action === 'revoke') { await window.api.readingSync.revoke(); setQr(''); setDevice(null); }
        else await window.api.readingSync.sync();
      }); }} />
  </section>;
}
