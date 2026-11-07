// @vitest-environment node
import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import { extractWebArticle } from "../../../src/main/services/web-capture/web-extract-article";

const PAGE_URL = "https://example.com/docs/guide/page.html";
const page = (body: string, head = "<title>示例页面</title>") =>
  `<!doctype html><html><head>${head}</head><body>${body}</body></html>`;
const prose = (count: number, label = "正文") =>
  Array.from(
    { length: count },
    (_, i) => `<p>${label}第 ${i + 1} 段：这是一段足够长的说明文字，让正文提取把它识别为文章内容，并且保持原有的段落顺序。</p>`,
  ).join("");
const extract = (html: string, url = PAGE_URL, status = 200) => extractWebArticle(html, url, status);
const markdownOf = (body: string) => extract(page(`<article>${prose(3)}${body}${prose(2, "结尾")}</article>`)).result.markdown;

describe("R1 代码块", () => {
  it("所有 <pre> 输出围栏代码块，原文不做 Markdown 转义，语言取自 class", () => {
    const markdown = markdownOf(
      '<pre><code class="language-js">const total = a * b; // _x_ [y](z) #1</code></pre>' +
        "<pre>plain *text* &lt;tag&gt;</pre>" +
        '<div class="highlight-python3 notranslate"><div class="highlight"><pre><span></span>print("hi")</pre></div></div>',
    );
    expect(markdown).toContain("```js\nconst total = a * b; // _x_ [y](z) #1\n```");
    expect(markdown).toContain("```\nplain *text* <tag>\n```");
    expect(markdown).toContain('```python3\nprint("hi")\n```');
  });

  it("按行排列的元素与 <br> 各成一行；代码含反引号时加长围栏", () => {
    const markdown = markdownOf(
      '<pre class="prism-code language-ts"><code><span class="token-line">let a = 1;<br></span><span class="token-line">let b = 2;<br></span></code></pre>' +
        "<pre><div>first</div>\n<div>second</div></pre>" +
        "<pre>before\n```\nafter</pre>",
    );
    expect(markdown).toContain("```ts\nlet a = 1;\nlet b = 2;\n```");
    expect(markdown).toContain("```\nfirst\nsecond\n```");
    expect(markdown).toContain("````\nbefore\n```\nafter\n````");
  });
});

describe("R2 表格", () => {
  it("数据表输出 GFM 管道表格：表头、单元格里的竖线转义、跨行跨列对齐", () => {
    const markdown = markdownOf(
      "<table><caption>参数</caption><thead><tr><th>名称</th><th>说明</th><th>默认</th></tr></thead><tbody>" +
        "<tr><td><code>a|b</code></td><td rowspan=\"2\">两行共用</td><td>1</td></tr>" +
        '<tr><td>c</td><td>2</td></tr><tr><td colspan="2">跨两列</td><td>3</td></tr></tbody></table>',
    );
    expect(markdown).toContain(
      [
        "参数",
        "",
        "| 名称 | 说明 | 默认 |",
        "| --- | --- | --- |",
        "| `a\\|b` | 两行共用 | 1 |",
        "| c |  | 2 |",
        "| 跨两列 |  | 3 |",
      ].join("\n"),
    );
  });

  it("单行或嵌套的版面表格不转成管道表格，内容按段落保留", () => {
    const markdown = markdownOf(
      '<table><tr><td><h2>侧栏标题</h2><p>版面里的段落一。</p></td><td><p>版面里的段落二，内容很长。</p></td></tr></table>',
    );
    expect(markdown).not.toMatch(/^\|/m);
    expect(markdown).toContain("版面里的段落一。");
    expect(markdown).toContain("版面里的段落二，内容很长。");
  });
});

