package kit

import "strings"

// 几段相邻文字拼成的等宽原文的折行（台账 B-063）。
//
// 渲染层的 `<pre className="whitespace-pre-wrap">{label}{value}</pre>` 在 DOM 里是两个相邻的文本节点，
// 视觉上是一段会折行的话：折行按拼起来的整段做（标签与内容之间没有断行边界），第二行回到 pre 的左缘。
// 对照的文字序列与文字行矩形都按文本节点各记一条，所以画的时候要把每一行再拆回各段（MonoParagraphRuns）。

// MonoFrag 是折好的一行里属于第 Run 段的那一截。
type MonoFrag struct {
	Run  int    // 属于 runs 里的第几段
	Text string // 这一段在本行里的那一截，非空
}

// WrapMonoRuns 把 runs 拼成一段，按 WrapMono 的规则在 width 宽的盒子里折行（font-mono，字号 size），
// 再按各段的范围把每一行拆回各段。返回的每一行是这一行里各段的那一截（按段的先后）；空行是空切片。
// 各行的文字连起来（硬换行处补回换行符）正好是拼起来的整段（制表符已按列展开、回车换行已统一成换行）。
func WrapMonoRuns(runs []string, width, size float32) [][]MonoFrag {
	joined := normalizeMono(strings.Join(runs, ""))
	// 各段在归一化之后的整段里的结束位置（字节）：归一化是自左向右的逐字符变换，前缀的归一化结果就是整段归一化结果的前缀。
	ends := make([]int, len(runs))
	for i := range runs {
		ends[i] = len(normalizeMono(strings.Join(runs[:i+1], "")))
	}
	lines := WrapMono(joined, width, size)
	out := make([][]MonoFrag, len(lines))
	pos := 0
	for i, line := range lines {
		if pos+len(line) > len(joined) || joined[pos:pos+len(line)] != line {
			// 不该发生（WrapMono 的各行是整段的连续子串）：退回「整行算在第一个非空段名下」，保证文字不丢。
			out[i] = fallbackFrags(runs, line)
			continue
		}
		end := pos + len(line)
		start := 0
		for k := range runs {
			lo, hi := max(pos, start), min(end, ends[k])
			start = ends[k]
			if lo < hi {
				out[i] = append(out[i], MonoFrag{Run: k, Text: joined[lo:hi]})
			}
		}
		pos = end
		if pos < len(joined) && joined[pos] == '\n' { // 硬换行：换行符不进任何一行
			pos++
		}
	}
	return out
}

func fallbackFrags(runs []string, line string) []MonoFrag {
	if line == "" {
		return nil
	}
	for k, r := range runs {
		if r != "" {
			return []MonoFrag{{Run: k, Text: line}}
		}
	}
	return []MonoFrag{{Run: 0, Text: line}}
}

// normalizeMono 做 WrapMono 开头的那几步：回车换行统一成换行，每个硬行里的制表符按列展开。
func normalizeMono(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if !strings.ContainsAny(s, "\t\n") {
		return s
	}
	hard := strings.Split(s, "\n")
	for i, h := range hard {
		hard[i] = expandTabs(h)
	}
	return strings.Join(hard, "\n")
}
