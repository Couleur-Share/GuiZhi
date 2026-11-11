// 合成输入对拍：Go 移植的 ExtractWebArticle 对 make-synthetic-golden.mts 生成的用例，逐字节比较 Markdown 与截断标志。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

	"guizhi-mygo-readability/extract"
)

type tcase struct {
	Name      string `json:"name"`
	HTML      string `json:"html"`
	Source    string `json:"source"`
	Markdown  string `json:"markdown"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

func main() {
	data, _ := os.ReadFile(os.Args[1])
	var cases []tcase
	if err := json.Unmarshal(data, &cases); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bad := 0
	for _, c := range cases {
		a := extract.ExtractWebArticle(c.HTML, "https://example.com/post", 200)
		ok := a.Markdown == c.Markdown && a.Truncated == c.Truncated && a.Source == c.Source
		if !ok {
			bad++
		}
		fmt.Printf("%-34s 一致=%v  ts(source=%s trunc=%v 码点=%d)  go(source=%s trunc=%v 码点=%d)\n", c.Name, ok, c.Source, c.Truncated, utf8.RuneCountInString(c.Markdown), a.Source, a.Truncated, utf8.RuneCountInString(a.Markdown))
	}
	fmt.Printf("共 %d 例，不一致 %d\n", len(cases), bad)
}
