import type {
  SettingsActionContext,
  SettingsActionGroup,
} from "./settings-action-context";
import {
  buildMainProcessSyncSettings,
  clampSyncProvider,
} from "./settings-normalizers";

type SyncActionKey =
  | "setWebdavEnabled"
  | "setWebdavUrl"
  | "setWebdavUsername"
  | "setWebdavPassword"
  | "setWebdavAutoSync"
  | "setWebdavSyncOnStartup"
  | "setWebdavSyncOnStartupDelay"
  | "setWebdavAutoSyncInterval"
  | "setWebdavSyncOnSave"
  | "setWebdavIncludeImages"
  | "setWebdavIncrementalSync"
  | "setWebdavEncryptionEnabled"
  | "setWebdavEncryptionPassword"
  | "setSelfHostedSyncEnabled"
  | "setSelfHostedSyncUrl"
  | "setSelfHostedSyncUsername"
  | "setSelfHostedSyncPassword"
  | "setSelfHostedSyncOnStartup"
  | "setSelfHostedSyncOnStartupDelay"
  | "setSelfHostedAutoSyncInterval"
  | "setS3StorageEnabled"
  | "setS3Endpoint"
  | "setS3Region"
  | "setS3Bucket"
  | "setS3AccessKeyId"
  | "setS3SecretAccessKey"
  | "setS3BackupPrefix"
  | "setS3SyncOnStartup"
  | "setS3SyncOnStartupDelay"
  | "setS3AutoSyncInterval"
  | "setS3SyncOnSave"
  | "setS3IncludeImages"
  | "setS3IncrementalSync"
  | "setS3EncryptionEnabled"
  | "setS3EncryptionPassword"
  | "setSyncProvider";

function syncProviderToMain(
  context: SettingsActionContext,
  provider: ReturnType<typeof clampSyncProvider>,
): void {
  void context.syncSettingsToMain({
    sync: buildMainProcessSyncSettings(provider),
  });
}

function createWebdavAvailabilityActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setWebdavEnabled: (enabled) => {
      const current = get();
      const syncProvider = enabled
        ? current.syncProvider
        : clampSyncProvider(current.syncProvider, {
            ...current,
            webdavEnabled: enabled,
          });
      setTouched({ webdavEnabled: enabled, syncProvider });
      syncProviderToMain(context, syncProvider);
    },
  } satisfies SettingsActionGroup<"setWebdavEnabled">;
}

function createWebdavConfigurationActions(context: SettingsActionContext) {
  const { setTouched } = context;
  return {
    setWebdavUrl: (webdavUrl) => setTouched({ webdavUrl }),
    setWebdavUsername: (webdavUsername) => setTouched({ webdavUsername }),
    setWebdavPassword: (webdavPassword) => setTouched({ webdavPassword }),
    setWebdavAutoSync: (enabled) =>
      setTouched({ webdavAutoSync: enabled, webdavSyncOnStartup: enabled }),
    setWebdavSyncOnStartup: (webdavSyncOnStartup) =>
      setTouched({ webdavSyncOnStartup }),
    setWebdavSyncOnStartupDelay: (delay) =>
      setTouched({
        webdavSyncOnStartupDelay: Math.max(0, Math.min(60, delay)),
      }),
    setWebdavAutoSyncInterval: (interval) =>
      setTouched({ webdavAutoSyncInterval: Math.max(0, interval) }),
    setWebdavSyncOnSave: (webdavSyncOnSave) => setTouched({ webdavSyncOnSave }),
    setWebdavIncludeImages: (webdavIncludeImages) =>
      setTouched({ webdavIncludeImages }),
    setWebdavIncrementalSync: (webdavIncrementalSync) =>
      setTouched({ webdavIncrementalSync }),
    setWebdavEncryptionEnabled: (webdavEncryptionEnabled) =>
      setTouched({ webdavEncryptionEnabled }),
    setWebdavEncryptionPassword: (webdavEncryptionPassword) =>
      setTouched({ webdavEncryptionPassword }),
  } satisfies SettingsActionGroup<
    | "setWebdavUrl"
    | "setWebdavUsername"
    | "setWebdavPassword"
    | "setWebdavAutoSync"
    | "setWebdavSyncOnStartup"
    | "setWebdavSyncOnStartupDelay"
    | "setWebdavAutoSyncInterval"
    | "setWebdavSyncOnSave"
    | "setWebdavIncludeImages"
    | "setWebdavIncrementalSync"
    | "setWebdavEncryptionEnabled"
    | "setWebdavEncryptionPassword"
  >;
}

function createSelfHostedAvailabilityActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setSelfHostedSyncEnabled: (enabled) => {
      const current = get();
      const syncProvider = enabled
        ? current.syncProvider
        : clampSyncProvider(current.syncProvider, {
            ...current,
            selfHostedSyncEnabled: enabled,
          });
      setTouched({ selfHostedSyncEnabled: enabled, syncProvider });
      syncProviderToMain(context, syncProvider);
    },
  } satisfies SettingsActionGroup<"setSelfHostedSyncEnabled">;
}

