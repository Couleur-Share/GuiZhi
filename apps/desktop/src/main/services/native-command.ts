import { execFile } from "node:child_process";

export interface NativeCommandRunOptions {
  timeout: number;
  maxBuffer: number;
}

export interface NativeCommandRunner {
  resolve(command: string): Promise<string | null>;
  run(
    command: string,
    args: string[],
    options: NativeCommandRunOptions,
  ): Promise<{ stdout: string; stderr: string }>;
}

function execFileAsync(
  command: string,
  args: string[],
  options: NativeCommandRunOptions,
): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      command,
      args,
      {
        env: process.env,
        timeout: options.timeout,
        maxBuffer: options.maxBuffer,
        windowsHide: true,
      },
      (error, stdout, stderr) => {
        if (error) {
          reject(error);
          return;
        }
        resolve({
          stdout,
          stderr,
        });
      },
    );
  });
}

async function resolveUnixCommand(command: string): Promise<string | null> {
  const shell = process.env.SHELL || "/bin/zsh";
  try {
    const result = await execFileAsync(
      shell,
      ["-lc", `command -v -- ${command}`],
      {
        timeout: 10_000,
        maxBuffer: 64 * 1024,
      },
    );
    const resolved = result.stdout.trim();
    return resolved.startsWith("/") ? resolved : null;
  } catch {
    return null;
  }
}

async function resolveWindowsCommand(command: string): Promise<string | null> {
  try {
    const result = await execFileAsync("where.exe", [command], {
      timeout: 10_000,
      maxBuffer: 64 * 1024,
    });
    return (
      result.stdout
        .split(/\r?\n/)
        .map((item) => item.trim().replace(/^"|"$/g, ""))
        .find(Boolean) || null
    );
  } catch {
    return null;
  }
}

export function createNativeCommandRunner(): NativeCommandRunner {
  return {
    async resolve(command) {
      if (!/^[a-z0-9_-]+$/i.test(command)) return null;
      try {
        await execFileAsync(command, ["--version"], {
          timeout: 10_000,
          maxBuffer: 64 * 1024,
        });
        return command;
      } catch {
        return process.platform === "win32"
          ? resolveWindowsCommand(command)
          : resolveUnixCommand(command);
      }
    },
    run: execFileAsync,
  };
}
