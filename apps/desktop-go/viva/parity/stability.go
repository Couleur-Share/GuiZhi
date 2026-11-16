package parity

import (
	"encoding/json"
	"fmt"
	"image"
)

// StabilityResult 是同一场景同一变体两次 React 采集的比较结果。
type StabilityResult struct {
	Scene      string  `json:"scene"`
	Variant    string  `json:"variant"`
	DiffPixels int     `json:"diffPixels"`
	DiffRatio  float64 `json:"diffRatio"`
	Issues     []Issue `json:"issues"`
}

// Stable 报告两次采集是否一致。
func (r StabilityResult) Stable() bool { return len(r.Issues) == 0 }

// CompareStability 比较同一场景同一变体的两次 React 采集（§2.4 的稳定性要求）：
// 可访问树、文字、绘制与字体取证必须逐字一致，外框与矩形一个小数都不能差；
// 图片逐像素比较（不遮罩、容忍度 0），差异占比必须小于「像素阈值 × 稳定性系数」。
// 不稳定就先修采集（压动画、等字体、停光标……），不放宽阈值。
func CompareStability(a, b *Facts, imgA, imgB image.Image, th Thresholds) StabilityResult {
	res := StabilityResult{Scene: a.Scene, Variant: a.Variant.Key(), Issues: []Issue{}}
	add := func(kind, subject, format string, args ...any) {
		res.Issues = append(res.Issues, Issue{Layer: LayerL1, Kind: kind, Subject: subject, Message: fmt.Sprintf(format, args...)})
	}
	if a.Scene != b.Scene || a.Variant != b.Variant {
		add("pairing", a.Scene, "两次采集不是同一场景同一变体：%s/%s 与 %s/%s", a.Scene, a.Variant.Key(), b.Scene, b.Variant.Key())
		return res
	}
	if a.Region != b.Region {
		add("region", a.Scene, "区域不同：%+v 与 %+v", a.Region, b.Region)
	}
	diffSection := func(kind string, x, y any) {
		xj, _ := json.Marshal(x)
		yj, _ := json.Marshal(y)
		if string(xj) != string(yj) {
			add(kind, a.Scene, "%s 两次采集不一致（首个差异位置：%s）", kind, firstDiff(xj, yj))
		}
	}
	diffSection("nodes", a.Nodes, b.Nodes)
	diffSection("texts", a.Texts, b.Texts)
	diffSection("paints", a.Paints, b.Paints)
	diffSection("fonts", a.Fonts, b.Fonts)
	if a.Aria != b.Aria {
		add("aria", a.Scene, "ARIA 快照不一致")
	}

	if imgA == nil || imgB == nil {
		add("image", a.Scene, "缺少图片")
		return res
	}
	ab, bb := imgA.Bounds(), imgB.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		add("image", a.Scene, "图片尺寸不同：%dx%d 与 %dx%d", ab.Dx(), ab.Dy(), bb.Dx(), bb.Dy())
		return res
	}
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			if rgba8(imgA, ab.Min.X+x, ab.Min.Y+y) != rgba8(imgB, bb.Min.X+x, bb.Min.Y+y) {
				res.DiffPixels++
			}
		}
	}
	total := ab.Dx() * ab.Dy()
	if total > 0 {
		res.DiffRatio = float64(res.DiffPixels) / float64(total)
	}
	limit := th.Pixel.MaxDiffRatio * th.Stability.Factor
	if res.DiffRatio >= limit && res.DiffPixels > 0 {
		add("pixels", a.Scene, "两次采集的像素差异 %d / %d = %.4f%%，要求小于 %.4f%%（像素阈值 × 稳定性系数 %.2f）",
			res.DiffPixels, total, res.DiffRatio*100, limit*100, th.Stability.Factor)
	}
	return res
}

// firstDiff 返回两段 JSON 第一处不同的位置附近的片段，便于定位。
func firstDiff(a, b []byte) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-30, 0)
	return fmt.Sprintf("字节 %d，「%s」对「%s」", i, clip(a, lo, i+40), clip(b, lo, i+40))
}

func clip(b []byte, lo, hi int) string {
	hi = min(hi, len(b))
	if lo >= hi {
		return ""
	}
	return string(b[lo:hi])
}
