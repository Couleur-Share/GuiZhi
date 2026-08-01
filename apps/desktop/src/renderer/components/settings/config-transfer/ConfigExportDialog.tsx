import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyRoundIcon } from "lucide-react";
import { Modal } from "../../ui/Modal";
import { PasswordInput, ToggleSwitch } from "../shared";

/** 密码保护的是 API Key。太短的口令用 scrypt 也拦不住离线爆破 */
const MIN_PASSWORD_LENGTH = 6;

const GHOST_BUTTON =
  "inline-flex h-9 items-center gap-1.5 rounded-lg border border-border/70 px-3 text-sm text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary";
const PRIMARY_BUTTON =
  "inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-60";

export function ConfigExportDialog({
  isOpen,
  isBusy,
  onClose,
  onExport,
}: {
  isOpen: boolean;
  isBusy: boolean;
  onClose: () => void;
  onExport: (options: {
    includeSecrets: boolean;
    password: string;
    includeUiLayout: boolean;
    includeIllustrationStyles: boolean;
    includeShortcuts: boolean;
    includeMcpScope: boolean;
  }) => void;
}) {
  const { t } = useTranslation();
  const [includeSecrets, setIncludeSecrets] = useState(true);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [includeUiLayout, setIncludeUiLayout] = useState(true);
  const [includeIllustrationStyles, setIncludeIllustrationStyles] = useState(true);
  const [includeShortcuts, setIncludeShortcuts] = useState(true);
  const [includeMcpScope, setIncludeMcpScope] = useState(true);

  useEffect(() => {
    if (isOpen) {
      setIncludeSecrets(true);
      setPassword("");
      setConfirmPassword("");
      setIncludeUiLayout(true);
      setIncludeIllustrationStyles(true);
      setIncludeShortcuts(true);
      setIncludeMcpScope(true);
    }
  }, [isOpen]);

  const tooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const mismatched = confirmPassword.length > 0 && password !== confirmPassword;
  const canExport =
    !includeSecrets ||
    (password.length >= MIN_PASSWORD_LENGTH && password === confirmPassword);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("settings.configExportTitle", "导出配置")}
      subtitle={t(
        "settings.configExportSubtitle",
        "把当前设备的全部软件设置写成一个 JSON 文件，在新设备上导入即可。",
      )}
      size="lg"
      contentClassName="flex min-h-0 flex-col"
    >
      <div className="space-y-4 px-6 py-4">
        <div className="flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5 text-sm font-medium">
              <KeyRoundIcon className="h-4 w-4 text-primary" aria-hidden="true" />
              {t("settings.configExportIncludeKeys", "包含 API Key")}
            </div>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {t(
                "settings.configExportIncludeKeysDesc",
                "关闭则只导出模型与服务商的地址、名称和路由，密钥留空，导入后手填。",
              )}
            </p>
          </div>
          <ToggleSwitch
            checked={includeSecrets}
            onChange={setIncludeSecrets}
            ariaLabel={t("settings.configExportIncludeKeys", "包含 API Key")}
          />
        </div>

        {includeSecrets && (
          <div className="space-y-3">
            <p className="text-xs leading-relaxed text-muted-foreground">
              {t(
                "settings.configExportPasswordHint",
                "密钥会用这个密码加密后写进文件，文件其余部分仍是可读的明文。密码只在导入时用得上，忘了就只能手填密钥——归知不保存它，也找不回来。",
              )}
            </p>
            <PasswordInput
              value={password}
              onChange={setPassword}
              placeholder={t(
                "settings.configExportPasswordPlaceholder",
                "至少 {{count}} 位",
                { count: MIN_PASSWORD_LENGTH },
              )}
              ariaLabel={t("settings.configExportPassword", "加密密码")}
            />
            <PasswordInput
              value={confirmPassword}
              onChange={setConfirmPassword}
              placeholder={t(
                "settings.configExportPasswordConfirm",
                "再输一次",
              )}
              ariaLabel={t(
                "settings.configExportPasswordConfirm",
                "再输一次",
              )}
            />
            {tooShort && (
              <p className="text-xs text-destructive">
                {t("settings.configExportPasswordTooShort", "密码至少 {{count}} 位", {
                  count: MIN_PASSWORD_LENGTH,
                })}
              </p>
            )}
            {mismatched && (
              <p className="text-xs text-destructive">
                {t("settings.configExportPasswordMismatch", "两次输入不一致")}
              </p>
            )}
          </div>
        )}

        <div className="space-y-2 rounded-lg border border-border/60 px-4 py-3">
          <p className="text-sm font-medium text-foreground">
            {t("settings.configExportOptional", "选择要同步的偏好")}
          </p>
          {[
            [includeUiLayout, setIncludeUiLayout, t("settings.configExportLayout", "界面布局与视图偏好")],
            [includeIllustrationStyles, setIncludeIllustrationStyles, t("settings.configExportStyles", "正文配图风格")],
            [includeShortcuts, setIncludeShortcuts, t("settings.configExportShortcuts", "快捷键")],
            [includeMcpScope, setIncludeMcpScope, t("settings.configExportMcpScope", "MCP 访问范围")],
          ].map(([checked, onChange, label]) => (
            <div key={String(label)} className="flex items-center justify-between gap-3 py-1">
              <span className="text-xs text-muted-foreground">{String(label)}</span>
              <ToggleSwitch
                checked={Boolean(checked)}
                onChange={onChange as (next: boolean) => void}
                ariaLabel={String(label)}
              />
            </div>
          ))}
        </div>

        <p className="text-xs leading-relaxed text-muted-foreground">
          {t(
            "settings.configExportScopeNote",
            "不会带走的：数据目录、yt-dlp 与 ffmpeg 的路径、开机自启、背景图片、本地转写引擎——它们绑定在这台设备上。知识条目本身走「本地备份」，与配置分开。",
          )}
        </p>
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-6 py-3">
        <button type="button" onClick={onClose} className={GHOST_BUTTON}>
          {t("common.cancel", "取消")}
        </button>
        <button
          type="button"
          disabled={!canExport || isBusy}
          onClick={() => onExport({
            includeSecrets,
            password,
            includeUiLayout,
            includeIllustrationStyles,
            includeShortcuts,
            includeMcpScope,
          })}
          className={PRIMARY_BUTTON}
          data-testid="config-export-confirm"
        >
          {t("settings.configExportConfirm", "选择位置并导出")}
        </button>
      </div>
    </Modal>
  );
}
