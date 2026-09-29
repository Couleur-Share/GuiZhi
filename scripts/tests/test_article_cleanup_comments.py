"""正文清理遇到 HTML 注释与处理指令时不得失败，且不误伤其后的正文。

回归背景：v0.26.0 的 clean_article 遍历所有后代并对每个节点调用 names()；lxml 注释节点的 .get()
忽略默认值直接返回 None，None + str 抛 TypeError，整页提取失败（38 个公开网页样本中 21 页受影响，
含 MDN、react.dev、vuejs.org 等文档站）。

本文件只依赖 lxml，可在没有 Crawl4AI 的环境（CI 的 Linux 通用检查）运行；
经完整转换链路的去噪回归见 test_article_cleanup.py，那份需要随包 Python。
"""
from pathlib import Path
import sys
import unittest

from lxml import html as lhtml

WORKER = Path(__file__).resolve().parents[2] / "apps" / "desktop" / "resources" / "crawl4ai-worker"
sys.path.insert(0, str(WORKER))
from article_cleanup import article_root, clean_article  # noqa: E402


def clean(source):
    return clean_article(article_root(lhtml.fromstring(source)))


def markup(node):
    return lhtml.tostring(node, encoding="unicode")


class CommentNodesTest(unittest.TestCase):
    def test_comments_inside_article_do_not_fail_and_are_removed(self):
        root = clean(
            '<html><body><article class="entry-content">'
            "<!-- wp:paragraph --><p>正文</p><!-- /wp:paragraph -->"
            '<div class="sidebar">推广位</div></article></body></html>'
        )
        self.assertIn("正文", root.text_content())
        self.assertNotIn("推广位", root.text_content(), "模板边栏仍按原规则移除")
        self.assertNotIn("<!--", markup(root))

    def test_comments_anywhere_when_the_page_has_no_article_region(self):
        # 没有正文区域时根就是整棵树，<head> 与 <body> 里的注释都会被遍历。
        root = clean("<html><head><!-- 统计脚本 --><title>t</title></head><body><p>甲</p><!-- 分隔 --><p>乙</p></body></html>")
        self.assertIn("甲", root.text_content())
        self.assertIn("乙", root.text_content())
        self.assertNotIn("<!--", markup(root))

    def test_conditional_comments_and_processing_instructions_are_removed(self):
        root = clean(
            "<html><head><!--[if IE]><script src='ie.js'></script><![endif]--></head>"
            "<body><article><?php echo 1; ?><p>正文</p></article></body></html>"
        )
        self.assertIn("正文", root.text_content())
        self.assertNotIn("ie.js", markup(root))
        self.assertNotIn("php", markup(root))

    def test_text_around_a_removed_comment_stays_joined(self):
        # 浏览器渲染时注释不产生间隔，React 服务端渲染常输出 "a<!-- -->b"。
        root = clean("<html><body><article><p>Hello<!-- x -->World</p></article></body></html>")
        self.assertEqual(root.xpath("string(.//p)"), "HelloWorld")

    def test_comments_next_to_removed_chrome_and_link_sections(self):
        root = clean(
            '<html><body><article class="entry-content"><p>正文</p>'
            '<div class="sidebar"><!-- ad -->推广位</div>'
            "<h2>相关文章</h2><!-- 列表 -->"
            "<ul><li><a href='/a'>甲文章</a></li><li><a href='/b'>乙文章</a></li></ul>"
            "</article></body></html>"
        )
        text = root.text_content()
        self.assertIn("正文", text)
        self.assertNotIn("推广位", text)
        self.assertNotIn("相关文章", text, "精确标题加高链接占比的文末链接区仍整段移除")


if __name__ == "__main__":
    unittest.main()
