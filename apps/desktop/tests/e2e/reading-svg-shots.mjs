import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

export default async ({ win, shot, outDir }) => {
  const directory = path.resolve(process.env.GUIZHI_SVG_FIXTURES ?? "../../artifacts/reading-svg");
  const origin = new URL("/reading-svg/", win.url()).href;
  const errors = [], requests = [], cases = [];
  let html = "";
  await win.goto("about:blank");
  await win.setContent('<iframe sandbox="allow-scripts" style="position:fixed;inset:0;width:100%;height:100%;border:0"></iframe>');
  win.on("pageerror", error => errors.push(error.message));
  await win.context().route("**/*", async route => {
    if (route.request().url().startsWith(origin)) await route.fulfill({ contentType: "text/html", body: html });
    else { requests.push(route.request().url()); await route.abort(); }
  });
  for (const kind of ["offline", "embedded"]) for (const theme of ["light", "dark"]) for (const width of [1440, 360]) {
    await win.setViewportSize({ width, height: 1100 });
    await win.emulateMedia({ colorScheme: theme });
    html = await fs.readFile(path.join(directory, `${kind}.html`), "utf8");
    const url = `${origin}${kind}-${theme}-${width}.html`;
    await win.locator("iframe").evaluate((el, url) => { el.src = url; }, url);
    const frame = await (await win.locator("iframe").elementHandle()).contentFrame();
    await frame.waitForURL(url);
    await frame.locator("h1").waitFor();
    if (kind === "embedded") await win.evaluate(theme => {
      document.querySelector("iframe").contentWindow.postMessage({ id: "svg-fixture", type: "appearance", value: { theme } }, "*");
    }, theme);
    await frame.waitForFunction(theme => document.documentElement.dataset.theme === theme, theme);
    const metrics = await frame.evaluate(() => {
      const svg = [...document.querySelectorAll("svg")];
      const labels = [...document.querySelectorAll("svg text,svg tspan")].filter(el => !el.querySelector("tspan"));
      const brokenLabels = labels.filter(el => {
        const box = el.getBoundingClientRect(), bounds = el.ownerSVGElement.getBoundingClientRect();
        return box.width <= 0 || box.height < 13 || box.left < bounds.left - 1 || box.right > bounds.right + 1 || box.top < bounds.top - 1 || box.bottom > bounds.bottom + 1;
      }).map(el => el.textContent);
      return {
        overflow: document.documentElement.scrollWidth > innerWidth + 2,
        svg: svg.length, brokenLabels,
        gradient: document.getElementById("flow-wash") instanceof SVGLinearGradientElement,
        radial: document.getElementById("relation-wash") instanceof SVGRadialGradientElement,
        marker: document.getElementById("flow-arrow") instanceof SVGMarkerElement,
        clip: document.getElementById("compare-clip") instanceof SVGClipPathElement,
        transform: document.querySelector("svg g[transform]").transform.baseVal.getItem(0).matrix.e,
        markerStyle: getComputedStyle(document.querySelector(".flow-link")).markerEnd,
        textFill: getComputedStyle(document.querySelector(".label")).fill,
        background: getComputedStyle(document.body).backgroundColor,
      };
    });
    assert.equal(metrics.overflow, false); assert.equal(metrics.svg, 3);
    assert.deepEqual(metrics.brokenLabels, []);
    for (const key of ["gradient", "radial", "marker", "clip"]) assert.equal(metrics[key], true);
    assert.equal(metrics.transform, 20); assert.match(metrics.markerStyle, /#flow-arrow/);
    assert.notEqual(metrics.textFill, metrics.background);
    for (const section of ["flow", "compare", "relation"]) {
      await frame.locator(`#${section}`).evaluate(el => el.scrollIntoView());
      await shot(`svg-${kind}-${theme}-${width}-${section}`);
    }
    if (kind === "offline") {
      await frame.getByRole("button", { name: "切换外观" }).click();
      assert.equal(await frame.locator("html").getAttribute("data-theme"), theme === "dark" ? "light" : "dark");
    }
    cases.push({ kind, theme, width, ...metrics });
  }
  assert.deepEqual(errors, []); assert.deepEqual(requests, []);
  await fs.writeFile(path.join(outDir, "evidence.json"), JSON.stringify({ success: true, fixtureOnly: true, modelVerified: false, cases, errors, requests }, null, 2));
};
