"""正文去噪回归：验证真实转换链路，并保护教程、目录、代码与多文章页面。"""
import hashlib
import json
import os
import re
from pathlib import Path
import sys
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
os.environ["LITELLM_LOCAL_MODEL_COST_MAP"] = "True"
sys.path.insert(0, str(ROOT / "apps/desktop/resources/crawl4ai-worker"))
from extract import extract


def capture(body):
    return extract(f"<html><head><title>SIM 安全指南</title></head><body>{body}</body></html>",
                   "https://example.com/guide", 200)


class ArticleCleanupTests(unittest.TestCase):
    def test_blog_template_and_promotions(self):
        result = capture('''<main><article><div class="entry-content">
          <h2>查询初始邮箱</h2><p>登录账户后打开个人资料，核对初始邮箱。</p>
          <ul><li>保留安全步骤：启用双重验证。</li>
          <li>SIM &amp;Card 羊毛资讯群：美国公司、信用卡 👉 <a href="https://t.me/group">点击加入 Telegram 群组</a></li>
          <li>The Miscellany 商店：精选好物，持续上新 👉 <a href="https://shop.example.com">前往逛逛</a></li></ul>
          </div><div id="comments"><h2>评论 (0)</h2><h3>发表评论</h3><form>昵称</form></div>
          </article><aside><h2>加入我们</h2><a href="https://t.me/group">Telegram 羊毛群</a>
          <h2>分类</h2><a href="/tax">税务</a><h2>热门文章</h2><a href="/other">葡萄牙 NIF</a></aside></main>''')
        markdown = result["markdown"]
        self.assertIn("核对初始邮箱", markdown)
        self.assertIn("启用双重验证", markdown)
        for noise in ("资讯群", "商店", "评论", "发表评论", "加入我们", "分类", "热门文章", "葡萄牙"):
            self.assertNotIn(noise, markdown)
        self.assertTrue(result["complete"])
        self.assertEqual(result["contentHash"], hashlib.sha256(markdown.encode()).hexdigest())
        self.assertEqual([p["text"] for p in result["paragraphs"]],
                         [p for p in re.split(r"\n\s*\n", markdown) if p.strip()])

    def test_nested_main_article_uses_inner_body(self):
        markdown = capture('''<main><article><p>需要保留的正文。</p></article>
          <aside>热门文章推荐</aside><footer>联系站长</footer></main>''')["markdown"]
        self.assertIn("需要保留", markdown)
        self.assertNotIn("热门文章", markdown)
        self.assertNotIn("联系站长", markdown)

    def test_link_heavy_footer_sections_without_classes(self):
        markdown = capture('''<article><p>正文结束，检查安全设置。</p>
          <h3>📕 加入我们</h3><p><a href="https://t.me/group">Telegram 羊毛群</a> · <a href="/shop">店铺</a></p>
          <h3>📂 分类</h3><p><a href="/a">教程 21</a> <a href="/b">税务 8</a></p>
          <h3>🔥 热门文章</h3><ul><li><a href="/c">其他文章一</a></li><li><a href="/d">其他文章二</a></li></ul>
          <h2>附录</h2><p>这段正文还应保留。</p></article>''')["markdown"]
        self.assertIn("安全设置", markdown)
        self.assertIn("正文还应保留", markdown)
        for noise in ("加入我们", "分类", "热门文章", "其他文章"):
            self.assertNotIn(noise, markdown)

    def test_body_keywords_and_structured_content_survive(self):
        markdown = capture('''<article><nav><a href="#steps">目录</a></nav>
          <h2>分类</h2><p>分类方法：按账户风险分组。这是正文说明。</p>
          <h2>Telegram 教程</h2><ul><li>打开 Telegram，<a href="https://t.me/tutorial">查看教程</a>。</li>
          <li>请勿点击加入资讯群的陌生链接，<code>点击加入 Telegram 群</code> 是诈骗示例。</li></ul>
          <aside>安全提示：不要公开邮箱。</aside><footer>作者补充说明。</footer>
          <table><tr><td>字段</td><td>邮箱</td></tr></table><pre>print("评论")</pre></article>''')["markdown"]
        for value in ("目录", "分类方法", "Telegram 教程", "查看教程", "诈骗示例", "安全提示", "作者补充", "字段", 'print("评论")'):
            self.assertIn(value, markdown)

    def test_long_linked_article_section_survives(self):
        markdown = capture('''<article><h2>相关文章</h2><p>以下是对两篇资料的详细分析，
          用于解释账户保护流程中的具体差异，而不是页脚推荐列表。
          <a href="/a">资料一</a> 与 <a href="/b">资料二</a> 支持这个结论。</p></article>''')["markdown"]
        self.assertIn("详细分析", markdown)
        self.assertIn("相关文章", markdown)

    def test_promotion_warnings_and_plain_tutorial_links_survive(self):
        markdown = capture('''<article><ul>
          <li>请勿打开 <a href="https://t.me/scam">点击加入 Telegram 群组</a>，这是诈骗。</li>
          <li>Telegram 群组设置教程：点击加入之前检查邀请者，<a href="/guide">教程详情</a>。</li>
          </ul></article>''')["markdown"]
        self.assertIn("这是诈骗", markdown)
        self.assertIn("教程详情", markdown)

    def test_multiple_articles_are_preserved(self):
        markdown = capture('''<main><article class="post-content"><p>第一篇正文。</p></article>
          <article class="post-content"><p>第二篇正文。</p></article></main>''')["markdown"]
        self.assertIn("第一篇正文", markdown)
        self.assertIn("第二篇正文", markdown)

    def test_wechat_explicit_body_is_preserved(self):
        result = extract('''<html><body><div id="js_content" style="display:none">
          <h2>加入我们</h2><p>公众号正文内容。</p><img data-src="https://example.com/a.png">
          </div><aside>热门文章</aside></body></html>''', "https://mp.weixin.qq.com/s/example", 200)
        self.assertIn("公众号正文内容", result["markdown"])
        self.assertIn("加入我们", result["markdown"])
        self.assertIn("a.png", result["markdown"])
        self.assertNotIn("热门文章", result["markdown"])

    def test_offline_worker_protocol(self):
        message = dict(v=1, id="cleanup-test", status=200, url="https://example.com",
                       html='<main><article>离线正文</article><aside>热门文章</aside></main>')
        process = subprocess.run(
            [sys.executable, "-B", "-s", str(ROOT / "apps/desktop/resources/crawl4ai-worker/extract-only.py")],
            input=json.dumps(message) + "\n", encoding="utf-8", capture_output=True, timeout=30,
        )
        self.assertEqual(process.returncode, 0, process.stderr)
        ready, response = [json.loads(line) for line in process.stdout.splitlines()]
        self.assertEqual(ready, dict(v=1, type="ready"))
        self.assertEqual(response["id"], "cleanup-test")
        self.assertIn("离线正文", response["result"]["markdown"])
        self.assertNotIn("热门文章", response["result"]["markdown"])


if __name__ == "__main__":
    unittest.main()