describe("R3 绝对地址", () => {
  it("用最终网址把链接与图片解析为绝对地址；页内锚点同样补全", () => {
    const markdown = markdownOf(
      '<p>见 <a href="../api/x.html">接口文档</a> 与 <a href="#install">安装</a>。<img src="/img/a.png" alt="示意图"></p>',
    );
    expect(markdown).toContain("[接口文档](https://example.com/docs/api/x.html)");
    expect(markdown).toContain("[安装](https://example.com/docs/guide/page.html#install)");
    expect(markdown).toContain("![示意图](https://example.com/img/a.png)");
  });

  it("遵循 <base href>；脚本链接只留文字；取不到地址的图片不保留，懒加载图片取 data-src", () => {
    const { markdown } = extract(
      page(
        `<article>${prose(3)}<p><a href="guide.html">指南</a> <a href="javascript:void(0)">展开</a>` +
          '<img alt="空图"><img src="data:image/gif;base64,R0lGODlh" data-src="lazy.png" alt="懒加载"></p></article>',
        '<title>示例页面</title><base href="https://cdn.example.org/base/">',
      ),
    ).result;
    expect(markdown).toContain("[指南](https://cdn.example.org/base/guide.html)");
    expect(markdown).toContain("展开");
    expect(markdown).not.toContain("javascript:");
    expect(markdown).not.toContain("空图");
    expect(markdown).toContain("![懒加载](https://cdn.example.org/base/lazy.png)");
    expect(markdown).not.toMatch(/\]\((?![a-z][a-z0-9+.-]*:)/i);
  });
});

