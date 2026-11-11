// 字符集解码对拍：产品静态抓取用 TextDecoder(label, {fatal:true}) 解码响应体（失败则回退到渲染路径）。
// 本程序生成输入（含合法编码文本、随机字节与 BOM），Node 解码，再由 Go 对比同一输入。
// 用法：
//
//	go run ./cmd/charset gen   <输入.json>
//	node cmd/charset/decode.mjs <输入.json> <Node 输出.json>
//	go run ./cmd/charset check <输入.json> <Node 输出.json>
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type testCase struct {
	Label string `json:"label"`
	Hex   string `json:"hex"`
	Kind  string `json:"kind"` // label 标签识别；bom；valid 合法编码文本；random 随机字节；special 特殊序列
}

type nodeResult struct {
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

// 来自 Encoding Standard 的标签（含大小写与空白变体、未知标签），验证「标签是否被识别」两边一致。
var labels = strings.Fields(`utf-8 UTF-8 utf8 unicode-1-1-utf-8 gbk GBK gb2312 gb_2312 gb18030 chinese csgb2312 x-gbk big5 big5-hkscs cn-big5 x-x-big5 shift_jis shift-jis sjis ms932 windows-31j x-sjis euc-jp x-euc-jp euc-kr korean ks_c_5601-1987 windows-949 iso-2022-jp iso-8859-1 latin1 iso8859-1 l1 ascii us-ascii windows-1252 cp1252 x-cp1252 windows-1251 cp1251 windows-1250 windows-1253 windows-1254 windows-1255 windows-1256 windows-1257 windows-1258 koi8-r koi8-u iso-8859-2 iso-8859-5 iso-8859-7 iso-8859-15 macintosh x-mac-cyrillic windows-874 tis-620 utf-16le utf-16 utf-16be ucs-2 x-user-defined replacement gb-18030 UTF8 utf-8; foo bar x-unknown`)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("用法: charset gen|check ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "gen":
		gen(os.Args[2])
	case "check":
		check(os.Args[2], os.Args[3])
	}
}

// goDecode 模拟 TextDecoder(label,{fatal:true}).decode(bytes)；标签无法识别或字节非法时 ok=false。
var fix = os.Getenv("CHARSET_FIX") == "1"

func goDecode(label string, b []byte) (string, bool) {
	enc, err := htmlindex.Get(strings.TrimSpace(strings.ToLower(label)))
	if err != nil {
		return "", false
	}
	name, _ := htmlindex.Name(enc)
	if fix {
		// WHATWG：gbk/gb2312 标签使用 GB18030 解码器；UTF-16 默认丢弃开头 BOM
		if name == "gbk" {
			enc = simplifiedchinese.GB18030
		}
	}
	if name == "utf-8" {
		if !utf8.Valid(b) {
			return "", false
		}
		return strings.TrimPrefix(string(b), "\uFEFF"), true
	}
	out, err := enc.NewDecoder().Bytes(b)
	if err != nil || strings.ContainsRune(string(out), utf8.RuneError) {
		return "", false
	}
	if fix && strings.HasPrefix(name, "utf-16") {
		return strings.TrimPrefix(string(out), "\uFEFF"), true
	}
	return string(out), true
}

