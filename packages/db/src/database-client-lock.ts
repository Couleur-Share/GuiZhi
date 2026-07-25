import fs from "fs";
import path from "path";

interface DatabaseClientLeaseRecord {
  pid: number;
  registeredAt: string;
}

export interface DatabaseClientLeaseOptions {
  pid?: number;
  isProcessAlive?: (pid: number) => boolean;
  registerExitHandler?: boolean;
  recoverUnregisteredLock?: boolean;
}

export interface DatabaseClientLease {
  release: () => void;
}

export type DatabaseLockRecoveryReason =
  | "live-client"
  | "unknown-client"
  | "unsafe-lock";

export interface DatabaseLockInspection {
  status: "absent" | "recoverable" | "blocked";
  recovered: false;
  lockPath: string;
  reason?: DatabaseLockRecoveryReason;
  livePids: number[];
  staleEntries: string[];
  unknownEntries: string[];
}

export interface DatabaseLockRecoveryResult extends Omit<
  DatabaseLockInspection,
  "status" | "recovered"
> {
  status: "absent" | "recovered" | "blocked";
  recovered: boolean;
}

interface ClientLeaseScan {
  hasLiveClient: boolean;
  hasRecoverableStaleLease: boolean;
  hasUnknownOwner: boolean;
}

interface ExitCleanupRegistry {
  releases: Set<() => void>;
  handleExit: () => void;
}

const EXIT_CLEANUP_REGISTRY_KEY = Symbol.for(
  "guizhi.database-client-lock.exit-cleanup",
);

function defaultIsProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code === "EPERM";
  }
}

function inspectClientLeaseDirectory(
  clientsDir: string,
  isProcessAlive: (pid: number) => boolean,
): Pick<
  DatabaseLockInspection,
  "livePids" | "staleEntries" | "unknownEntries"
> {
  if (!fs.existsSync(clientsDir)) {
    return { livePids: [], staleEntries: [], unknownEntries: [] };
  }
  const stat = fs.lstatSync(clientsDir);
  if (!stat.isDirectory() || stat.isSymbolicLink()) {
    return {
      livePids: [],
      staleEntries: [],
      unknownEntries: [path.basename(clientsDir)],
    };
  }

  const livePids: number[] = [];
  const staleEntries: string[] = [];
  const unknownEntries: string[] = [];
  for (const entry of fs.readdirSync(clientsDir, { withFileTypes: true })) {
    const filePath = path.join(clientsDir, entry.name);
    if (!entry.isFile() || entry.isSymbolicLink()) {
      unknownEntries.push(entry.name);
      continue;
    }
    const pid = readLeasePid(filePath);
    if (pid === null || entry.name !== `${pid}.json`) {
      staleEntries.push(entry.name);
      continue;
    }
    if (isProcessAlive(pid)) {
      livePids.push(pid);
    } else {
      staleEntries.push(entry.name);
    }
  }
  return { livePids, staleEntries, unknownEntries };
}

export function inspectDatabaseClientLock(
  dbPath: string,
  options: { isProcessAlive?: (pid: number) => boolean } = {},
): DatabaseLockInspection {
  const lockPath = `${dbPath}.lock`;
  const empty = { livePids: [], staleEntries: [], unknownEntries: [] };
  if (!fs.existsSync(lockPath)) {
    return { status: "absent", recovered: false, lockPath, ...empty };
  }
  const lockStat = fs.lstatSync(lockPath);
  if (!lockStat.isDirectory() || lockStat.isSymbolicLink()) {
    return {
      status: "blocked",
      recovered: false,
      lockPath,
      reason: "unsafe-lock",
      ...empty,
    };
  }

  const clients = inspectClientLeaseDirectory(
    `${dbPath}.clients`,
    options.isProcessAlive ?? defaultIsProcessAlive,
  );
  const reason =
    clients.livePids.length > 0
      ? "live-client"
      : clients.unknownEntries.length > 0
        ? "unknown-client"
        : undefined;
  return {
    status: reason ? "blocked" : "recoverable",
    recovered: false,
    lockPath,
    ...(reason && { reason }),
    ...clients,
  };
}

export function recoverDatabaseClientLock(
  dbPath: string,
  options: { isProcessAlive?: (pid: number) => boolean } = {},
): DatabaseLockRecoveryResult {
  const inspection = inspectDatabaseClientLock(dbPath, options);
  if (inspection.status === "absent") {
    return { ...inspection, status: "absent" };
  }
  if (inspection.status === "blocked") {
    return { ...inspection, status: "blocked" };
  }

  const lease = acquireDatabaseClientLease(dbPath, {
    isProcessAlive: options.isProcessAlive,
    recoverUnregisteredLock: true,
    registerExitHandler: false,
  });
  lease.release();
  const after = inspectDatabaseClientLock(dbPath, options);
  if (after.status === "absent") {
    return { ...after, status: "recovered", recovered: true };
  }
  return {
    ...after,
    status: "blocked",
    recovered: false,
    reason: after.reason ?? "unknown-client",
  };
}

function readLeasePid(filePath: string): number | null {
  try {
    const stat = fs.lstatSync(filePath);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      return null;
    }
    const parsed = JSON.parse(
      fs.readFileSync(filePath, "utf8"),
    ) as Partial<DatabaseClientLeaseRecord>;
    return Number.isInteger(parsed.pid) && Number(parsed.pid) > 0
      ? Number(parsed.pid)
      : null;
  } catch {
    return null;
  }
}

