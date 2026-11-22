package capture

import (
	"reflect"
	"testing"
)

// 以下用例逐条对应 apps/desktop/tests/unit/components/capture-utils.test.ts，再补上 Go 与 JavaScript 空白、大小写语义的边界。

// 抖音「复制打开抖音」口令原样（尾部那串是分享校验噪音，不是链接）。
const douyinShareText = "0.02 复制打开抖音，看看【六叔ultra的作品】4 个提示词，让 AI 像博士一样帮你调研🔥 用A... https://v.douyin.com/0lZNY93J6Ck/ :3pm t@E.Ul FhB:/ 12/15"

// 小红书 PC 版分享口令原样：【】包住标题，链接带 xsec_token（含 `=`）。
const xhsPCShareText = "84 【AI漫剧培训实战课程丨12天🉑独立出片 - 司晨视觉 | 小红书 - 你的生活兴趣社区】 😆 5wtLOclJhAjzJzW 😆 https://www.xiaohongshu.com/discovery/item/6a59e7f3000000000301fc49?source=webshare&xhsshare=pc_web&xsec_token=CBai8aBe1zi_hNlq7nojlsTLX0Z9M9ZhGy22tGMBTxhUI=&xsec_source=pc_share"

const xhsPCShareURL = "https://www.xiaohongshu.com/discovery/item/6a59e7f3000000000301fc49?source=webshare&xhsshare=pc_web&xsec_token=CBai8aBe1zi_hNlq7nojlsTLX0Z9M9ZhGy22tGMBTxhUI=&xsec_source=pc_share"

