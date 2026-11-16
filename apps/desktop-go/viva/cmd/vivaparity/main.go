// vivaparity 把 Viva 屏幕与 React 参照逐变体对照，输出 L1–L3 报告与差异图。
//
// 用法（在 apps/desktop-go 下）：
//
//	go run ./viva/cmd/vivaparity -scene settings.general [-react <目录>] [-out <目录>] [-variant <子串>]
//	go run ./viva/cmd/vivaparity -scene settings.general -stability [-react <目录>] [-react2 <目录>]
//
// -stability 是采集稳定性检查：不渲染 Viva，只比较同一场景的两次 React 采集。给了 -react2 就比两个目录里同名变体；
// 没给就比 -react 目录里每个变体的 <场景>.r1.* 与 <场景>.r2.*（collect-react.mjs --repeat 2 的产物）。
//
// React 参照由 scripts/parity/collect-react.mjs 产出（<react>/<变体键>/<场景>.facts.json 与 .png）。
// 产物写到 <out>/<变体键>/：<场景>.viva.png、.viva.facts.json、.report.json、.report.md、.diff.png（react | viva | diff 三联图）。
// 任何变体未通过则以退出码 1 结束。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"guizhi-mygo-shell/viva/parity"
	"guizhi-mygo-shell/viva/parity/scenes"
)

func benchDir() string {
	if v := os.Getenv("GUIZHI_BENCH_DIR"); v != "" {
		return v
	}
	return `D:\Project\GuiZhi-bench`
}

func main() {
	scene := flag.String("scene", "", "场景 ID（必填）")
	reactDir := flag.String("react", filepath.Join(benchDir(), "parity", "react"), "React 参照目录")
	outDir := flag.String("out", filepath.Join(benchDir(), "parity", "viva"), "输出目录")
	filter := flag.String("variant", "", "只跑变体键包含该子串的变体")
	stability := flag.Bool("stability", false, "采集稳定性检查：只比较两次 React 采集")
	react2 := flag.String("react2", "", "稳定性检查的第二个 React 目录；空表示比 -react 里的 .r1 与 .r2")
	flag.Parse()
	if *scene == "" {
		fmt.Fprintln(os.Stderr, "缺少 -scene；已登记：", strings.Join(scenes.IDs(), ", "))
		os.Exit(2)
	}
	runner := func() error { return run(*scene, *reactDir, *outDir, *filter) }
	if *stability {
		runner = func() error { return runStability(*scene, *reactDir, *react2, *outDir, *filter) }
	}
	if err := runner(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(sceneID, reactDir, outDir, filter string) error {
	scene, err := scenes.Get(sceneID)
	if err != nil {
		return err
	}
	th, err := parity.DefaultThresholds()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(reactDir)
	if err != nil {
		return fmt.Errorf("读不到 React 参照目录：%w", err)
	}
	var variants []string
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), filter) {
			continue
		}
		if _, err := os.Stat(filepath.Join(reactDir, e.Name(), sceneID+".facts.json")); err == nil {
			variants = append(variants, e.Name())
		}
	}
	sort.Strings(variants)
	if len(variants) == 0 {
		return fmt.Errorf("%s 下没有场景 %s 的参照（先运行 scripts/parity/collect-react.mjs）", reactDir, sceneID)
	}
	failed := 0
	for _, key := range variants {
		rep, err := compareVariant(scene, filepath.Join(reactDir, key), filepath.Join(outDir, key), th)
		if err != nil {
			return fmt.Errorf("%s：%w", key, err)
		}
		status := "通过"
		if !rep.Pass {
			status = "未通过"
			failed++
		}
		fmt.Printf("%-44s %s  L1 问题 %d  L2 问题 %d  L3 比例 %.4f%%（问题 %d）\n",
			key, status, rep.Failed(parity.LayerL1), rep.Failed(parity.LayerL2), rep.L3.Ratio*100, rep.Failed(parity.LayerL3))
		if !rep.Pass {
			for i, is := range rep.Issues {
				if i >= 12 {
					fmt.Printf("    …另有 %d 项，见报告\n", len(rep.Issues)-i)
					break
				}
				fmt.Printf("    [%s/%s] %s：%s\n", is.Layer, is.Kind, is.Subject, is.Message)
			}
		}
	}
	fmt.Printf("共 %d 个变体，未通过 %d；阈值 %s（%s）\n", len(variants), failed, parity.ThresholdsDigest()[:12], frozenText(th))
	if failed > 0 {
		return fmt.Errorf("有 %d 个变体未通过", failed)
	}
	return nil
}

