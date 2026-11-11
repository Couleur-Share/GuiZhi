package main

// 配图风格预设文件（config/illustration-styles.json）的读写。
// 对应 packages/core/src/illustration-styles.ts 的 CoreIllustrationStyleService。
//
// 配图功能本身已冻结、不迁移；这里只保留配置迁移（config:export/apply）要用的那部分：
// 读取（含首次播种与新内置风格的增量补齐）、整份回写（严格校验）。
// 内置预设来自 TS 源码的导出（illustration_builtin.json，由 spikes/mygo-db/dump-builtin-styles.mts 生成）。

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

//go:embed illustration_builtin.json
var illustrationBuiltinJSON []byte

const (
	illustrationStylesFile    = "illustration-styles.json"
	illustrationStylesVersion = 2
	illustrationMaxShots      = 12
	illustrationMaxLabels     = 10
)

var illustrationAspectRatios = []string{"16:9", "4:3", "1:1"}

// STYLES_README：JSON 没有注释，只能占一个字段。与 TS 逐字一致。
var illustrationReadme = []any{
	"归知正文配图的风格预设。改完保存即生效，无需重启。",
	"visualDna：画法、配色、留白，会原样拼进生图提示词（写英文对图像模型更稳）。",
	"character：每张图都要出场并承担核心动作的固定角色；留空则画面里不要求有角色。",
	"negative：明确排除的观感，同样拼进提示词。",
	"maxLabels：单张图最多几处文字标注。图像模型写中文容易出错字，标注越少越稳。",
	"group：选择器里的分组名，自由文本，留空则排在最前。",
	"dismissedBuiltIns：被你删掉的内置风格 id。应用更新带来的新内置风格会自动补进 styles，" +
		"但这个名单里的不会再回来——想要回来，删掉对应的 id 即可。",
}

func illustrationBuiltins() []*omap {
	v, err := parseOrdered(illustrationBuiltinJSON)
	if err != nil {
		panic("内置配图风格 JSON 损坏：" + err.Error())
	}
	arr, _ := v.([]any)
	out := make([]*omap, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(*omap); ok {
			out = append(out, m)
		}
	}
	return out
}

func styleString(m *omap, key string) string {
	if s, ok := m.vals[key].(string); ok {
		return s
	}
	return ""
}

func (l *Library) illustrationStylesPath() string {
	return filepath.Join(l.configDir, illustrationStylesFile)
}

func illustrationReadText(m *omap, key string) string {
	if s, ok := m.vals[key].(string); ok {
		return jsTrim(s)
	}
	return ""
}

// illustrationClamp 对应 clamp()：Math.trunc(Number(value))，非有限或 <1 回落，否则不超过 max。
func illustrationClamp(value any, fallback, max float64) float64 {
	if arr, ok := value.([]any); ok {
		value = jsArr(arr)
	}
	parsed := math.Trunc(jsToNumber(value))
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 1 {
		return fallback
	}
	return math.Min(parsed, max)
}

// normalizeIllustrationStyle 对应 normalizeStyle：缺 id 或画法返回 nil；字段顺序与 TS 一致。
func normalizeIllustrationStyle(raw any) *omap {
	src, ok := raw.(*omap)
	if !ok || src == nil {
		return nil
	}
	id := illustrationReadText(src, "id")
	dna := illustrationReadText(src, "visualDna")
	if id == "" || dna == "" {
		return nil
	}
	name := illustrationReadText(src, "name")
	if name == "" {
		name = id
	}
	aspect := "16:9"
	if s, ok := src.vals["aspectRatio"].(string); ok {
		for _, a := range illustrationAspectRatios {
			if a == s {
				aspect = s
			}
		}
	}
	return newOmap().
		set("id", id).
		set("name", name).
		set("description", illustrationReadText(src, "description")).
		set("group", illustrationReadText(src, "group")).
		set("visualDna", dna).
		set("character", illustrationReadText(src, "character")).
		set("negative", illustrationReadText(src, "negative")).
		set("aspectRatio", aspect).
		set("maxShots", illustrationClamp(src.vals["maxShots"], 5, illustrationMaxShots)).
		set("maxLabels", illustrationClamp(src.vals["maxLabels"], 5, illustrationMaxLabels))
}

// validateIllustrationStyle 对应 validateStyle：保存是主动操作，要说得出哪一条不行。
func validateIllustrationStyle(raw any, index int) (*omap, string) {
	src, ok := raw.(*omap)
	if !ok || src == nil {
		return nil, fmt.Sprintf("第 %d 项不是有效的风格", index+1)
	}
	label := illustrationReadText(src, "name")
	if label == "" {
		label = illustrationReadText(src, "id")
	}
	if label == "" {
		label = fmt.Sprintf("第 %d 项", index+1)
	}
	if illustrationReadText(src, "id") == "" {
		return nil, label + "：缺少 id"
	}
	if illustrationReadText(src, "visualDna") == "" {
		return nil, label + "：画法与配色不能为空"
	}
	style := normalizeIllustrationStyle(raw)
	if style == nil {
		return nil, label + "：无法解析"
	}
	return style, ""
}

