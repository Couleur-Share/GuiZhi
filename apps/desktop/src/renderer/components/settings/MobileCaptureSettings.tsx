import { useTranslation } from "react-i18next";
import {
  ArrowDownToLine,
  Check,
  Clock3,
  FolderOpen,
  Inbox,
  Loader2,
  Monitor,
  Pause,
  Play,
  ShieldCheck,
  Smartphone,
  Unplug,
} from "lucide-react";
import { Select } from "../ui/Select";
import { Button } from "../ui/Button";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { LoadErrorState } from "../ui/LoadErrorState";
import { MobileCapturePairing } from "./MobileCapturePairing";
import { useMobileCaptureSettings } from "./useMobileCaptureSettings";

export function MobileCaptureSettings() {
  const { i18n } = useTranslation();
  const text = (zh: string, en: string) =>
    i18n.language.startsWith("en") ? en : zh;
  const state = useMobileCaptureSettings(text);
  const { settings, error, busy, devices, collections, perform } = state;
  const activeDevices = devices.filter((device) => device.active);
  const status = settings?.error
    ? text("取件需要检查", "Collection needs attention")
    : settings?.paused
      ? text("取件已暂停", "Collection paused")
      : text("自动取件已开启", "Automatic collection enabled");

  return (
    <div
      className="space-y-6 text-foreground"
      data-testid="mobile-capture-settings"
    >
      <section className="relative overflow-hidden rounded-2xl border border-primary/20 bg-primary/5 p-6">
        <div
          className="pointer-events-none absolute right-0 -top-20 h-64 w-64 rounded-full bg-primary/5"
          aria-hidden="true"
        />
        <div className="relative grid items-center gap-5 lg:grid-cols-[minmax(0,1fr)_auto]">
          <div className="max-w-xl space-y-2">
            <p className="text-xs font-medium tracking-widest text-primary">
              {text("随手收集 · 回到电脑继续", "CAPTURE ON THE GO")}
            </p>
            <h2 className="text-xl font-semibold tracking-tight">
              {text(
                "把手机上的发现，带回归知",
                "Bring your discoveries to GuiZhi",
              )}
            </h2>
            <p className="text-sm leading-relaxed text-muted-foreground">
              {text(
                "在手机上提交链接或文字，电脑上线后自动取回，使用本机配置完成采集。",
                "Submit links or text from your phone. Your desktop retrieves them when online and processes them with your local capture settings.",
              )}
            </p>
          </div>
          <div
            aria-hidden="true"
            className="flex items-center gap-3 self-center rounded-2xl border border-primary/15 bg-background/60 px-5 py-4 text-primary"
          >
            <Smartphone className="h-8 w-8" />
            <span className="w-7 border-t border-dashed border-primary/40" />
            <Inbox className="h-6 w-6" />
            <span className="w-7 border-t border-dashed border-primary/40" />
            <Monitor className="h-8 w-8" />
          </div>
        </div>
      </section>
      {error && (
        <div className="app-settings-card">
          <LoadErrorState message={error} onRetry={() => void perform()} />
        </div>
      )}
      {!settings && !error && (
        <div
          role="status"
          className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground"
        >
          <Loader2 className="h-4 w-4 animate-spin" />
          {text("正在读取手机收集设置…", "Loading mobile capture settings…")}
        </div>
      )}
      {settings && !settings.persistent && (
        <p
          role="status"
          className="rounded-xl border border-border bg-muted/50 p-4 text-sm leading-relaxed"
        >
          {text(
            "系统安全存储不可用。凭证只保留到本次退出，重启后需要新建收件箱并重新配对。",
            "Secure storage is unavailable. Credentials last for this session only; create a new inbox and pair again after restarting.",
          )}
        </p>
      )}

      {settings && !settings.connected && (
        <section className="app-settings-card p-5 sm:p-6">
          <h3 className="font-semibold">
            {text("连接手机收件箱", "Connect your mobile inbox")}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {text(
              "填写服务地址与邀请码，激活后即可扫码绑定手机。",
              "Enter your service URL and invitation code, then pair your phone with a QR code.",
            )}
          </p>
          <form
            className="mt-5 max-w-xl space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              void perform(async () => {
                await window.api.mobileCapture.activate(
                  state.origin.trim(),
                  state.invite.trim(),
                );
                state.setInvite("");
              });
            }}
          >
            <label className="block space-y-2 text-sm">
              <span>{text("公共服务地址", "Service URL")}</span>
              <input
                required
                className="app-settings-input h-10 w-full rounded-lg px-3"
                type="url"
                placeholder="https://…"
                value={state.origin}
                onChange={(event) => state.setOrigin(event.target.value)}
              />
            </label>
            <label className="block space-y-2 text-sm">
              <span>{text("邀请码", "Invitation code")}</span>
              <input
                required
                type="password"
                autoComplete="off"
                className="app-settings-input h-10 w-full rounded-lg px-3"
                value={state.invite}
                onChange={(event) => state.setInvite(event.target.value)}
              />
            </label>
            <Button
              type="submit"
              disabled={busy || !state.origin.trim() || !state.invite.trim()}
            >
              <Inbox className="h-4 w-4" />
              {text("激活手机收集", "Activate mobile capture")}
            </Button>
          </form>
        </section>
      )}

      {settings?.connected && (
        <>
          <section className="app-settings-card overflow-hidden">
            <div className="flex flex-wrap items-center justify-between gap-4 p-5">
              <div className="flex items-center gap-3">
                <span className="rounded-xl bg-primary/10 p-3 text-primary">
                  <Inbox className="h-5 w-5" aria-hidden="true" />
                </span>
                <div>
                  <h3 className="font-semibold">
                    {text("手机收件箱", "Mobile inbox")}
                  </h3>
                  <p
                    role="status"
                    className={`mt-1 flex items-center gap-1.5 text-xs ${settings.error ? "text-destructive" : "text-muted-foreground"}`}
                  >
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${settings.error ? "bg-destructive" : settings.paused ? "bg-muted-foreground" : "bg-primary"}`}
                    />
                    {status}
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="secondary"
                  disabled={busy}
                  onClick={() =>
                    void perform(() =>
                      window.api.mobileCapture.configure(
                        !settings.paused,
                        settings.collectionId,
                      ),
                    )
                  }
                >
                  {settings.paused ? (
                    <Play className="h-4 w-4" />
                  ) : (
                    <Pause className="h-4 w-4" />
                  )}
                  {settings.paused
                    ? text("恢复取件", "Resume")
                    : text("暂停取件", "Pause")}
                </Button>
                <Button
                  disabled={busy || settings.paused}
                  onClick={() =>
                    void perform(async () => {
                      const result = await window.api.mobileCapture.fetch();
                      if (result.error) throw new Error(result.error);
                    })
                  }
                >
                  <ArrowDownToLine className="h-4 w-4" />
                  {text("立即取件", "Collect now")}
                </Button>
              </div>
            </div>
            {settings.error && (
              <p
                role="alert"
                className="mx-5 mb-4 break-words rounded-lg bg-destructive/10 p-3 text-sm text-destructive"
              >
                {settings.error}
              </p>
            )}
            <div className="grid gap-4 border-t border-border/70 bg-muted/20 px-5 py-4 sm:grid-cols-2">
              <div className="min-w-0">
                <p className="text-xs text-muted-foreground">
                  {text("收集服务", "Capture service")}
                </p>
                <p className="mt-1.5 break-all text-sm">{settings.origin}</p>
              </div>
              <div>
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Clock3 className="h-3.5 w-3.5" />
                  {text("上次取件", "Last received")}
                </p>
                <p className="mt-1.5 text-sm">
                  {settings.lastReceivedAt
                    ? new Date(settings.lastReceivedAt).toLocaleString(
                        i18n.language,
                      )
                    : text("尚未取回内容", "No content received yet")}
                </p>
              </div>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-4 border-t border-border/70 p-5">
              <div className="flex items-start gap-3">
                <FolderOpen className="mt-0.5 h-4 w-4 text-muted-foreground" />
                <div>
                  <p className="text-sm font-medium">
                    {text("默认目标知识库", "Default collection")}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {text(
                      "手机收集的内容默认保存到这里",
                      "New mobile captures are saved here by default",
                    )}
                  </p>
                </div>
              </div>
              <Select
                className="w-full sm:w-60"
                ariaLabel={text("默认目标知识库", "Default collection")}
                value={settings.collectionId ?? ""}
                disabled={busy}
                options={[
                  { value: "", label: text("未分类", "Unfiled") },
                  ...collections.map((collection) => ({
                    value: collection.id,
                    label: collection.name,
                  })),
                ]}
                onChange={(id) =>
                  void perform(() =>
                    window.api.mobileCapture.configure(
                      settings.paused,
                      id || null,
                    ),
                  )
                }
              />
            </div>
          </section>
          <MobileCapturePairing
            action={state.pairingAction}
            success={state.pairingSuccess}
            qr={state.qr}
            expires={state.expires}
            now={state.now}
            pairings={state.pairings}
            busy={busy}
            text={text}
            onGenerate={state.generatePairing}
            onConfirm={state.confirmPairing}
          />
          <section className="app-settings-card overflow-hidden">
            <div className="flex items-center justify-between gap-3 p-5">
              <div>
                <h3 className="font-semibold">
                  {text("已绑定设备", "Paired devices")}
                  <span className="ml-2 rounded-md bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                    {activeDevices.length}
                  </span>
                </h3>
                <p className="mt-1 text-xs text-muted-foreground">
                  {text(
                    "只有已确认的设备可以向此收件箱投递",
                    "Only confirmed devices can submit to this inbox",
                  )}
                </p>
              </div>
              <Smartphone
                className="h-5 w-5 text-muted-foreground"
                aria-hidden="true"
              />
            </div>
            {!activeDevices.length && (
              <div className="border-t border-dashed border-border px-5 py-7 text-center">
                <p className="text-sm">
                  {text("还没有绑定设备", "No paired devices yet")}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {text(
                    "生成上方二维码，用手机扫码并在电脑上确认绑定。",
                    "Generate a QR code above, scan with your phone, then confirm here.",
                  )}
                </p>
              </div>
            )}
            {activeDevices.map((device) => (
              <div
                key={device.id}
                className="flex items-center justify-between gap-4 border-t border-border/70 px-5 py-4"
              >
                <div className="flex min-w-0 items-center gap-3">
                  <span className="rounded-xl bg-muted p-2.5">
                    <Smartphone className="h-5 w-5 text-muted-foreground" />
                  </span>
                  <div className="min-w-0">
                    <p className="break-words text-sm font-medium">
                      {device.name}
                    </p>
                    <p className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                      <Check className="h-3 w-3" />
                      {device.kind === "shortcut"
                        ? text("快捷指令 · 已授权投递", "Shortcut · Authorized")
                        : text("手机 · 已授权投递", "Phone · Authorized")}
                    </p>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  className="shrink-0 text-muted-foreground hover:text-destructive"
                  aria-label={text(
                    `解除绑定 ${device.name}`,
                    `Unpair ${device.name}`,
                  )}
                  disabled={busy}
                  onClick={() => state.setDestructive(device.id)}
                >
                  {text("解除绑定", "Unpair")}
                </Button>
              </div>
            ))}
          </section>
        </>
      )}
      <div className="flex items-start gap-3 px-1 text-muted-foreground">
        <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" />
        <div className="space-y-1.5 text-xs leading-relaxed">
          <p className="font-medium text-foreground">
            {text(
              "暂存收集内容，知识库保留在本地",
              "Temporary delivery, local knowledge",
            )}
          </p>
          <p>
            {text(
              "公共服务可读取暂存内容，电脑确认接收后清除主库原文；此功能不会同步本地知识库。恢复备份后会自动暂停取件，请检查连接再恢复。",
              "The public service can read queued content and clears payloads from its main database after desktop acknowledgement. Your local library is not synced. Restoring a backup pauses collection; review the connection before resuming.",
            )}
          </p>
        </div>
      </div>
      {settings?.connected && (
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border/70 pt-4">
          <p className="text-xs text-muted-foreground">
            {text(
              "不再使用手机收集？停用后，所有设备将无法继续投递。",
              "Disabling this inbox stops submissions from all devices.",
            )}
          </p>
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground hover:text-destructive"
            disabled={busy}
            onClick={() => state.setDestructive("mailbox")}
          >
            <Unplug className="h-3.5 w-3.5" />
            {text("停用此收件箱", "Disable this inbox")}
          </Button>
        </div>
      )}
      <ConfirmDialog
        isOpen={!!state.destructive}
        onClose={() => {
          if (!busy) state.setDestructive(null);
        }}
        variant="destructive"
        title={
          state.destructive === "mailbox"
            ? text("停用此收件箱", "Disable this inbox")
            : text("解除设备绑定", "Unpair device")
        }
        message={
          state.destructive === "mailbox"
            ? text(
                "所有已绑定设备都将无法继续投递。重新启用需要新邀请码，并重新绑定手机。",
                "All paired devices will lose access. Reactivating requires a new invitation code and pairing your phones again.",
              )
            : text(
                "解除后，此设备将无法继续投递。需要使用时可以重新扫码绑定。",
                "This device will no longer be able to submit. You can pair it again using a new QR code.",
              )
        }
        confirmText={
          state.destructive === "mailbox"
            ? text("确认停用", "Disable inbox")
            : text("解除绑定", "Unpair")
        }
        cancelText={text("取消", "Cancel")}
        isLoading={busy}
        onConfirm={state.revokeConnection}
      />
    </div>
  );
}
