package parity

import "testing"

// frozenThresholdsDigest 是冻结时 thresholds.json（统一为 LF 换行）的 SHA-256。
// 改动阈值文件会让 TestThresholdsFrozen 变红：放宽任何阈值都属于需要用户裁定的 C 类事项
// （docs/agents/viva-ui-development.md §2.5、docs/agents/viva-parity-ledger.md）；收紧后更新这里并在台账登记。
const frozenThresholdsDigest = "5128b9532a966886abec487d63634acf3d9aa20e94b9e7098a8bd948ecbf53c0"

func TestThresholdsFrozen(t *testing.T) {
	th, err := DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	if th.FrozenAt == "" {
		t.Skip("阈值尚在校准，未冻结")
	}
	if got := ThresholdsDigest(); got != frozenThresholdsDigest {
		t.Fatalf("thresholds.json 与冻结值不符：摘要 %s，冻结值 %s。放宽阈值需要用户裁定（C 类）；收紧后更新 frozenThresholdsDigest 并在台账登记", got, frozenThresholdsDigest)
	}
}

// TestThresholdsNeverLooserThanInitial 把契约里固定的数值写成不变量：
// 外框容差固定 1.0 逻辑像素、稳定性系数固定 0.5、像素阈值只能比初始值更严（初始：容忍度 12/255、遮罩外扩 1、差异比例 1%）。
func TestThresholdsNeverLooserThanInitial(t *testing.T) {
	th, err := DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	if th.Metrics.MaxDeltaPx != 1.0 {
		t.Errorf("metrics.maxDeltaPx 固定为 1.0，得到 %v", th.Metrics.MaxDeltaPx)
	}
	if th.Stability.Factor != 0.5 {
		t.Errorf("stability.factor 固定为 0.5，得到 %v", th.Stability.Factor)
	}
	if th.Pixel.Tolerance > 12 || th.Pixel.Tolerance < 0 {
		t.Errorf("pixel.tolerance 不得大于初始值 12，得到 %d", th.Pixel.Tolerance)
	}
	if th.Pixel.MaskPad > 1 || th.Pixel.MaskPad < 0 {
		t.Errorf("pixel.maskPad 不得大于初始值 1，得到 %d", th.Pixel.MaskPad)
	}
	if th.Pixel.MaxDiffRatio > 0.01 || th.Pixel.MaxDiffRatio <= 0 {
		t.Errorf("pixel.maxDiffRatio 不得大于初始值 0.01，得到 %v", th.Pixel.MaxDiffRatio)
	}
	if th.Metrics.InkMaxDeltaY != 0 {
		t.Errorf("metrics.inkMaxDeltaY 固定为 0（基线差一个像素就是缺陷），得到 %d", th.Metrics.InkMaxDeltaY)
	}
	if th.Metrics.InkMaxDeltaX < 0 || th.Metrics.InkMaxDeltaX > 1 || th.Metrics.InkMaxDeltaXFractional < 0 || th.Metrics.InkMaxDeltaXFractional > 1 {
		t.Errorf("metrics.inkMaxDeltaX / inkMaxDeltaXFractional 不得超过初始值 1，得到 %d / %d", th.Metrics.InkMaxDeltaX, th.Metrics.InkMaxDeltaXFractional)
	}
	if th.Metrics.WrappedTextPitchPx < 0 || th.Metrics.WrappedTextPitchPx > th.Metrics.MaxDeltaPx {
		t.Errorf("metrics.wrappedTextPitchPx 应在 [0, maxDeltaPx] 内，得到 %v", th.Metrics.WrappedTextPitchPx)
	}
	if th.Metrics.InkMassMin < 0.8 || th.Metrics.InkMassMax > 1.25 {
		t.Errorf("metrics.inkMass 范围不得比初始 [0.8, 1.25] 更宽，得到 [%v, %v]", th.Metrics.InkMassMin, th.Metrics.InkMassMax)
	}
}
