import { webTextRequest } from "./web-network";

interface Rule {
  allow: boolean;
  path: string;
}
interface Group {
  agents: string[];
  rules: Rule[];
}
export interface RobotsPolicy {
  rules: Rule[];
  sitemaps: string[];
}
export function parseRobots(text: string): RobotsPolicy {
  const groups: Group[] = [];
  const sitemaps: string[] = [];
  let group: Group | undefined;
  let hasDirective = false;
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.split("#")[0].trim(),
      colon = line.indexOf(":");
    if (colon < 0) continue;
    const key = line.slice(0, colon).trim().toLowerCase(),
      value = line.slice(colon + 1).trim();
    if (key === "sitemap") {
      if (sitemaps.length < 10) sitemaps.push(value);
      continue;
    }
    if (key === "user-agent") {
      if (!group || hasDirective) {
        group = { agents: [], rules: [] };
        groups.push(group);
        hasDirective = false;
      }
      group.agents.push(value.toLowerCase());
    } else if (group && ["allow", "disallow"].includes(key)) {
      hasDirective = true;
      if (value) group.rules.push({ allow: key === "allow", path: value });
    }
  }
  const specific = groups.filter((g) =>
    g.agents.some((a) => a !== "*" && "guizhi".includes(a)),
  );
  return {
    rules: (specific.length
      ? specific
      : groups.filter((g) => g.agents.includes("*"))
    ).flatMap((g) => g.rules),
    sitemaps,
  };
}
export function robotsAllows(url: string, policy: RobotsPolicy): boolean {
  const parsed = new URL(url),
    path = parsed.pathname + parsed.search;
  let best = -1,
    allowed = true;
  for (const rule of policy.rules) {
    const end = rule.path.endsWith("$"),
      value = end ? rule.path.slice(0, -1) : rule.path;
    const pattern = value
      .split("*")
      .map((p) => p.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
      .join(".*");
    if (!new RegExp("^" + pattern + (end ? "$" : "")).test(path)) continue;
    const length = value.replace(/\*/g, "").length;
    if (length > best || (length === best && rule.allow)) {
      best = length;
      allowed = rule.allow;
    }
  }
  return allowed;
}
export async function loadRobots(
  origin: string,
  signal: AbortSignal,
): Promise<RobotsPolicy> {
  const result = await webTextRequest(
    `${origin}/robots.txt`,
    signal,
    (url) => new URL(url).origin === origin,
  );
  if (result.status === 404 || result.status === 410)
    return { rules: [], sitemaps: [] };
  if (result.status < 200 || result.status >= 300)
    throw new Error(`robots.txt 读取失败（HTTP ${result.status}），已暂停来源`);
  if (result.text.length > 512000 || /^\s*</.test(result.text))
    throw new Error("robots.txt 不是有效规则文件，已暂停来源");
  return parseRobots(result.text);
}
