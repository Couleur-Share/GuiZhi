export interface FlareSolverrSettings {
  enabled: boolean;
  connection: "local" | "ssh";
  port: number;
  sshHost: string;
}

export const DEFAULT_FLARESOLVERR_SETTINGS: FlareSolverrSettings = {
  enabled: false,
  connection: "local",
  port: 8191,
  sshHost: "",
};

/** 不接受命令、口令或任意 URL；SSH 使用用户已有的主机别名和密钥。 */
export function normalizeFlareSolverrSettings(
  value: unknown,
): FlareSolverrSettings {
  const input = (
    value && typeof value === "object" ? value : {}
  ) as Partial<FlareSolverrSettings>;
  return {
    enabled: input.enabled === true,
    connection: input.connection === "ssh" ? "ssh" : "local",
    port:
      typeof input.port === "number" &&
      Number.isInteger(input.port) &&
      input.port > 0 &&
      input.port <= 65535
        ? input.port
        : 8191,
    sshHost:
      typeof input.sshHost === "string" &&
      /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}$/.test(input.sshHost.trim())
        ? input.sshHost.trim()
        : "",
  };
}

export function validateFlareSolverrSettings(
  value: unknown,
): FlareSolverrSettings {
  const result = normalizeFlareSolverrSettings(value);
  const input = value as Partial<FlareSolverrSettings>;
  if (
    !input ||
    typeof input !== "object" ||
    typeof input.enabled !== "boolean" ||
    typeof input.connection !== "string" ||
    !["local", "ssh"].includes(input.connection) ||
    input.port !== result.port ||
    input.sshHost !== result.sshHost
  )
    throw new Error("FlareSolverr 设置无效，请填写有效端口和 SSH 主机别名");
  if (result.enabled && result.connection === "ssh" && !result.sshHost)
    throw new Error("请填写已有 SSH 配置中的主机别名");
  return result;
}
