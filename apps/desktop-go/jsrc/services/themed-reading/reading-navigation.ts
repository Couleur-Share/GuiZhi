/** 为已保存和新生成的页面补齐目录语义；正文节点、文字与顺序保持不变。 */
export function enhanceThemedNavigation(document: Document): void {
  // 目录可以默认收起，但模型误放其中的正文必须保持可见。
  for (const details of document.querySelectorAll("details.reading-contents")) {
    if (details.querySelector("[data-source-block]")) details.setAttribute("open", "");
  }
  for (const nav of document.querySelectorAll("nav")) {
    const links = [...nav.querySelectorAll<HTMLAnchorElement>("a[href^='#']")]
      .filter((link) => document.getElementById(link.getAttribute("href").slice(1)));
    if (links.length < 2) continue;
    nav.setAttribute("data-reader-toc", "");
    nav.setAttribute("aria-label", "文章目录");
    for (const link of links) link.setAttribute("data-reader-toc-link", "");
    const label = [...nav.children].find((node) => !node.querySelector("a") && !node.matches("a") && node.textContent.trim());
    if (label) label.setAttribute("data-reader-toc-label", "");
  }
}