func gen(path string) {
	r := rand.New(rand.NewSource(20261111))
	samples := []string{
		"归知 GuiZhi 知识库 — “引号” €100 ¥ ©", "こんにちは世界 ｱｲｳ 日本語", "안녕하세요 한국어", "Привет мир", "café ñ ü Ωmega", "繁體中文：資訊與網路", "𠮷野家 𝒳 😀",
	}
	encs := strings.Fields("gbk gb18030 big5 shift_jis euc-jp euc-kr windows-1252 windows-1251 iso-8859-2 koi8-r utf-8")
	var cases []testCase
	for _, l := range labels {
		cases = append(cases, testCase{l, "", "label"}, testCase{l, hex.EncodeToString([]byte("abc")), "valid"}, testCase{l, "efbbbf616263", "bom"}, testCase{l, "fffe6100", "bom"}, testCase{l, "6100", "valid"})
	}
	for _, e := range encs {
		enc, _ := htmlindex.Get(e)
		for _, s := range samples {
			b, err := encoder(enc).Bytes([]byte(s))
			if err == nil {
				cases = append(cases, testCase{e, hex.EncodeToString(b), "valid"})
			}
			// 每个字符单独编码，避免一个不可编码字符使整句丢弃
			for _, ch := range s {
				if b, err := encoder(enc).Bytes([]byte(string(ch))); err == nil {
					cases = append(cases, testCase{e, hex.EncodeToString(b), "valid"})
				}
			}
		}
		for i := 0; i < 600; i++ {
			n := 1 + r.Intn(12)
			b := make([]byte, n)
			for j := range b {
				if r.Intn(4) == 0 {
					b[j] = byte(r.Intn(0x80))
				} else {
					b[j] = byte(0x80 + r.Intn(0x80))
				}
			}
			cases = append(cases, testCase{e, hex.EncodeToString(b), "random"})
		}
	}
	// GB18030 四字节序列与欧元号等 WHATWG 特有映射
	for _, h := range []string{"80", "a2e3", "81308130", "8431a439", "e3329a35", "8135f437", "a8bf", "fe50"} {
		for _, e := range []string{"gbk", "gb2312", "gb18030"} {
			cases = append(cases, testCase{e, h, "special"})
		}
	}
	data, _ := json.Marshal(cases)
	_ = os.WriteFile(path, data, 0o644)
	fmt.Println("生成用例", len(cases))
}

func encoder(e encoding.Encoding) *encoding.Encoder { return e.NewEncoder() }

func check(inPath, outPath string) {
	var cases []testCase
	var res []nodeResult
	in, _ := os.ReadFile(inPath)
	out, _ := os.ReadFile(outPath)
	_ = json.Unmarshal(in, &cases)
	if err := json.Unmarshal(out, &res); err != nil || len(res) != len(cases) {
		fmt.Println("输出与输入数量不符", err)
		os.Exit(1)
	}
	// 方向：goFail=Go 判失败而 Node 成功（产品会回退到渲染路径，安全降级）；nodeFail=Go 成功而 Node 失败（会得到乱码）；textDiff=两边都成功但文本不同（静默损坏）
	type cnt struct{ total, same, goFail, nodeFail, textDiff int }
	byKind := map[string]*cnt{}
	byLabel := map[string]int{}
	shown := map[string]int{}
	for i, c := range cases {
		b, _ := hex.DecodeString(c.Hex)
		gt, gok := goDecode(c.Label, b)
		nr := res[i]
		k := byKind[c.Kind]
		if k == nil {
			k = &cnt{}
			byKind[c.Kind] = k
		}
		k.total++
		dir := ""
		switch {
		case gok == nr.OK && (!gok || gt == nr.Text):
			k.same++
			continue
		case !gok && nr.OK:
			k.goFail++
			dir = "goFail"
		case gok && !nr.OK:
			k.nodeFail++
			dir = "nodeFail"
		default:
			k.textDiff++
			dir = "textDiff"
		}
		byLabel[strings.ToLower(c.Label)+"/"+dir]++
		if (c.Kind == "valid" && !strings.Contains(c.Label, "big5")) || (c.Kind != "random" && c.Kind != "valid" && shown[dir] < 4) {
			shown[dir]++
			fmt.Printf("  [%s/%s] label=%q hex=%s node(ok=%v %q) go(ok=%v %q)\n", c.Kind, dir, c.Label, c.Hex, nr.OK, nr.Text, gok, gt)
		}
	}
	fmt.Printf("共 %d 例\n%-8s %6s %6s %8s %9s %9s\n", len(cases), "类型", "总数", "相同", "Go判失败", "Go成功Node失败", "文本不同")
	for _, kind := range []string{"label", "valid", "bom", "special", "random"} {
		if k := byKind[kind]; k != nil {
			fmt.Printf("%-8s %6d %6d %8d %9d %9d\n", kind, k.total, k.same, k.goFail, k.nodeFail, k.textDiff)
		}
	}
	fmt.Println("按标签与方向的不一致数：")
	for l, n := range byLabel {
		fmt.Printf("  %-28s %d\n", l, n)
	}
}