function createSelfHostedConfigurationActions(context: SettingsActionContext) {
  const { setTouched } = context;
  return {
    setSelfHostedSyncUrl: (selfHostedSyncUrl) =>
      setTouched({ selfHostedSyncUrl }),
    setSelfHostedSyncUsername: (selfHostedSyncUsername) =>
      setTouched({ selfHostedSyncUsername }),
    setSelfHostedSyncPassword: (selfHostedSyncPassword) =>
      setTouched({ selfHostedSyncPassword }),
    setSelfHostedSyncOnStartup: (selfHostedSyncOnStartup) =>
      setTouched({ selfHostedSyncOnStartup }),
    setSelfHostedSyncOnStartupDelay: (delay) =>
      setTouched({
        selfHostedSyncOnStartupDelay: Math.max(0, Math.min(60, delay)),
      }),
    setSelfHostedAutoSyncInterval: (interval) =>
      setTouched({ selfHostedAutoSyncInterval: Math.max(0, interval) }),
  } satisfies SettingsActionGroup<
    | "setSelfHostedSyncUrl"
    | "setSelfHostedSyncUsername"
    | "setSelfHostedSyncPassword"
    | "setSelfHostedSyncOnStartup"
    | "setSelfHostedSyncOnStartupDelay"
    | "setSelfHostedAutoSyncInterval"
  >;
}

function createS3AvailabilityActions(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setS3StorageEnabled: (enabled) => {
      const current = get();
      const syncProvider = enabled
        ? current.syncProvider
        : clampSyncProvider(current.syncProvider, {
            ...current,
            s3StorageEnabled: enabled,
          });
      setTouched({ s3StorageEnabled: enabled, syncProvider });
      syncProviderToMain(context, syncProvider);
    },
  } satisfies SettingsActionGroup<"setS3StorageEnabled">;
}

function createS3ConfigurationActions(context: SettingsActionContext) {
  const { setTouched } = context;
  return {
    setS3Endpoint: (s3Endpoint) => setTouched({ s3Endpoint }),
    setS3Region: (s3Region) => setTouched({ s3Region }),
    setS3Bucket: (s3Bucket) => setTouched({ s3Bucket }),
    setS3AccessKeyId: (s3AccessKeyId) => setTouched({ s3AccessKeyId }),
    setS3SecretAccessKey: (s3SecretAccessKey) =>
      setTouched({ s3SecretAccessKey }),
    setS3BackupPrefix: (s3BackupPrefix) => setTouched({ s3BackupPrefix }),
    setS3SyncOnStartup: (s3SyncOnStartup) => setTouched({ s3SyncOnStartup }),
    setS3SyncOnStartupDelay: (delay) =>
      setTouched({
        s3SyncOnStartupDelay: Math.max(0, Math.min(60, delay)),
      }),
    setS3AutoSyncInterval: (interval) =>
      setTouched({ s3AutoSyncInterval: Math.max(0, interval) }),
    setS3SyncOnSave: (s3SyncOnSave) => setTouched({ s3SyncOnSave }),
    setS3IncludeImages: (s3IncludeImages) => setTouched({ s3IncludeImages }),
    setS3IncrementalSync: (s3IncrementalSync) =>
      setTouched({ s3IncrementalSync }),
    setS3EncryptionEnabled: (s3EncryptionEnabled) =>
      setTouched({ s3EncryptionEnabled }),
    setS3EncryptionPassword: (s3EncryptionPassword) =>
      setTouched({ s3EncryptionPassword }),
  } satisfies SettingsActionGroup<
    | "setS3Endpoint"
    | "setS3Region"
    | "setS3Bucket"
    | "setS3AccessKeyId"
    | "setS3SecretAccessKey"
    | "setS3BackupPrefix"
    | "setS3SyncOnStartup"
    | "setS3SyncOnStartupDelay"
    | "setS3AutoSyncInterval"
    | "setS3SyncOnSave"
    | "setS3IncludeImages"
    | "setS3IncrementalSync"
    | "setS3EncryptionEnabled"
    | "setS3EncryptionPassword"
  >;
}

function createSyncProviderAction(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    setSyncProvider: (provider) => {
      const syncProvider = clampSyncProvider(provider, get());
      setTouched({ syncProvider });
      syncProviderToMain(context, syncProvider);
    },
  } satisfies SettingsActionGroup<"setSyncProvider">;
}

export function createSyncSettingsActions(
  context: SettingsActionContext,
): SettingsActionGroup<SyncActionKey> {
  return Object.assign(
    {},
    createWebdavAvailabilityActions(context),
    createWebdavConfigurationActions(context),
    createSelfHostedAvailabilityActions(context),
    createSelfHostedConfigurationActions(context),
    createS3AvailabilityActions(context),
    createS3ConfigurationActions(context),
    createSyncProviderAction(context),
  );
}
