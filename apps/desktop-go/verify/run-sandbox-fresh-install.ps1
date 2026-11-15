# 宿主侧驱动：在 Windows 沙盒（网络禁用、一次性）里静默安装 NSIS 安装包并首次启动，取回证据并给出 PASS/FAIL。
# 这是“全新机器、无网络安装”的真实验证：沙盒镜像是干净的 Windows（没有归知痕迹、没有 mygo 缓存、没有 VC++ 运行库、没有网络）。
# 不碰宿主：安装包与脚本只读映射，输出目录单独可写；只停自己启动的那个沙盒（按 ID），已有沙盒在运行时拒绝执行。
# 用法：pwsh -File run-sandbox-fresh-install.ps1 -Installer <GuiZhi-Setup-x.y.z-x64.exe> [-ExpectDllSha256 <Sqlitecore.dll 的 sha256>] [-TimeoutSec 420] [-KeepSandbox]
# 说明：wsb connect 会在宿主桌面弹出沙盒的远程会话窗口（没有登录会话 LogonCommand 不会执行）；跑完自动关闭沙盒。
param(
  [Parameter(Mandatory)][string]$Installer,
  [string]$ExpectDllSha256 = '',
  [int]$TimeoutSec = 420,
  [switch]$KeepSandbox
)
$ErrorActionPreference = 'Stop'
$Installer = (Resolve-Path -LiteralPath $Installer).Path
if (-not (Get-Command wsb -ErrorAction SilentlyContinue)) { throw '找不到 wsb（Windows 沙盒未启用？）' }
$running = (& wsb list 2>&1 | Out-String)
if ($running -match '[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}') { throw "已有沙盒在运行，先不打扰它：`n$running" }

$root = Join-Path ([IO.Path]::GetTempPath()) 'guizhi-fresh-sandbox'
if (Test-Path $root) { Remove-Item $root -Recurse -Force }
$inDir = Join-Path $root 'in'; $outDir = Join-Path $root 'out'
New-Item -ItemType Directory -Force $inDir, $outDir | Out-Null
Copy-Item $Installer $inDir
Copy-Item (Join-Path $PSScriptRoot 'sandbox-fresh-install-guest.ps1') $inDir
@{ dllSha256 = $ExpectDllSha256.ToLower() } | ConvertTo-Json | Set-Content (Join-Path $inDir 'expect.json') -Encoding UTF8

$xml = "<Configuration><Networking>Disable</Networking><vGPU>Disable</vGPU><MemoryInMB>4096</MemoryInMB>" +
  "<AudioInput>Disable</AudioInput><VideoInput>Disable</VideoInput><ClipboardRedirection>Disable</ClipboardRedirection><PrinterRedirection>Disable</PrinterRedirection>" +
  "<MappedFolders>" +
  "<MappedFolder><HostFolder>$inDir</HostFolder><SandboxFolder>C:\GuiZhiFreshIn</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>" +
  "<MappedFolder><HostFolder>$outDir</HostFolder><SandboxFolder>C:\GuiZhiFreshOut</SandboxFolder><ReadOnly>false</ReadOnly></MappedFolder>" +
  "</MappedFolders>" +
  "<LogonCommand><Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\GuiZhiFreshIn\sandbox-fresh-install-guest.ps1</Command></LogonCommand></Configuration>"

$startOut = (& wsb start --config $xml 2>&1 | Out-String)
if ($startOut -notmatch '([0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12})') { throw "没有拿到沙盒 ID：$startOut" }
$id = $Matches[1]
"沙盒已启动：$id"
$connectJob = $null
try {
  # wsb connect 会一直阻塞到远程会话窗口关闭，所以放进后台作业，主线程轮询来宾写出的 DONE.flag。
  $connectJob = Start-Job -ScriptBlock { param($i) & wsb connect --id $i 2>&1 } -ArgumentList $id
  $deadline = (Get-Date).AddSeconds($TimeoutSec)
  while ((Get-Date) -lt $deadline -and -not (Test-Path (Join-Path $outDir 'DONE.flag'))) { Start-Sleep -Seconds 3 }
  if (-not (Test-Path (Join-Path $outDir 'DONE.flag'))) { throw "超时 ${TimeoutSec}s 仍没有 DONE.flag（来宾脚本没跑完）；证据目录：$outDir" }
} finally {
  if (-not $KeepSandbox) {
    $null = & wsb stop --id $id 2>&1
    '沙盒已关闭'
    if ($connectJob) { Stop-Job $connectJob -ErrorAction SilentlyContinue; Remove-Job $connectJob -Force -ErrorAction SilentlyContinue }
  }
}

$res = Get-Content (Join-Path $outDir 'result.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$res | ConvertTo-Json -Depth 4
$fail = @()
if ($res.networkReachable) { $fail += '沙盒居然能联网：这次验证不满足“无网络”前提' }
if ($res.installExit -ne 0) { $fail += "安装退出码 $($res.installExit)" }
if (-not $res.exePresent) { $fail += '安装目录里没有 GuiZhi.exe' }
if (-not $res.dllPresent) { $fail += '安装目录里没有 Sqlitecore.dll' }
if ($ExpectDllSha256 -and -not $res.dllMatchesExpected) { $fail += "Sqlitecore.dll 的 sha256 与预期不符：$($res.dllSha256)" }
if (-not $res.distPresent) { $fail += '安装目录里没有 dist\renderer' }
if (-not $res.processAlive) { $fail += '首次启动后进程没有存活' }
if (-not $res.dbCreated -or -not $res.dbHeaderOk) { $fail += '数据库没有建出或不是 SQLite 文件' }
if ($res.errorLog -match '数据库|sqlite|Sqlitecore') { $fail += "error.log 含数据库相关错误：$($res.errorLog)" }
"证据目录：$outDir（result.json / guest.log / screen.png）"
if ($fail.Count) { "FAIL：" + ($fail -join '；'); exit 1 }
"PASS：干净沙盒（无网络、无缓存）静默安装后首次启动正常，数据库已建出"
