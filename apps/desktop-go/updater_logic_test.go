package main

// 更新器纯逻辑差分测试：golden.updater 由真实 version.ts / changelog.ts / updater-channel.ts 生成。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type updaterGolden struct {
	Updater struct {
		Compare []struct {
			A string `json:"a"`
			B string `json:"b"`
			R int    `json:"r"`
		} `json:"compare"`
		Normalize []struct {
			V   string `json:"v"`
			N   string `json:"n"`
			Pre bool   `json:"pre"`
			Tag string `json:"tag"`
		} `json:"normalize"`
		Pick []struct {
			Name   string                   `json:"name"`
			List   []githubReleaseCandidate `json:"list"`
			Choice *previewFeedChoice       `json:"choice"`
			Tag    string                   `json:"tag"`
		} `json:"pick"`
		Changelog []struct {
			Name     string `json:"name"`
			Text     string `json:"text"`
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
			Latest string `json:"latest"`
			Ranges []struct {
				NV string `json:"nv"`
				CV string `json:"cv"`
				R  string `json:"r"`
			} `json:"ranges"`
		} `json:"changelog"`
	} `json:"updater"`
}

func TestUpdaterLogicParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g updaterGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	u := g.Updater
	if len(u.Compare) < 1000 {
		t.Fatalf("golden 场景太少：%d", len(u.Compare))
	}
	sign := func(n int) int {
		switch {
		case n > 0:
			return 1
		case n < 0:
			return -1
		}
		return 0
	}
	bad := 0
	for _, c := range u.Compare {
		if got := sign(compareVersions(c.A, c.B)); got != c.R {
			if bad++; bad <= 15 {
				t.Errorf("compareVersions(%q, %q) = %d，TS = %d", c.A, c.B, got, c.R)
			}
		}
	}
	if bad > 0 {
		t.Errorf("版本比较共 %d 处不一致", bad)
	}
	for _, c := range u.Normalize {
		if normalizeVersion(c.V) != c.N || isPrereleaseVersion(c.V) != c.Pre || getReleaseTagForVersion(c.V) != c.Tag {
			t.Errorf("normalize(%q)：got (%q,%v,%q) want (%q,%v,%q)", c.V, normalizeVersion(c.V), isPrereleaseVersion(c.V), getReleaseTagForVersion(c.V), c.N, c.Pre, c.Tag)
		}
	}
	for _, c := range u.Pick {
		got := pickPreviewFeedRelease(c.List)
		if (got == nil) != (c.Choice == nil) || (got != nil && *got != *c.Choice) {
			t.Errorf("pick %s：got %+v want %+v", c.Name, got, c.Choice)
		}
		if tag := releaseTagForPreviewFeed(got); tag != c.Tag {
			t.Errorf("pick %s 的 tag：got %q want %q", c.Name, tag, c.Tag)
		}
	}
	for _, c := range u.Changelog {
		var vs []string
		for _, s := range parseChangelogVersions(c.Text) {
			vs = append(vs, s.Version)
		}
		if len(vs) != len(c.Versions) {
			t.Errorf("changelog %s 版本节数 got %d want %d", c.Name, len(vs), len(c.Versions))
		} else {
			for i := range vs {
				if vs[i] != c.Versions[i].Version {
					t.Errorf("changelog %s 第 %d 节版本 got %q want %q", c.Name, i, vs[i], c.Versions[i].Version)
				}
			}
		}
		if got := extractLatestChangelogSection(c.Text); got != c.Latest {
			t.Errorf("changelog %s latest：\n got=%q\nwant=%q", c.Name, got, c.Latest)
		}
		for _, r := range c.Ranges {
			if got := extractChangelogRange(c.Text, r.NV, r.CV); got != r.R {
				t.Errorf("changelog %s range(%s,%s)：\n got=%q\nwant=%q", c.Name, r.NV, r.CV, got, r.R)
			}
		}
	}
}
