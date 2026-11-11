# 产品对 go-readability v2.1.3 的两处改动

本目录是 `codeberg.org/readeck/go-readability/v2 v2.1.3` 的副本（MIT，见 LICENSE），由 `go.mod` 的 `replace` 引用。
对应 TS 产品里对 `@mozilla/readability` 的两处改动：

1. `Parser.LinkDensityModifier`：条件清理里的链接占比阈值（0.2 / 0.5）加上该修正值（产品取 0.1）。
2. `Parser.KeepTitleHeader`：为真时不删除与标题重复的首个 h1/h2（产品把 `_headerDuplicatesTitle` 改成恒 false）。

改动位置见 `parser.go` 中的 `LinkDensityModifier`、`KeepTitleHeader`。已删去测试页、`cmd/` 与 CI 配置。
升级上游时参照 `spikes/mygo-readability/prepare-patched.ps1` 重新生成副本，并用 `spikes/mygo-readability/cmd/compare-extract` 对拍。