function removeLeaseFile(filePath: string): boolean {
  try {
    fs.rmSync(filePath, { force: true });
    return true;
  } catch {
    return false;
  }
}

function scanClientLeases(
  clientsDir: string,
  currentPid: number,
  isProcessAlive: (pid: number) => boolean,
): ClientLeaseScan {
  let hasLiveClient = false;
  let hasRecoverableStaleLease = false;
  let hasUnknownOwner = false;
  for (const entry of fs.readdirSync(clientsDir, { withFileTypes: true })) {
    const filePath = path.join(clientsDir, entry.name);
    const pid = entry.isFile() ? readLeasePid(filePath) : null;
    if (pid !== null && entry.name !== `${pid}.json`) {
      const removed = removeLeaseFile(filePath);
      hasRecoverableStaleLease ||= removed;
      hasUnknownOwner ||= !removed;
      continue;
    }
    if (pid === currentPid) {
      continue;
    }
    if (pid === null || !isProcessAlive(pid)) {
      const removed = removeLeaseFile(filePath);
      hasRecoverableStaleLease ||= removed;
      hasUnknownOwner ||= !removed;
      continue;
    }
    hasLiveClient = true;
  }
  return { hasLiveClient, hasRecoverableStaleLease, hasUnknownOwner };
}

function writeCurrentLease(clientsDir: string, pid: number): string {
  const leasePath = path.join(clientsDir, `${pid}.json`);
  const record: DatabaseClientLeaseRecord = {
    pid,
    registeredAt: new Date().toISOString(),
  };
  removeLeaseFile(leasePath);
  fs.writeFileSync(leasePath, JSON.stringify(record), {
    encoding: "utf8",
    flag: "wx",
    mode: 0o600,
  });
  return leasePath;
}

function removeOrphanedLock(
  dbPath: string,
  scan: ClientLeaseScan,
  recoverUnregisteredLock: boolean,
): void {
  const lockDir = `${dbPath}.lock`;
  const canRecover =
    (scan.hasRecoverableStaleLease || recoverUnregisteredLock) &&
    !scan.hasLiveClient &&
    !scan.hasUnknownOwner;
  if (!canRecover || !fs.existsSync(lockDir)) {
    return;
  }
  const lockStat = fs.lstatSync(lockDir);
  if (!lockStat.isDirectory() || lockStat.isSymbolicLink()) {
    return;
  }
  fs.rmSync(lockDir, { recursive: true, force: true });
  console.log(`[DB] Recovered orphaned lock: ${lockDir}`);
}

function getExitCleanupRegistry(): ExitCleanupRegistry {
  const scope = globalThis as typeof globalThis & {
    [EXIT_CLEANUP_REGISTRY_KEY]?: ExitCleanupRegistry;
  };
  if (!scope[EXIT_CLEANUP_REGISTRY_KEY]) {
    const releases = new Set<() => void>();
    scope[EXIT_CLEANUP_REGISTRY_KEY] = {
      releases,
      handleExit: () => {
        for (const release of [...releases]) {
          release();
        }
      },
    };
  }
  return scope[EXIT_CLEANUP_REGISTRY_KEY];
}

function registerExitCleanup(release: () => void): () => void {
  const registry = getExitCleanupRegistry();
  if (registry.releases.size === 0) {
    process.once("exit", registry.handleExit);
  }
  registry.releases.add(release);
  return () => {
    registry.releases.delete(release);
    if (registry.releases.size === 0) {
      process.off("exit", registry.handleExit);
    }
  };
}

function removeLeaseRegistration(leasePath: string, clientsDir: string): void {
  removeLeaseFile(leasePath);
  try {
    fs.rmdirSync(clientsDir);
  } catch {
    // Other clients or stale entries still own the directory.
  }
}

export function acquireDatabaseClientLease(
  dbPath: string,
  options: DatabaseClientLeaseOptions = {},
): DatabaseClientLease {
  const pid = options.pid ?? process.pid;
  const isProcessAlive = options.isProcessAlive ?? defaultIsProcessAlive;
  const clientsDir = `${dbPath}.clients`;
  fs.mkdirSync(clientsDir, { recursive: true, mode: 0o700 });
  const clientsDirStat = fs.lstatSync(clientsDir);
  if (!clientsDirStat.isDirectory() || clientsDirStat.isSymbolicLink()) {
    throw new Error(`Invalid database clients directory: ${clientsDir}`);
  }
  const leasePath = writeCurrentLease(clientsDir, pid);
  try {
    const clientScan = scanClientLeases(clientsDir, pid, isProcessAlive);
    removeOrphanedLock(
      dbPath,
      clientScan,
      options.recoverUnregisteredLock === true,
    );
  } catch (error) {
    removeLeaseRegistration(leasePath, clientsDir);
    throw error;
  }

  let released = false;
  let unregisterExitCleanup: () => void = () => undefined;
  const release = () => {
    if (released) {
      return;
    }
    released = true;
    unregisterExitCleanup();
    removeLeaseRegistration(leasePath, clientsDir);
  };

  if (options.registerExitHandler !== false) {
    unregisterExitCleanup = registerExitCleanup(release);
  }

  return { release };
}