func illustrationUniqueID(id string, used map[string]bool) string {
	if !used[id] {
		return id
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", id, suffix)
		if !used[candidate] {
			return candidate
		}
	}
}

func illustrationComputeDismissed(styles []*omap) []any {
	present := map[string]bool{}
	for _, s := range styles {
		present[styleString(s, "id")] = true
	}
	out := []any{}
	for _, b := range illustrationBuiltins() {
		if id := styleString(b, "id"); !present[id] {
			out = append(out, id)
		}
	}
	return out
}

func (l *Library) persistIllustrationStyles(styles []*omap, dismissed []any) error {
	list := make([]any, len(styles))
	for i, s := range styles {
		list[i] = s
	}
	doc := newOmap().
		set("kind", "guizhi-illustration-styles").
		set("version", float64(illustrationStylesVersion)).
		set("readme", illustrationReadme).
		set("dismissedBuiltIns", append([]any{}, dismissed...)).
		set("styles", list)
	text, err := stringifyIndent2(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.configDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.illustrationStylesPath(), []byte(text+"\n"), 0o644)
}

// readIllustrationStyles 对应 CoreIllustrationStyleService.read。
func (l *Library) readIllustrationStyles() []*omap {
	builtins := illustrationBuiltins()
	path := l.illustrationStylesPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 播种是后台行为，失败只记日志
			if perr := l.persistIllustrationStyles(builtins, nil); perr != nil {
				logAppErrorf("config-transfer", "写入内置配图风格预设", "%s", perr.Error())
			}
		}
		return builtins
	}
	parsed, err := parseOrdered(data)
	root, _ := parsed.(*omap)
	if err != nil || root == nil {
		return builtins // 无法解析：用内置预设兜底，不覆写文件
	}
	var styles []*omap
	if arr, ok := root.vals["styles"].([]any); ok {
		for _, e := range arr {
			if s := normalizeIllustrationStyle(e); s != nil {
				styles = append(styles, s)
			}
		}
	}
	if len(styles) == 0 {
		return builtins
	}
	dismissed := []any{}
	if arr, ok := root.vals["dismissedBuiltIns"].([]any); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				dismissed = append(dismissed, s)
			}
		}
	}
	known := map[string]bool{}
	for _, s := range styles {
		known[styleString(s, "id")] = true
	}
	for _, d := range dismissed {
		known[d.(string)] = true
	}
	var missing []*omap
	for _, b := range builtins {
		if !known[styleString(b, "id")] {
			missing = append(missing, b)
		}
	}
	if len(missing) == 0 {
		return styles
	}
	merged := append(append([]*omap{}, styles...), missing...)
	if perr := l.persistIllustrationStyles(merged, dismissed); perr != nil {
		logAppErrorf("config-transfer", "补齐内置配图风格", "%s", perr.Error())
	}
	return merged
}

// writeIllustrationStyles 对应 CoreIllustrationStyleService.write：成功返回规范化结果，失败返回原因。
func (l *Library) writeIllustrationStyles(raw any) (styles []*omap, errMsg string) {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil, "至少要保留一套配图风格"
	}
	used := map[string]bool{}
	for i, e := range arr {
		style, msg := validateIllustrationStyle(e, i)
		if msg != "" || style == nil {
			if msg == "" {
				msg = fmt.Sprintf("第 %d 项无法保存", i+1)
			}
			return nil, msg
		}
		style.set("id", illustrationUniqueID(styleString(style, "id"), used))
		used[styleString(style, "id")] = true
		styles = append(styles, style)
	}
	if err := l.persistIllustrationStyles(styles, illustrationComputeDismissed(styles)); err != nil {
		return nil, err.Error()
	}
	return styles, ""
}

// stringifyIndent2 对应 JSON.stringify(v, null, 2)：沿用已有的有序序列化，再套两空格缩进；
// 与 JS 一致，U+2028/2029 不转义。
func stringifyIndent2(v any) (string, error) {
	raw, err := marshalNoEscape(v)
	if err != nil {
		return "", err
	}
	return indentJSON(raw)
}

func indentJSON(raw []byte) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return "", err
	}
	return strings.ReplaceAll(strings.ReplaceAll(buf.String(), `\u2028`, "\u2028"), `\u2029`, "\u2029"), nil
}
