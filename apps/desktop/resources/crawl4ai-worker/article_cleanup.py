"""按 DOM 正文边界清理博客模板；不按正文关键词或英文词数删段落。"""
import re

from lxml import etree


CONTENT_NAMES = {"entry-content", "post-content", "article-content", "post-body"}
CHROME_NAMES = {
    "sidebar", "widget", "widget-area", "comments", "comment-list",
    "comment-respond", "respond", "disqus_thread", "related-posts", "related-articles", "post-navigation",
    "social-share", "share-buttons",
}


def names(node):
    return set((node.get("class", "") + " " + node.get("id", "")).lower().split())


def text(node):
    return " ".join(node.itertext()).strip()


def unique_inner(nodes):
    # main 包着 article 时只取内层；多个独立 article 是列表页，不能只存第一篇。
    leaves = [node for node in nodes if not any(
        other is not node and node in other.iterancestors() for other in nodes
    )]
    return leaves[0] if len(leaves) == 1 else None


def article_root(tree):
    candidates = [node for node in tree.iter() if isinstance(node.tag, str) and (
        names(node) & CONTENT_NAMES or node.get("itemprop") == "articleBody"
    )]
    chosen = unique_inner(candidates)
    if chosen is not None:
        return chosen
    regions = tree.xpath("//main | //article | //*[@role='main']")
    chosen = unique_inner(regions)
    return chosen if chosen is not None else tree


def is_promotion(node):
    value = text(node)
    # 只处理独立短列表项：行动号召 + 推广对象 + 链接，正文里的教程链接保留。
    if len(value) > 240 or node.xpath(".//pre|.//code|.//table"):
        return False
    if re.search(r"不要|请勿|切勿|警惕|诈骗|骗局|风险|do not|don't|scam|warning", value, re.I):
        return False
    links = node.xpath(".//a[@href]")
    return any(re.search(
        r"点击加入|前往逛逛|加入.{0,8}(?:群|频道)|立即购买|click to join|shop now", text(link), re.I
    ) for link in links) and bool(re.search(
        r"资讯群|Telegram\s*群|商店|店铺|购物|优惠群|telegram\s*(?:group|channel)", value, re.I
    ))


def clean_article(root):
    # 注释与处理指令不含正文，又是 .get()/.tag 语义与元素不同的节点：先整体移除并保留其后的文字，
    # 下游只需处理元素。此前正文内出现 HTML 注释会因 None + str 抛 TypeError，整页提取失败。
    etree.strip_elements(root, etree.Comment, etree.ProcessingInstruction, with_tail=False)
    for node in list(root.iterdescendants()):
        if node.getparent() is None or root not in node.iterancestors():
            continue
        if names(node) & CHROME_NAMES or node.get("role") == "complementary":
            node.drop_tree()
        elif node.tag == "li" and is_promotion(node):
            node.drop_tree()
    clean_link_sections(root)
    # 页面级边栏/页脚可以删；正文内的 aside、目录 nav 和作者 footer 保留。
    for node in root.xpath(".//aside|.//nav|.//footer|.//header"):
        if not any(names(parent) & CONTENT_NAMES or parent.tag == "article"
                   or parent.get("itemprop") == "articleBody"
                   for parent in node.iterancestors()):
            node.drop_tree()
    return root


def clean_link_sections(root):
    # 没有模板 class 的文末链接区：精确标题 + 高链接占比，两项同时满足才删。
    labels = {"加入我们", "热门文章", "相关文章", "分类", "join us", "related posts", "popular posts"}
    for heading in list(root.xpath(".//h2|.//h3|.//h4")):
        if root not in heading.iterancestors():
            continue
        label = re.sub(r"^[\W_]+|[\W_]+$", "", text(heading)).lower()
        if label not in labels:
            continue
        blocks = []
        for sibling in heading.itersiblings():
            if sibling.tag in ("h1", "h2", "h3", "h4", "h5", "h6"):
                break
            blocks.append(sibling)
        links = [link for block in blocks for link in block.xpath("descendant-or-self::a[@href]")]
        chars = sum(len(text(block)) for block in blocks)
        linked = sum(len(text(link)) for link in links)
        if len(links) < (1 if label in {"加入我们", "join us"} else 2):
            continue
        if not chars or linked / chars < 0.65 or any(
            block.xpath("descendant-or-self::pre|descendant-or-self::code|descendant-or-self::table")
            for block in blocks
        ):
            continue
        for node in [heading, *blocks]:
            node.drop_tree()
