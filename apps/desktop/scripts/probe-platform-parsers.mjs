#!/usr/bin/env node
/**
 * 平台解析线上探针（默认不进 CI）。
 *
 * 发版前或用户报「抖音/小红书采不了」时手动跑：拉真实页面，检查 SSR marker
 * 是否仍在、能否抠出作品 id。不替代单测契约夹具，也不声称能挡住改版——
 * 只是尽早发现「线上已经换皮」。
 *
 * 用法（PowerShell）：
 *   $env:GUIZHI_PROBE_DOUYIN_URL="https://www.iesdouyin.com/share/video/<id>/"
 *   $env:GUIZHI_PROBE_XHS_URL="https://www.xiaohongshu.com/explore/<id>?xsec_token=..."
 *   node apps/desktop/scripts/probe-platform-parsers.mjs
 *
 * 任一 URL 未设置则跳过该平台；一个都没设则打印用法并以 2 退出。
 */

const DOUYIN_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
const XHS_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

const ROUTER_MARKER = "window._ROUTER_DATA";
const STATE_MARKER = "window.__INITIAL_STATE__";

/**
 * @param {string} url
 * @param {string} userAgent
 */
async function fetchText(url, userAgent) {
  const response = await fetch(url, {
    redirect: "follow",
    headers: {
      "User-Agent": userAgent,
      Accept: "text/html,application/xhtml+xml",
    },
  });
  const text = await response.text();
  return { status: response.status, finalUrl: response.url, text };
}

/**
 * @param {string} html
 * @param {string} marker
 */
function sliceObjectAfterMarker(html, marker) {
  const at = html.indexOf(marker);
  if (at < 0) {
    return null;
  }
  const start = html.indexOf("{", at + marker.length);
  if (start < 0) {
    return null;
  }
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let i = start; i < html.length; i++) {
    const char = html[i];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === '"') {
        inString = false;
      }
      continue;
    }
    if (char === '"') {
      inString = true;
    } else if (char === "{") {
      depth++;
    } else if (char === "}" && --depth === 0) {
      return html.slice(start, i + 1);
    }
  }
  return null;
}

/**
 * @param {string} label
 * @param {string} code
 * @param {string} detail
 */
function fail(label, code, detail) {
  console.error(`[FAIL] ${label} code=${code} ${detail}`);
  return false;
}

/**
 * @param {string} label
 * @param {string} detail
 */
function ok(label, detail) {
  console.log(`[OK] ${label} ${detail}`);
  return true;
}

/**
 * @param {string} url
 */
async function probeDouyin(url) {
  const label = "douyin";
  try {
    const page = await fetchText(url, DOUYIN_UA);
    if (!page.text.includes(ROUTER_MARKER)) {
      return fail(
        label,
        "structure_missing",
        `marker 缺失 status=${page.status} htmlLength=${page.text.length} final=${page.finalUrl}`,
      );
    }
    const json = sliceObjectAfterMarker(page.text, ROUTER_MARKER);
    if (!json) {
      return fail(label, "structure_missing", "marker 在但对象切不出来");
    }
    let payload;
    try {
      payload = JSON.parse(json);
    } catch {
      return fail(label, "structure_missing", "marker 对象不是合法 JSON");
    }
    const info = Object.values(payload.loaderData ?? {})
      .map((pageData) => pageData?.videoInfoRes)
      .find(Boolean);
    const item = info?.item_list?.[0];
    if (!item) {
      const reason =
        info?.filter_list?.[0]?.detail_msg ||
        info?.filter_list?.[0]?.notice ||
        "无 item_list";
      return fail(label, "note_unavailable", String(reason));
    }
    const id = item.aweme_id || "(unknown)";
    const title = String(item.desc ?? "")
      .split(/\r?\n/)
      .find((line) => line.trim())
      ?.trim()
      .slice(0, 40);
    return ok(label, `awemeId=${id} title=${JSON.stringify(title ?? "")}`);
  } catch (error) {
    return fail(
      label,
      "network",
      error instanceof Error ? error.message : String(error),
    );
  }
}

/**
 * @param {string} url
 */
async function probeXiaohongshu(url) {
  const label = "xiaohongshu";
  try {
    const page = await fetchText(url, XHS_UA);
    let finalPath = "/";
    try {
      finalPath = new URL(page.finalUrl).pathname;
    } catch {
      /* ignore */
    }
    if (finalPath.startsWith("/404")) {
      return fail(
        label,
        "token_invalid",
        `落地 /404 final=${page.finalUrl}`,
      );
    }
    if (!page.text.includes(STATE_MARKER)) {
      return fail(
        label,
        "structure_missing",
        `marker 缺失 status=${page.status} htmlLength=${page.text.length} final=${page.finalUrl}`,
      );
    }
    const raw = sliceObjectAfterMarker(page.text, STATE_MARKER);
    if (!raw) {
      return fail(label, "structure_missing", "marker 在但对象切不出来");
    }
    // 与生产解析一致：只把字符串外的 undefined 换成 null（探针简化为全局替换足够冒烟）
    const jsonText = raw.replace(/\bundefined\b/g, "null");
    let state;
    try {
      state = JSON.parse(jsonText);
    } catch {
      return fail(label, "structure_missing", "marker 对象不是合法 JSON");
    }
    const detailMap = state?.note?.noteDetailMap ?? {};
    const noteId =
      state?.note?.firstNoteId || Object.keys(detailMap)[0] || null;
    const note = noteId ? detailMap[noteId]?.note : null;
    if (!noteId || !note) {
      return fail(label, "note_unavailable", "noteDetailMap 无笔记");
    }
    const title = String(note.title || note.desc || "").slice(0, 40);
    return ok(label, `noteId=${noteId} title=${JSON.stringify(title)}`);
  } catch (error) {
    return fail(
      label,
      "network",
      error instanceof Error ? error.message : String(error),
    );
  }
}

async function main() {
  const douyinUrl = process.env.GUIZHI_PROBE_DOUYIN_URL?.trim();
  const xhsUrl = process.env.GUIZHI_PROBE_XHS_URL?.trim();
  if (!douyinUrl && !xhsUrl) {
    console.error(
      "请设置 GUIZHI_PROBE_DOUYIN_URL 和/或 GUIZHI_PROBE_XHS_URL 后重试。\n" +
        "本脚本依赖外网，默认不进 CI。",
    );
    process.exit(2);
  }

  const results = [];
  if (douyinUrl) {
    results.push(await probeDouyin(douyinUrl));
  }
  if (xhsUrl) {
    results.push(await probeXiaohongshu(xhsUrl));
  }
  process.exit(results.every(Boolean) ? 0 : 1);
}

main();
