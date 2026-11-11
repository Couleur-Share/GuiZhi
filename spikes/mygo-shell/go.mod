module guizhi-mygo-shell

go 1.27.3

require (
	codeberg.org/readeck/go-readability/v2 v2.1.3
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.2
	github.com/andybalholm/cascadia v1.3.5
	github.com/egoist/mygo v0.8.3
	github.com/nlnwa/whatwg-url v0.6.2
	github.com/tdewolff/parse/v2 v2.8.20
	golang.org/x/crypto v0.57.0
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.50.0
	golang.org/x/text v0.43.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.62.0
)

require (
	github.com/JohannesKaufmann/dom v0.3.1 // indirect
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-shiori/dom v0.0.0-20230515143342-73569d674e1c // indirect
	github.com/gogs/chardet v0.0.0-20211120154057-b7413eaefb8f // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/itlightning/dateparse v0.2.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace codeberg.org/readeck/go-readability/v2 => ./third_party/readability
