export interface StartupDevToolsOptions {
  isDev: boolean;
  isE2E: boolean;
  env?: Record<string, string | undefined>;
}

function isTruthyEnv(value: string | undefined): boolean {
  if (!value) {
    return false;
  }
  return value === "1" || value.toLowerCase() === "true";
}

export function shouldOpenStartupDevTools({
  isDev,
  isE2E,
  env = process.env,
}: StartupDevToolsOptions): boolean {
  if (!isDev || isE2E) {
    return false;
  }

  return (
    isTruthyEnv(env.GUIZHI_OPEN_DEVTOOLS) ||
    isTruthyEnv(env.ELECTRON_OPEN_DEVTOOLS)
  );
}
