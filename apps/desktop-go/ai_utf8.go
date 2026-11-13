package main

import "strings"

// utf8StreamDecoder 对应 `new TextDecoder()` 的流式解码（WHATWG Encoding Standard 的 UTF-8 decoder）。
//
// 不能用 Go 自带的 utf8.DecodeRune 逐字节处理：非法序列怎么折成 U+FFFD 两边不同——
// WHATWG 按“最大合法前缀”折成一个，Go 的 `string([]byte)` 与 range 是每个坏字节一个。
// 流式场景还要跨块保留未完成的多字节序列，所以直接实现标准里的状态机。
type utf8StreamDecoder struct {
	cp          rune
	needed      int
	seen        int
	lower       byte
	upper       byte
	bomHandled  bool // TextDecoder 默认 ignoreBOM=false：只剥流开头的一个 BOM
	initialized bool
}

func (d *utf8StreamDecoder) reset() {
	d.cp, d.needed, d.seen = 0, 0, 0
	d.lower, d.upper = 0x80, 0xBF
}

func (d *utf8StreamDecoder) emit(sb *strings.Builder, r rune) {
	if !d.bomHandled {
		d.bomHandled = true
		if r == 0xFEFF {
			return
		}
	}
	sb.WriteRune(r)
}

// write 解码一块数据；未完成的序列留在状态里等下一块。final=true 时把残留折成 U+FFFD。
func (d *utf8StreamDecoder) write(p []byte, final bool) string {
	if !d.initialized {
		d.initialized = true
		d.reset()
	}
	var sb strings.Builder
	sb.Grow(len(p))
	for i := 0; i < len(p); {
		b := p[i]
		if d.needed == 0 {
			switch {
			case b <= 0x7F:
				d.emit(&sb, rune(b))
			case b >= 0xC2 && b <= 0xDF:
				d.needed, d.cp = 1, rune(b&0x1F)
			case b >= 0xE0 && b <= 0xEF:
				if b == 0xE0 {
					d.lower = 0xA0
				} else if b == 0xED {
					d.upper = 0x9F
				}
				d.needed, d.cp = 2, rune(b&0x0F)
			case b >= 0xF0 && b <= 0xF4:
				if b == 0xF0 {
					d.lower = 0x90
				} else if b == 0xF4 {
					d.upper = 0x8F
				}
				d.needed, d.cp = 3, rune(b&0x07)
			default:
				d.emit(&sb, 0xFFFD)
			}
			i++
			continue
		}
		if b < d.lower || b > d.upper {
			// 序列被打断：已读部分折成一个 U+FFFD，当前字节按新序列重新处理（不前进 i）
			d.reset()
			d.emit(&sb, 0xFFFD)
			continue
		}
		d.lower, d.upper = 0x80, 0xBF
		d.cp = d.cp<<6 | rune(b&0x3F)
		d.seen++
		i++
		if d.seen == d.needed {
			cp := d.cp
			d.reset()
			d.emit(&sb, cp)
		}
	}
	if final {
		if d.needed != 0 {
			d.reset()
			d.emit(&sb, 0xFFFD)
		}
		// 流结束后 TextDecoder 回到初始状态（BOM 判定也重置）
		d.bomHandled = false
	}
	return sb.String()
}