func frozenText(th parity.Thresholds) string {
	if th.FrozenAt == "" {
		return "校准中，未冻结"
	}
	return "冻结于 " + th.FrozenAt
}

func compareVariant(scene parity.Scene, reactVariantDir, outVariantDir string, th parity.Thresholds) (*parity.Report, error) {
	reactFacts, err := parity.LoadFacts(filepath.Join(reactVariantDir, scene.ID+".facts.json"))
	if err != nil {
		return nil, err
	}
	reactImg, err := parity.LoadPNG(filepath.Join(reactVariantDir, scene.ID+".png"))
	if err != nil {
		return nil, err
	}
	vivaFacts, vivaImg, err := parity.Render(scene, reactFacts.Variant, reactFacts.Region)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outVariantDir, 0o755); err != nil {
		return nil, err
	}
	base := filepath.Join(outVariantDir, scene.ID)
	if err := vivaFacts.Save(base + ".viva.facts.json"); err != nil {
		return nil, err
	}
	if err := parity.WritePNG(base+".viva.png", vivaImg); err != nil {
		return nil, err
	}
	rep, montage := parity.Compare(parity.Inputs{React: reactFacts, Viva: vivaFacts, ReactImg: reactImg, VivaImg: vivaImg, Waivers: scene.Waivers}, th)
	if montage != nil {
		if err := parity.WritePNG(base+".diff.png", montage); err != nil {
			return nil, err
		}
	}
	data, err := json.MarshalIndent(rep, "", " ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(base+".report.json", append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(base+".report.md", []byte(rep.Markdown()), 0o644); err != nil {
		return nil, err
	}
	return rep, nil
}

// runStability 比较同一场景的两次 React 采集，任何变体不稳定则返回错误。
func runStability(sceneID, dirA, dirB, outDir, filter string) error {
	th, err := parity.DefaultThresholds()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dirA)
	if err != nil {
		return fmt.Errorf("读不到 React 参照目录：%w", err)
	}
	// 文件名后缀：同目录比较时是 .r1 / .r2，跨目录比较时两边都没有后缀。
	suffixA, suffixB, dirB2 := ".r1", ".r2", dirA
	if dirB != "" {
		suffixA, suffixB, dirB2 = "", "", dirB
	}
	var results []parity.StabilityResult
	unstable := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), filter) {
			continue
		}
		a := filepath.Join(dirA, e.Name(), sceneID+suffixA)
		b := filepath.Join(dirB2, e.Name(), sceneID+suffixB)
		if _, err := os.Stat(a + ".facts.json"); err != nil {
			continue
		}
		if _, err := os.Stat(b + ".facts.json"); err != nil {
			return fmt.Errorf("%s：找不到第二次采集 %s", e.Name(), b+".facts.json")
		}
		fa, err := parity.LoadFacts(a + ".facts.json")
		if err != nil {
			return err
		}
		fb, err := parity.LoadFacts(b + ".facts.json")
		if err != nil {
			return err
		}
		ia, err := parity.LoadPNG(a + ".png")
		if err != nil {
			return err
		}
		ib, err := parity.LoadPNG(b + ".png")
		if err != nil {
			return err
		}
		res := parity.CompareStability(fa, fb, ia, ib, th)
		results = append(results, res)
		status := "稳定"
		if !res.Stable() {
			status = "不稳定"
			unstable++
		}
		fmt.Printf("%-44s %s  像素差异 %d（%.4f%%）\n", e.Name(), status, res.DiffPixels, res.DiffRatio*100)
		for _, is := range res.Issues {
			fmt.Printf("    [%s] %s：%s\n", is.Kind, is.Subject, is.Message)
		}
	}
	if len(results) == 0 {
		return fmt.Errorf("%s 下没有场景 %s 可比较的采集（%s）", dirA, sceneID, "是否已用 --repeat 2 采集")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", " ")
	if err != nil {
		return err
	}
	report := filepath.Join(outDir, "stability-"+sceneID+".json")
	if err := os.WriteFile(report, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("共 %d 个变体，不稳定 %d；报告 %s\n", len(results), unstable, report)
	if unstable > 0 {
		return fmt.Errorf("有 %d 个变体的采集不稳定", unstable)
	}
	return nil
}