describe("R4 一级标题", () => {
  it("保留正文开头的主标题与正文内的 h1，不降为 h2", () => {
    const { markdown } = extract(
      page(`<article><h1>示例页面</h1>${prose(3)}<h1>附录</h1>${prose(2, "附录")}</article>`),
    ).result;
    expect(markdown.startsWith("# 示例页面")).toBe(true);
    expect(markdown).toMatch(/^# 附录$/m);
    expect(markdown).not.toMatch(/^## (示例页面|附录)$/m);
  });
});

describe("R5 语义区域回退", () => {
  // Readability 把 class 含 remark 的容器当作无关内容整块删除，只剩导语；语义区域仍包含全文。
  const truncated = (intro: string) =>
    page(
      `<main><h1>示例页面</h1>${intro}<div class="remark-text">${prose(12, "主体")}</div>` +
        '<div class="related-posts"><a href="/a">相关文章一</a> <a href="/b">相关文章二</a></div></main>' +
        `<aside>${prose(1, "侧栏")}</aside>`,
    );

  it("Readability 取到的文字不足清洗后语义区域的 60% 时改用该区域，并清掉区域里的模板容器", () => {
    const article = extract(truncated(prose(2, "导语")));
    expect(article.source).toBe("region");
    expect(article.readabilityChars).toBeLessThan(0.6 * article.regionChars);
    const { markdown } = article.result;
    expect(markdown).toContain("导语第 1 段");
    expect(markdown).toContain("主体第 12 段");
    expect(markdown).not.toContain("相关文章");
    expect(markdown).not.toContain("侧栏第 1 段");
  });

  it("区域文字不足 500 字时即使比例偏低也不回退", () => {
    const article = extract(
      page(`<main>${prose(3, "导语")}<div class="remark-text">${prose(5, "备注")}</div></main>`),
    );
    expect(article.regionChars).toBeGreaterThan(0);
    expect(article.regionChars).toBeLessThan(500);
    expect(article.readabilityChars).toBeLessThan(0.6 * article.regionChars);
    expect(article.source).toBe("readability");
  });

  it("比例只计段落文字：区域里没标成导航的链接目录不会触发回退", () => {
    const menu = Array.from({ length: 60 }, (_, i) => `<li><a href="/chapter/${i}">第 ${i + 1} 章：组件与状态</a></li>`).join("");
    const article = extract(page(`<main><div class="chapters"><span>基础</span><ul>${menu}</ul></div><div class="tutorial">${prose(12)}</div></main>`));
    expect(article.regionChars).toBeGreaterThanOrEqual(500);
    expect(article.source).toBe("readability");
    expect(article.result.markdown).toContain("正文第 12 段");
    expect(article.result.markdown).not.toContain("组件与状态");
  });

  it("Readability 取不到 100 字时改用清理页框后的整页：main 外的导航与 main 内的边栏不进正文", () => {
    const article = extract(
      page(
        '<nav>广告导航</nav><main><h1>组件配置</h1><p>动态正文支持中文知识管理。</p><div class="sidebar">推广位</div>' +
          '<pre><code>print("归知")</code></pre><div style="display: none">隐藏弹层</div>' +
          '<p style="color:red;visibility:hidden">隐藏提示</p></main><footer>版权所有</footer>',
      ),
    );
    expect(article.source).toBe("page");
    const { markdown } = article.result;
    expect(markdown).toContain("# 组件配置");
    expect(markdown).toContain("动态正文支持中文知识管理。");
    expect(markdown).toContain('print("归知")');
    for (const chrome of ["广告导航", "推广位", "版权所有", "隐藏弹层", "隐藏提示"]) expect(markdown).not.toContain(chrome);
    // 没有页框的短文照常保留全文。
    const short = extract(page("<article><p>今天发布了一个小更新。</p></article>", "<title>短文</title>"));
    expect(short.result.markdown).toBe("今天发布了一个小更新。");
    expect(short.result.error).toBeUndefined();
  });

  it("正文区域只按 class 认定：id 为 article-content 的短标签块不会被当成正文", () => {
    const html = page(
      `<main>${prose(6)}</main><div id="article-content" class="c-article-meta"><a href="/t/1">标签一</a> <a href="/t/2">标签二</a></div>`,
    );
    const article = extract(html);
    expect(article.source).toBe("readability");
    expect(article.result.markdown).toContain("正文第 6 段");
  });
});

describe("回归中补充的正文规则", () => {
  it("「标题链接 + 一句说明」的卡片不被当成链接区删掉（链接占比 0.2–0.3）", () => {
    const card = (title: string, text: string) =>
      `<div class="card"><a href="/${title}" class="button">${title}</a><p>${text}</p></div>`;
    const { markdown } = extract(
      page(
        `<main><h2>文档</h2>${prose(2)}<section>${card("标准库参考手册", "涵盖全部模块、类型与函数的接口说明和示例。")}` +
          `${card("版本迁移指南", "逐个版本列出不兼容的变化以及对应的迁移步骤说明。")}` +
          `${card("编译错误索引", "编译器可能给出的每一个错误及其详细的原因与解释。")}</section>${prose(2, "后记")}</main>`,
      ),
    ).result;
    expect(markdown).toContain("涵盖全部模块、类型与函数的接口说明和示例。");
    expect(markdown).toContain("逐个版本列出不兼容的变化以及对应的迁移步骤说明。");
    expect(markdown).toContain("编译器可能给出的每一个错误及其详细的原因与解释。");
  });

  it("标签页里未选中的面板与收起的 <details> 内容都保留", () => {
    const { markdown } = extract(
      page(
        `<article>${prose(3)}<div role="tabpanel"><pre><code>npm install demo</code></pre></div>` +
          '<div role="tabpanel" hidden><pre><code>pnpm add demo</code></pre></div>' +
          '<details><summary>版本历史</summary><div style="display: none; overflow: hidden"><p>第二版起支持在后台运行，此前只能在前台运行。</p></div></details>' +
          `${prose(2, "结尾")}</article>`,
      ),
    ).result;
    expect(markdown).toContain("npm install demo");
    expect(markdown).toContain("pnpm add demo");
    expect(markdown).toContain("第二版起支持在后台运行，此前只能在前台运行。");
  });

  it("渲染快照里逐行展开的 CodeMirror 编辑器拼回一个代码块", () => {
    const { markdown } = extract(
      page(
        `<article>${prose(3)}<div class="cm-editor"><div class="cm-gutters"><div>1</div><div>2</div></div>` +
          '<div class="cm-content"><div class="cm-line">import { html } from "lit";</div><div class="cm-line"><br></div>' +
          '<div class="cm-line">export const view = html`&lt;p&gt;hi&lt;/p&gt;`;</div></div></div>' +
          `${prose(2, "结尾")}</article>`,
      ),
    ).result;
    expect(markdown).toContain('```\nimport { html } from "lit";\n\nexport const view = html`<p>hi</p>`;\n```');
  });

  it("Readability 把标题区当页框删掉时，补回主标题与导语，不带入折叠的徽标说明", () => {
    const { markdown } = extract(
      page(
        '<main><div class="layout__header"><h1>示例页面</h1><details class="badge"><summary>支持情况</summary>' +
          "<p>这一功能在主流浏览器中已经稳定可用很多年了。</p></details>" +
          "<section><p>导语：这个接口用于异步观察目标元素与视口或祖先元素之间交叉状态的变化。</p></section></div>" +
          `<aside class="sidebar">${prose(1, "侧栏")}</aside><div class="layout__body">${prose(8, "主体")}</div></main>`,
      ),
    ).result;
    expect(markdown.startsWith("# 示例页面\n\n导语：这个接口用于异步观察目标元素与视口或祖先元素之间交叉状态的变化。")).toBe(true);
    expect(markdown).toContain("主体第 8 段");
    expect(markdown).not.toContain("已经稳定可用很多年");
  });
});

describe("R7 结果字段与错误分类", () => {
  it("沿用原提取器字段：标题、作者、带时区的发布时间、段落编号与内容哈希", () => {
    const head =
      '<title> 示例 页面 </title><meta name="author" content="张三">' +
      '<meta property="article:published_time" content="2026-11-07T08:30:00+08:00">';
    const { result } = extract(page(`<article>${prose(3)}</article>`, head));
    expect(result).toMatchObject({
      title: "示例 页面",
      author: "张三",
      publishedAt: Date.parse("2026-11-07T08:30:00+08:00"),
      dateConfidence: "exact",
      complete: true,
      truncated: false,
      warnings: [],
    });
    expect(result.error).toBeUndefined();
    expect(result.contentHash).toBe(createHash("sha256").update(result.markdown).digest("hex"));
    expect(result.paragraphs[0]).toEqual({ id: "p1", text: result.markdown.split(/\n\s*\n/)[0] });
    const naive = extract(page(`<article>${prose(3)}</article>`, '<title>t</title><meta property="article:published_time" content="2026-11-07T08:30:00">')).result;
    expect(naive.publishedAt).toBeNull();
    expect(naive.dateConfidence).toBe("unknown");
  });

  it.each([
    [401, "", "login", false],
    [407, "", "login", false],
    [200, '<form><input type="password"></form>', "login", false],
    [403, "", "restricted", false],
    [429, "", "restricted", false],
    [502, "", "network", true],
  ])("HTTP %i 与页面内容按原规则分类", (status, extra, code, retryable) => {
    const title = extra ? "<title>账号登录</title>" : "<title>示例页面</title>";
    const { result } = extract(page(`<article>${prose(3)}</article>${extra}`, title), PAGE_URL, status);
    expect(result.error).toMatchObject({ code, retryable });
    expect(result.complete).toBe(false);
  });

  it("验证页标题报 captcha，没有正文报 empty", () => {
    expect(extract(page(`<article>${prose(2)}</article>`, "<title>Just a moment...</title>")).result.error?.code).toBe("captcha");
    const empty = extract(page("<div></div>")).result;
    expect(empty.error).toMatchObject({ code: "empty", retryable: false });
    expect(empty.title).toBe("示例页面");
  });

  it("超过 200,000 字符按码点截断，不切断代理对，并给出警告", () => {
    const { result } = extract(page(`<article>${prose(2)}<pre>${"😀".repeat(210_000)}</pre></article>`));
    expect(result.truncated).toBe(true);
    expect(result.complete).toBe(false);
    expect(result.warnings).toEqual(["正文超过 200,000 字符，已截断"]);
    expect(Array.from(result.markdown)).toHaveLength(200_000);
    expect(result.markdown.endsWith("😀")).toBe(true);
  });

  it("公众号页面直接取 #js_content：去掉初始隐藏，懒加载图片换成真实地址", () => {
    const { result, source } = extract(
      page(
        '<h1 id="activity-name"> 冬游黄山 </h1><nav>导航</nav><div id="js_content" style="visibility: hidden">' +
          '<p>黄山两次旅行的正文和路线。</p><img data-src="https://mmbiz.qpic.cn/a.jpg"></div>',
        "<title></title>",
      ),
      "https://mp.weixin.qq.com/s/example",
    );
    expect(source).toBe("wechat");
    expect(result.title).toBe("冬游黄山");
    expect(result.markdown).toContain("黄山两次旅行的正文和路线。");
    expect(result.markdown).toContain("![](https://mmbiz.qpic.cn/a.jpg)");
    expect(result.markdown).not.toContain("导航");
  });
});