func TestIsHTTPURLLike(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"https 带查询", "https://example.com/a?b=1", true},
		{"两端空白不算", "  http://example.com  ", true},
		{"协议不分大小写", "HTTPS://Example.com/A", true},
		{"file 协议", "file:///C:/x.txt", false},
		{"ftp 协议", "ftp://example.com", false},
		{"非链接文字", "随手记一笔", false},
		{"空串", "", false},
		{"只有空白", " \t\n", false},
		{"内部有空白", "https://example.com/a b", false},
		{"内部有全角空格", "https://example.com/a\u3000b", false},
		{"只有协议没有主机", "https://", false},
		{"协议前有字符", "xhttps://example.com", false},
		{"U+017F 不是 s（JavaScript 的 /i 不折叠它）", "http\u017f://example.com", false},
		{"两端的 U+FEFF 属于 JavaScript 空白", "\ufeffhttps://example.com\ufeff", true},
		{"U+0085 不属于 JavaScript 空白", "\u0085https://example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsHTTPURLLike(tc.input); got != tc.want {
				t.Fatalf("IsHTTPURLLike(%q) = %v，期望 %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestExtractURLsFromText(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"抖音分享口令里的短链", douyinShareText, []string{"https://v.douyin.com/0lZNY93J6Ck/"}},
		{"小红书 PC 版口令：xsec_token 尾部的 = 要留住", xhsPCShareText, []string{xhsPCShareURL}},
		{"小红书口令的全角逗号紧贴链接，不能被吞进 URL",
			"78 【标题 - 作者】😆 http://xhslink.com/a/abc123，复制本条信息，打开【小红书】查看",
			[]string{"http://xhslink.com/a/abc123"}},
		{"句尾的半角句点", "see https://example.com/a.", []string{"https://example.com/a"}},
		{"多出来的右括号去掉", "(见 https://example.com/a)", []string{"https://example.com/a"}},
		{"链接自带的配平括号留住", "参考 https://ex.com/wiki/Foo_(bar) 这页", []string{"https://ex.com/wiki/Foo_(bar)"}},
		{"多出来的右方括号去掉", "[https://ex.com/a]", []string{"https://ex.com/a"}},
		{"配平的方括号留住", "https://ex.com/a[1]", []string{"https://ex.com/a[1]"}},
		{"连续的句读一并去掉", "看 https://ex.com/a?!,;:.", []string{"https://ex.com/a"}},
		{"句读去掉后再去掉多出来的括号", "(https://ex.com/a).", []string{"https://ex.com/a"}},
		{"按出现顺序去重", "a https://ex.com/1 b https://ex.com/2 c https://ex.com/1", []string{"https://ex.com/1", "https://ex.com/2"}},
		{"协议大写也认", "HTTPS://EX.COM/A 与 Http://ex.com/b", []string{"HTTPS://EX.COM/A", "Http://ex.com/b"}},
		{"路径里的中文保留", "https://ex.com/路径/文章", []string{"https://ex.com/路径/文章"}},
		{"引号截断", `点 "https://ex.com/a" 或 'https://ex.com/b'`, []string{"https://ex.com/a", "https://ex.com/b"}},
		{"尖括号截断", "<https://ex.com/a>", []string{"https://ex.com/a"}},
		{"紧贴的两个链接以后者协议为界", "https://ex.com/ahttps://ex.com/b", []string{"https://ex.com/ahttps://ex.com/b"}},
		{"只有协议没有主体不算", "https:// 空", nil},
		{"没有链接时为空", "会议纪要 12/15 t@E.Ul FhB:/", nil},
		{"空串", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractURLsFromText(tc.input)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractURLsFromText(%q) = %q，期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseDraft(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Draft
	}{
		{"空输入", "   \n  ", Draft{Kind: DraftEmpty}},
		{"U+FEFF 与全角空格也是空白", "\ufeff\u3000", Draft{Kind: DraftEmpty}},
		{"单个链接", " https://example.com/a ", Draft{Kind: DraftURLs, URLs: []string{"https://example.com/a"}}},
		{"多行链接批量导入", "https://example.com/a\nhttps://example.com/b\n\nhttps://example.com/c",
			Draft{Kind: DraftURLs, URLs: []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"}}},
		{"同一行的多个链接也拆开", "https://example.com/a https://example.com/b",
			Draft{Kind: DraftURLs, URLs: []string{"https://example.com/a", "https://example.com/b"}}},
		{"CRLF 与制表符分隔", "https://example.com/a\r\nhttps://example.com/b\thttps://example.com/c",
			Draft{Kind: DraftURLs, URLs: []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"}}},
		{"批内重复链接去重", "https://example.com/a\nhttps://example.com/a", Draft{Kind: DraftURLs, URLs: []string{"https://example.com/a"}}},
		{"抖音分享口令默认采集其中的链接", douyinShareText,
			Draft{Kind: DraftMixed, URLs: []string{"https://v.douyin.com/0lZNY93J6Ck/"}, Text: douyinShareText, Prefer: ActionURLs}},
		{"小红书 PC 口令默认采集链接", xhsPCShareText,
			Draft{Kind: DraftMixed, URLs: []string{xhsPCShareURL}, Text: xhsPCShareText, Prefer: ActionURLs}},
		{"小红书短链口令默认采集链接", "78 【标题 - 作者】😆 http://xhslink.com/a/abc123，复制本条信息，打开【小红书】查看",
			Draft{Kind: DraftMixed, URLs: []string{"http://xhslink.com/a/abc123"},
				Text: "78 【标题 - 作者】😆 http://xhslink.com/a/abc123，复制本条信息，打开【小红书】查看", Prefer: ActionURLs}},
		{"普通网页链接混进说明文字仍默认存文本", "明天看这个 https://example.com/a",
			Draft{Kind: DraftMixed, URLs: []string{"https://example.com/a"}, Text: "明天看这个 https://example.com/a", Prefer: ActionText}},
		{"混合文字取去掉首尾空白的原文", "\n  明天看 https://example.com/a  \n",
			Draft{Kind: DraftMixed, URLs: []string{"https://example.com/a"}, Text: "明天看 https://example.com/a", Prefer: ActionText}},
		{"多个链接里只要有一个没有连接器就默认存文本", "https://v.douyin.com/abc/ 与 https://example.com/a",
			Draft{Kind: DraftMixed, URLs: []string{"https://v.douyin.com/abc/", "https://example.com/a"},
				Text: "https://v.douyin.com/abc/ 与 https://example.com/a", Prefer: ActionText}},
		{"多个链接都有连接器才默认采集", "https://v.douyin.com/abc/ 与 https://b23.tv/abc123",
			Draft{Kind: DraftMixed, URLs: []string{"https://v.douyin.com/abc/", "https://b23.tv/abc123"},
				Text: "https://v.douyin.com/abc/ 与 https://b23.tv/abc123", Prefer: ActionURLs}},
		{"纯文本按文本保存", "会议纪要\n第一条", Draft{Kind: DraftText, Text: "会议纪要\n第一条"}},
		{"没有 http 协议的网址按文本", "example.com/a", Draft{Kind: DraftText, Text: "example.com/a"}},
		{"file 协议按文本", "file:///C:/x.txt", Draft{Kind: DraftText, Text: "file:///C:/x.txt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseDraft(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseDraft(%q) =\n%+v\n期望\n%+v", tc.input, got, tc.want)
			}
		})
	}
}

// 其他有专用连接器的平台（视频、论坛）同样默认采集；B 站专栏不是视频页，按普通网页处理。
func TestParseDraftPlatformConnectors(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		prefer ActionKind
	}{
		{"B 站短链", "【标题】 https://b23.tv/abc123", ActionURLs},
		{"V2EX 帖子", "这帖有意思 https://www.v2ex.com/t/1227616", ActionURLs},
		{"NGA 帖子", "看看这个 https://bbs.nga.cn/read.php?tid=37194262&fav=:x&rand=1", ActionURLs},
		{"小众软件论坛", "这个讨论挺有意思 https://meta.appinn.net/t/topic/89533", ActionURLs},
		{"2libra 帖子", "这个讨论也看看 https://2libra.com/post/health-consultation/bUSaOUc", ActionURLs},
		{"B 站专栏不是视频页", "看看这个 https://www.bilibili.com/read/cv123", ActionText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDraft(tc.input)
			if got.Kind != DraftMixed || got.Prefer != tc.prefer {
				t.Fatalf("ParseDraft(%q) = %+v，期望 mixed 且 Prefer=%v", tc.input, got, tc.prefer)
			}
		})
	}
}

func TestResolveAction(t *testing.T) {
	single := ParseDraft("https://example.com/a")
	douyin := ParseDraft(douyinShareText)
	plain := ParseDraft("明天看 https://example.com/a")
	cases := []struct {
		name     string
		draft    Draft
		override ActionKind
		want     Action
	}{
		{"链接批原样透传", single, ActionEmpty, Action{Kind: ActionURLs, URLs: []string{"https://example.com/a"}}},
		{"链接批不受改判影响", single, ActionText, Action{Kind: ActionURLs, URLs: []string{"https://example.com/a"}}},
		{"空草稿不受改判影响", Draft{Kind: DraftEmpty}, ActionURLs, Action{Kind: ActionEmpty}},
		{"纯文本不受改判影响", ParseDraft("随手记"), ActionURLs, Action{Kind: ActionText, Text: "随手记"}},
		{"未改判时按 prefer 采集链接", douyin, ActionEmpty, Action{Kind: ActionURLs, URLs: []string{"https://v.douyin.com/0lZNY93J6Ck/"}}},
		{"未改判时按 prefer 存文本", plain, ActionEmpty, Action{Kind: ActionText, Text: "明天看 https://example.com/a"}},
		{"改判成文本", douyin, ActionText, Action{Kind: ActionText, Text: douyinShareText}},
		{"改判成链接", plain, ActionURLs, Action{Kind: ActionURLs, URLs: []string{"https://example.com/a"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveAction(tc.draft, tc.override); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ResolveAction = %+v，期望 %+v", got, tc.want)
			}
		})
	}
}
