package parity

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Thresholds 是对照用的阈值。校准一次后冻结（frozenAt 非空），之后任何放宽都属于需要用户裁定的 C 类事项，
// thresholds_test.go 用 SHA-256 守住冻结值（docs/agents/viva-ui-development.md §2.5）。
type Thresholds struct {
	Metrics struct {
		// MaxDeltaPx 是按「角色 + 可访问名称」对齐的两个外框，位置与尺寸各自允许的最大差（逻辑像素）。固定为 1.0。
		MaxDeltaPx float64 `json:"maxDeltaPx"`
		// WrappedTextPitchPx 是折行文字相邻两行的行距（行盒顶之差）允许的差（逻辑像素）：行距差意味着行高不对。
		WrappedTextPitchPx float64 `json:"wrappedTextPitchPx"`
		// InkMaxDeltaX、InkMaxDeltaY 是同一行文字墨迹剖面最佳对齐位移的上限（整像素）。上下与左右都取 0：差一个像素就是缺陷。
		// InkMaxDeltaXFractional 只对左边缘落在小数像素位置的文字放宽水平位移：Viva 的文字按整设备像素落位
		// （四舍五入），渲染层是子像素定位，所以这类文字最多差半个像素（引擎限制，台账 A 类，见 kit/snap_test.go）。
		InkMaxDeltaX           int `json:"inkMaxDeltaX"`
		InkMaxDeltaXFractional int `json:"inkMaxDeltaXFractional"`
		InkMaxDeltaY           int `json:"inkMaxDeltaY"`
		// InkMassMin、InkMassMax 是同一行文字墨迹总量比（Viva / React）的允许范围，用来发现字重、颜色或字号不对。
		InkMassMin float64 `json:"inkMassMin"`
		InkMassMax float64 `json:"inkMassMax"`
	} `json:"metrics"`
	Pixel struct {
		// Tolerance 是单通道差异容忍度（0..255）；超过即记为差异像素。
		Tolerance int `json:"tolerance"`
		// MaskPad 是文字区域遮罩向外扩的像素。
		MaskPad int `json:"maskPad"`
		// MaxDiffRatio 是差异像素占未遮罩像素的比例上限。
		MaxDiffRatio float64 `json:"maxDiffRatio"`
	} `json:"pixel"`
	Stability struct {
		// Factor 是采集稳定性：同一场景 React 采两次，差异必须小于阈值乘这个系数；不稳定就修采集，不放宽阈值。固定为 0.5。
		Factor float64 `json:"factor"`
	} `json:"stability"`
	// FrozenAt 是校准冻结的日期；空串表示还在校准。
	FrozenAt string `json:"frozenAt"`
}

//go:embed thresholds.json
var thresholdsJSON []byte

// DefaultThresholds 读取内嵌的 thresholds.json。
func DefaultThresholds() (Thresholds, error) {
	var t Thresholds
	if err := json.Unmarshal(thresholdsJSON, &t); err != nil {
		return t, fmt.Errorf("thresholds.json 解析失败：%w", err)
	}
	return t, nil
}

// ThresholdsDigest 是内嵌阈值文件（统一换行后）的 SHA-256，守卫测试与报告都用它标识所用阈值。
func ThresholdsDigest() string {
	sum := sha256.Sum256(normalizeEOL(thresholdsJSON))
	return hex.EncodeToString(sum[:])
}

func normalizeEOL(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' && i+1 < len(b) && b[i+1] == '\n' {
			continue
		}
		out = append(out, b[i])
	}
	return out
}
