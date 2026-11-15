package aiflow

// jsx.go 与 V8 真实行为的对拍；夹具来自 aiflow-golden-text.test.ts。

import (
	"math"
	"reflect"
	"strconv"
	"testing"
)

func TestJSXGolden(t *testing.T) {
	var golden struct {
		Strings []struct {
			In           string   `json:"in"`
			Trim         string   `json:"trim"`
			TrimStart    string   `json:"trimStart"`
			Upper        string   `json:"upper"`
			Lower        string   `json:"lower"`
			NFKC         string   `json:"nfkc"`
			Collapse     string   `json:"collapse"`
			RemoveSpaces string   `json:"removeSpaces"`
			Fields       []string `json:"fields"`
			Length       int      `json:"length"`
			Slices       []struct {
				From int    `json:"from"`
				To   int    `json:"to"`
				Out  string `json:"out"`
			} `json:"slices"`
			Units []int `json:"units"`
		} `json:"strings"`
		Compare []struct {
			A   string `json:"a"`
			B   string `json:"b"`
			Out int    `json:"out"`
		} `json:"compare"`
		NumberString []struct {
			In  float64 `json:"in"`
			Out string  `json:"out"`
		} `json:"numberString"`
		ToNumber []struct {
			In       string   `json:"in"`
			OK       bool     `json:"ok"`
			Out      *float64 `json:"out"`
			Infinite int      `json:"infinite"`
		} `json:"toNumber"`
		Sorted []string `json:"sorted"`
	}
	loadGolden(t, "text", &golden)
	if len(golden.Strings) < 50 || len(golden.Compare) < 2000 {
		t.Fatalf("golden 规模异常：%d 个字符串，%d 个比较", len(golden.Strings), len(golden.Compare))
	}

	check := func(what, in, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s(%q)：Go=%q V8=%q", what, in, got, want)
		}
	}
	for _, c := range golden.Strings {
		check("trim", c.In, jsTrim(c.In), c.Trim)
		check("trimStart", c.In, jsTrimStart(c.In), c.TrimStart)
		check("upper", c.In, jsUpper(c.In), c.Upper)
		check("lower", c.In, jsLower(c.In), c.Lower)
		check("nfkc", c.In, jsNFKC(c.In), c.NFKC)
		check("collapse", c.In, jsCollapseSpaces(c.In, " "), c.Collapse)
		check("removeSpaces", c.In, jsRemoveSpaces(c.In), c.RemoveSpaces)
		if got := jsFields(c.In); !reflect.DeepEqual(nonNil(got), nonNil(c.Fields)) {
			t.Errorf("fields(%q)：Go=%q V8=%q", c.In, got, c.Fields)
		}
		if got := u16Len(c.In); got != c.Length {
			t.Errorf("length(%q)：Go=%d V8=%d", c.In, got, c.Length)
		}
		for _, s := range c.Slices {
			if got := u16Slice(c.In, s.From, s.To); got != s.Out {
				t.Errorf("slice(%q,%d,%d)：Go=%q V8=%q", c.In, s.From, s.To, got, s.Out)
			}
		}
		for i, unit := range c.Units {
			if got, ok := u16At(c.In, i); !ok || int(got) != unit {
				t.Errorf("charCodeAt(%q,%d)：Go=%d(ok=%v) V8=%d", c.In, i, got, ok, unit)
			}
		}
	}

	for _, c := range golden.Compare {
		if got := jsLocaleCompare(c.A, c.B); got != c.Out {
			t.Errorf("localeCompare(%q,%q)：Go=%d V8=%d", c.A, c.B, got, c.Out)
		}
	}

	for _, c := range golden.NumberString {
		if got := jsNumberString(c.In); got != c.Out {
			t.Errorf("String(%v)：Go=%q V8=%q", c.In, got, c.Out)
		}
	}

	for _, c := range golden.ToNumber {
		got, ok := jsNumber(c.In)
		if ok != c.OK {
			t.Errorf("Number(%q) 是否为数：Go=%v V8=%v（Go 值 %v）", c.In, ok, c.OK, got)
			continue
		}
		switch {
		case !ok:
		case c.Infinite != 0:
			if !math.IsInf(got, c.Infinite) {
				t.Errorf("Number(%q)：Go=%v V8=%+dInf", c.In, got, c.Infinite)
			}
		case c.Out == nil:
			t.Errorf("夹具异常：Number(%q) 有限但没有取值", c.In)
		case got != *c.Out:
			t.Errorf("Number(%q)：Go=%s V8=%s", c.In, strconv.FormatFloat(got, 'g', -1, 64), strconv.FormatFloat(*c.Out, 'g', -1, 64))
		}
	}

	// 模型列表排序：与 Array.prototype.sort + localeCompare 的结果一致（稳定排序）
	ids := make([]ModelInfo, 0, len(golden.Sorted))
	seen := map[string]bool{}
	for _, c := range golden.Compare {
		if !seen[c.A] {
			seen[c.A] = true
			ids = append(ids, ModelInfo{ID: c.A})
		}
	}
	sortModels(ids)
	got := make([]string, len(ids))
	for i, m := range ids {
		got[i] = m.ID
	}
	if !reflect.DeepEqual(got, golden.Sorted) {
		t.Errorf("模型 id 排序不一致\nGo=%q\nV8=%q", got, golden.Sorted)
	}
}
