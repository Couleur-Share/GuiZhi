# 把 MyGo 0.9 起随壳分发的 SQLite 原生库（Sqlitecore.dll）放进 -Out 目录。
# 共用方：build-shell.ps1（发布构建）与 scripts/isolated-go.mjs（pnpm shot 的隔离构建）。Windows PowerShell 5.1 与 pwsh 7 均可运行。
#
# 为什么必须这么做：ffi 按「$MYGO_NATIVE_SQLITECORE → 资源目录 → exe 同目录 →（非 mygo build 打包时）用户缓存 → 联网下载」找这个 DLL；
# 我们的 exe 是普通 go build，不是 mygo build 的应用包，而且带 -trimpath 的二进制读不到清单、缓存与下载两条路都不通，
# 所以 DLL 必须放在 exe 同目录。开发机因为有缓存，不放也能跑普通 go build 的产物；全新机器（离线或访问不了 GitHub）首次启动会直接失败
# （0.8.x 时代已踩过这个坑，见 docs/agents/mygo-migration.md 缺陷 1）。ffi 只校验缓存路径里的文件，exe 同目录那份不再校验，所以这里必须验。
#
# 版本、文件名、sha256、下载地址一律取自 go.mod 所指 MyGo 模块里的清单（plugins\sqlite\internal\sqlitecore\mygo-plugin.json），不手写。
# 取文件：MyGo 用户缓存（%LOCALAPPDATA%\mygo\natives\<sha256>\<名称>，校验通过才用）→ 否则按清单地址下载，校验后写进缓存。
# 必须在 go build 之后调用：要靠 go list 找到已下载的 MyGo 模块。
# 用法：ensure-sqlitecore.ps1 -Out <目录> [-GoRoot <含 go.mod 的 apps\desktop-go>]
param(
  [Parameter(Mandatory)][string]$Out,
  [string]$GoRoot = (Resolve-Path "$PSScriptRoot\..").Path
)
$ErrorActionPreference = "Stop"

Push-Location $GoRoot
try { $mygoDir = ("" + (go list -m -f '{{.Dir}}' github.com/egoist/mygo)).Trim() } finally { Pop-Location }
if (-not $mygoDir) { throw "找不到 MyGo 模块目录：请先在 $GoRoot 里 go build（或 go mod download）" }
$manifestPath = Join-Path $mygoDir "plugins\sqlite\internal\sqlitecore\mygo-plugin.json"
if (-not (Test-Path -LiteralPath $manifestPath)) { throw "找不到 SQLite 原生模块清单：$manifestPath（MyGo 升级后目录可能又变了，请对照 plugins/sqlite 的源码调整本脚本）" }
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
$lib = @($manifest.libraries) | Where-Object { $_.name -eq 'Sqlitecore' } | Select-Object -First 1
if (-not $lib) { throw "清单 $manifestPath 里没有名为 Sqlitecore 的库" }
$native = $lib.files.'windows-amd64'
if (-not $native) { throw "清单里没有 windows-amd64 的 Sqlitecore" }

function Test-Sha256([string]$path, [string]$want) {
  (Test-Path -LiteralPath $path) -and ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ieq $want)
}

Write-Host "      内置 SQLite 原生库（$($native.name)）"
$dllCache = Join-Path $env:LOCALAPPDATA ("mygo\natives\{0}\{1}" -f $native.sha256, $native.name)
if (-not (Test-Sha256 $dllCache $native.sha256)) {
  Write-Host "  缓存里没有校验通过的原生库，从清单地址下载：$($native.url)"
  New-Item -ItemType Directory -Force (Split-Path $dllCache) | Out-Null
  $tmp = "$dllCache.part"
  Invoke-WebRequest -Uri $native.url -OutFile $tmp -UseBasicParsing
  if (-not (Test-Sha256 $tmp $native.sha256)) { Remove-Item $tmp -Force; throw "$($native.name) 的 SHA-256 与插件清单不一致，已丢弃下载内容" }
  Move-Item $tmp $dllCache -Force
}
New-Item -ItemType Directory -Force $Out | Out-Null
$dllDest = Join-Path $Out $native.name
Copy-Item -LiteralPath $dllCache -Destination $dllDest -Force
if (-not (Test-Sha256 $dllDest $native.sha256)) { throw "复制后的 $($native.name) 校验失败" }
