"""归知的保守正文转换：不用英文词数或 fit_markdown 决定是否保留正文。"""
import hashlib
import re
from datetime import datetime
from urllib.parse import urlsplit
from lxml import html as lhtml
from crawl4ai.content_scraping_strategy import LXMLWebScrapingStrategy
from crawl4ai.markdown_generation_strategy import DefaultMarkdownGenerator


def extract(html, url, status):
    tree = lhtml.fromstring(html or "<html><body></body></html>", base_url=url)
    title = " ".join(tree.xpath("//title/text()") or tree.xpath("//h1//text()"))[:300]
    author = " ".join(tree.xpath('//meta[@name="author"]/@content'))[:300]
    requires_login = bool(tree.xpath('//input[@type="password"]')) and bool(re.search(r"登录|登入|sign in|log in|login", title, re.I))
    dates = tree.xpath('//meta[@property="article:published_time"]/@content')
    published = None
    if dates:
        try:
            value = datetime.fromisoformat(dates[0].replace("Z", "+00:00"))
            if value.tzinfo:
                published = int(value.timestamp() * 1000)
        except ValueError:
            pass
    # 只有显式正文区域存在时才去除页面级导航；保留正文内目录、表格与代码。
    wechat = tree.xpath('//*[@id="js_content"]') if urlsplit(url).hostname == "mp.weixin.qq.com" else []
    regions = wechat or tree.xpath("//main | //article | //*[@role='main']")
    root = regions[0] if len(regions) == 1 else tree
    if wechat:
        title = " ".join(tree.xpath('//*[@id="activity-name"]//text()') or tree.xpath('//meta[@property="og:title"]/@content')).strip()[:300] or title
        # 公众号正文初始隐藏，图片由脚本填充；明确正文区域不依赖脚本执行成功。
        root.attrib.pop("style", None)
        for image in root.xpath('.//img[@data-src]'):
            if urlsplit(image.get("data-src", "")).scheme in ("http", "https"):
                image.set("src", image.get("data-src"))
    for node in root.xpath(".//script|.//style|.//noscript|.//form"):
        node.drop_tree()
    if root is tree:
        for node in root.xpath("./body/nav|./body/footer|./body/header"):
            node.drop_tree()
    markup = lhtml.tostring(root, encoding="unicode")
    scraped = LXMLWebScrapingStrategy().scrap(url, markup, word_count_threshold=0)
    markdown = DefaultMarkdownGenerator().generate_markdown(scraped.cleaned_html, base_url=url).raw_markdown.strip()
    text = " ".join(root.itertext()).strip()
    error = None
    if status in (401, 407) or requires_login:
        error = ("login", "网页需要登录")
    elif status in (403, 429):
        error = ("restricted", "网站拒绝访问或限制请求频率")
    elif status >= 400:
        error = ("network", "网站返回 HTTP " + str(status))
    elif re.search(r"^(just a moment|verify you are human|人机验证|安全验证)", title.strip(), re.I):
        error = ("captcha", "网页要求完成验证码")
    elif not text or not markdown:
        error = ("empty", "网页没有可保存的正文")
    truncated = len(markdown) > 200000
    markdown = markdown[:200000]
    return dict(title=title or url, author=author, publishedAt=published,
                dateConfidence="exact" if published else "unknown", markdown=markdown,
                paragraphs=[dict(id="p" + str(i + 1), text=p) for i, p in enumerate(re.split(r"\n\s*\n", markdown)) if p.strip()],
                contentHash=hashlib.sha256(markdown.encode()).hexdigest(),
                complete=not error and not truncated, truncated=truncated,
                warnings=["正文超过 200,000 字符，已截断"] if truncated else [],
                **({"error": dict(code=error[0], message=error[1], retryable=error[0] == "network")} if error else {}))
