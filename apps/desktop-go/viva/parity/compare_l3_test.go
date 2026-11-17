package parity

import "testing"

// 把差异像素按 8 邻接分片，并记下每片的最大通道差：小而强的差异（一条线、一圈环）要能从「最强」列表里看到。
func TestDiffClustersTrackMaxDeltaAndRankByBoth(t *testing.T) {
	const w, h = 20, 12
	diff := make([]bool, w*h)
	dist := make([]uint8, w*h)
	set := func(x, y int, d uint8) {
		diff[y*w+x] = true
		dist[y*w+x] = d
	}
	// 一大片弱差：4×4 = 16 个像素，差 30。
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			set(x, y, 30)
		}
	}
	// 一条 1×6 的强线：差 171。
	for y := 3; y < 9; y++ {
		set(10, y, 171)
	}
	// 与强线斜向相邻的一个像素属于同一片（8 邻接）。
	set(11, 9, 40)
	// 孤立的一个像素：差 90。
	set(18, 10, 90)

	all := diffClusters(diff, dist, w, h)
	if len(all) != 3 {
		t.Fatalf("应分成 3 片，得到 %d：%+v", len(all), all)
	}
	byPixels, byDelta := topClusters(all, 2)
	if len(byPixels) != 2 || byPixels[0].Pixels != 16 || byPixels[0].MaxDelta != 30 {
		t.Errorf("按像素数第一应是 16 像素、差 30 的弱片：%+v", byPixels)
	}
	if byPixels[1].Pixels != 7 {
		t.Errorf("按像素数第二应是线加斜邻像素共 7 个：%+v", byPixels)
	}
	if len(byDelta) != 2 || byDelta[0].MaxDelta != 171 || byDelta[0].Pixels != 7 {
		t.Errorf("按最大通道差第一应是强线：%+v", byDelta)
	}
	if byDelta[1].MaxDelta != 90 || byDelta[1].Pixels != 1 {
		t.Errorf("按最大通道差第二应是孤立的 90：%+v", byDelta)
	}
	line := byDelta[0]
	if line.X != 10 || line.Y != 3 || line.Width != 2 || line.Height != 7 {
		t.Errorf("强线片的外框应含斜邻像素（10,3 2×7）：%+v", line)
	}
}
