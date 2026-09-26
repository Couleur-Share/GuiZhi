import { spawn } from "node:child_process";
import process from "node:process";

type Profile = "quick" | "release";

type Check = {
  id: string;
  label: string;
  args: string[];
  profile: Profile;
};

// 只列出当前仓库实际存在的入口；不能把未匹配的 filter 当成通过。
const checks: Check[] = [
  { id: "workspace-lint", label: "全仓文件门禁与桌面 ESLint", args: ["lint"], profile: "quick" },
  { id: "workspace-typecheck", label: "全部工作区类型检查", args: ["typecheck"], profile: "quick" },
  { id: "desktop-unit", label: "验证工具与桌面全量单测", args: ["test:unit"], profile: "quick" },
  { id: "desktop-build", label: "隔离生产构建与包体预算", args: ["build:isolated"], profile: "quick" },
  { id: "desktop-e2e-smoke", label: "隔离生产构建、包体预算与 Electron 冒烟", args: ["test:e2e:smoke"], profile: "release" },
];

function getProfile(): Profile {
  const profileArg = process.argv.find((arg) => arg.startsWith("--profile="));
  const profileFlagIndex = process.argv.indexOf("--profile");

  if (process.argv.includes("--quick")) {
    return "quick";
  }

  if (profileFlagIndex !== -1) {
    const profile = process.argv[profileFlagIndex + 1];

    if (profile === "quick" || profile === "release") {
      return profile;
    }

    throw new Error(`Unsupported profile: ${profile}`);
  }

  if (!profileArg) {
    return "release";
  }

  const profile = profileArg.split("=")[1];

  if (profile === "quick" || profile === "release") {
    return profile;
  }

  throw new Error(`Unsupported profile: ${profile}`);
}

function assertUniqueChecks(checksToValidate: Check[]): void {
  const seenIds = new Set<string>();
  const seenCommands = new Set<string>();

  for (const check of checksToValidate) {
    const command = `pnpm ${check.args.join(" ")}`;

    if (seenIds.has(check.id)) {
      throw new Error(`Duplicate release harness check id: ${check.id}`);
    }

    if (seenCommands.has(command)) {
      throw new Error(`Duplicate release harness command: ${command}`);
    }

    seenIds.add(check.id);
    seenCommands.add(command);
  }
}

function formatDuration(startedAt: number): string {
  return `${((Date.now() - startedAt) / 1000).toFixed(1)}s`;
}

function shouldRun(check: Check, profile: Profile): boolean {
  // 完整检查由冒烟入口构建一次，避免重复编译；两种模式都检查新产物预算。
  if (profile === "release" && check.id === "desktop-build") return false;
  return profile === "release" || check.profile === "quick";
}

function runCheck(check: Check, index: number, total: number): Promise<void> {
  const startedAt = Date.now();
  const command = `pnpm ${check.args.join(" ")}`;

  console.log(`\n[${index}/${total}] ${check.label}`);
  console.log(`$ ${command}`);

  return new Promise((resolve, reject) => {
    const child = spawn("pnpm", check.args, {
      stdio: "inherit",
      shell: process.platform === "win32",
    });

    child.on("error", reject);
    child.on("close", (code, signal) => {
      if (code === 0) {
        console.log(`[ok] ${check.id} (${formatDuration(startedAt)})`);
        resolve();
        return;
      }

      const reason = signal ? `signal ${signal}` : `exit code ${code}`;
      reject(new Error(`${check.id} failed with ${reason}`));
    });
  });
}

async function main(): Promise<void> {
  const profile = getProfile();
  const selectedChecks = checks.filter((check) => shouldRun(check, profile));

  assertUniqueChecks(checks);

  if (process.argv.includes("--list")) {
    for (const check of selectedChecks) {
      console.log(`${check.id}: pnpm ${check.args.join(" ")}`);
    }
    return;
  }

  const startedAt = Date.now();
  console.log(`GuiZhi release harness profile: ${profile}`);

  for (const [index, check] of selectedChecks.entries()) {
    await runCheck(check, index + 1, selectedChecks.length);
  }

  console.log(`\nRelease harness passed in ${formatDuration(startedAt)}.`);
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  console.error(`\nRelease harness failed: ${message}`);
  process.exitCode = 1;
});
