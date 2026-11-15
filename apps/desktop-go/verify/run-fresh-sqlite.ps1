# 真实进程验证：发布目录自带 SQLite 原生库（Sqlitecore.dll），在“全新机器”条件下也能启动；缺库时快速、明确地失败。
# 背景：MyGo 0.9 起 SQLite 是原生模块，按 $MYGO_NATIVE_SQLITECORE → 资源目录 → 可执行文件旁 → 用户缓存 → 联网下载 的顺序找；
#       build-shell.ps1 用 -tags mygo_nodownload 编译（绝不联网下载，找不到就报错），并把 DLL 放在 GuiZhi.exe 旁。
# 沙箱：整个发布目录复制进沙箱（只剩发布目录自带的文件）；LOCALAPPDATA 指进空目录（没有 mygo 缓存）；
#       HTTP(S)_PROXY 指向不可用端口（任何联网尝试都会失败）。
# 断言：①成功路径：离屏启动并截图成功、数据库建出且是 SQLite 文件头、error.log 没有数据库错误；
#       ②失败路径：删掉 Sqlitecore.dll 后，进程在 30 秒内以非零状态退出（不联网重试），error.log 写明“找不到 SQLite 原生库 Sqlitecore.dll”。
#          此时数据库文件已由更早一步的 modernc 初始化建好，属正常现象，不作断言。
# 用法：pwsh -File run-fresh-sqlite.ps1 [-Exe <发布目录里的 GuiZhi.exe，默认 out\win-x64>]
param([string]$Exe)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
if (-not $Exe) { $Exe = $script:DefaultExe }
if (-not (Test-Path -LiteralPath $Exe)) { throw "找不到 $Exe（先 pnpm build，或用 -Exe 指向发布目录）" }
if ($env:MYGO_NATIVE_SQLITECORE) { throw '本会话设置了 MYGO_NATIVE_SQLITECORE，会绕过“可执行文件旁”的查找，验证无意义；先清掉再运行。' }
$releaseDir = Split-Path -Parent (Resolve-Path -LiteralPath $Exe).Path
if (-not (Test-Path -LiteralPath (Join-Path $releaseDir 'Sqlitecore.dll'))) { throw "发布目录里没有 Sqlitecore.dll：$releaseDir" }

# 子进程环境：无缓存、无网络
function New-FreshEnv($sb) {
  $dead = 'http://127.0.0.1:9'
  @{ LOCALAPPDATA = (Join-Path $sb.Root 'local'); HTTP_PROXY = $dead; HTTPS_PROXY = $dead; ALL_PROXY = $dead }
}
# 复制发布目录进沙箱，返回沙箱里的 GuiZhi.exe 路径
function Copy-Release($sb) {
  $app = Join-Path $sb.Root 'app'
  New-Item -ItemType Directory -Force $app, (Join-Path $sb.Root 'local') | Out-Null
  Copy-Item -Path (Join-Path $releaseDir '*') -Destination $app -Recurse -Force
  Join-Path $app 'GuiZhi.exe'
}
function Test-SqliteHeader($path) {
  if (-not (Test-Path -LiteralPath $path)) { return $false }
  $fs = [IO.File]::OpenRead($path)
  try {
    $buf = New-Object byte[] 16
    if ($fs.Read($buf, 0, 16) -ne 16) { return $false }
    return [Text.Encoding]::ASCII.GetString($buf, 0, 15) -eq 'SQLite format 3'
  } finally { $fs.Dispose() }
}
function Read-ErrorLog($sb) {
  $p = Join-Path $sb.UserData 'logs\error.log'
  if (Test-Path -LiteralPath $p) { Get-Content -LiteralPath $p -Raw -Encoding UTF8 } else { '' }
}

$fail = @()

# ① 成功路径
$sb1 = New-Sandbox -Name 'fresh-sqlite-ok'
$exe1 = Copy-Release $sb1
$r1 = Invoke-Shot -Sandbox $sb1 -Exe $exe1 -Wait 6s -Png a.png -TimeoutSec 120 -ExtraEnv (New-FreshEnv $sb1)
$log1 = Read-ErrorLog $sb1
$png1 = Test-Path -LiteralPath $r1.Png
"① 全新环境启动：exit=$($r1.ExitCode) 用时=$($r1.Seconds)s 截图=$png1 数据库=$(Test-SqliteHeader $sb1.Db)"
if ($r1.TimedOut -or $r1.ExitCode -ne 0) { $fail += "全新环境启动失败（exit=$($r1.ExitCode)）：$(($r1.Stderr + $log1).Trim())" }
if (-not $png1 -or (Get-Item -LiteralPath $r1.Png -ErrorAction SilentlyContinue).Length -lt 2000) { $fail += '没有截到有效画面' }
if (-not (Test-SqliteHeader $sb1.Db)) { $fail += '数据库没有建出或不是 SQLite 文件' }
if ($log1 -match '数据库|sqlite|Sqlitecore') { $fail += "error.log 含数据库相关错误：$($log1.Trim())" }

# ② 失败路径：缺原生库
$sb2 = New-Sandbox -Name 'fresh-sqlite-missing'
$exe2 = Copy-Release $sb2
Remove-Item -LiteralPath (Join-Path (Split-Path -Parent $exe2) 'Sqlitecore.dll') -Force
$r2 = Invoke-Shot -Sandbox $sb2 -Exe $exe2 -Wait 6s -Png b.png -TimeoutSec 60 -ExtraEnv (New-FreshEnv $sb2)
$log2 = Read-ErrorLog $sb2
"② 缺 Sqlitecore.dll：exit=$($r2.ExitCode) 用时=$($r2.Seconds)s 超时=$($r2.TimedOut)"
($log2.Trim() -split "`n" | Select-Object -First 3) | ForEach-Object { "   error.log：$($_.Trim())" }
if ($r2.TimedOut -or $r2.Seconds -ge 30) { $fail += "缺库时没有快速失败（用时 $($r2.Seconds)s）：可能在联网重试" }
if ($r2.ExitCode -eq 0) { $fail += '缺库时进程竟以 0 退出' }
if ($log2 -notmatch '找不到 SQLite 原生库 Sqlitecore\.dll') { $fail += 'error.log 没有写明找不到 SQLite 原生库 Sqlitecore.dll' }

if ($fail.Count) { "FAIL：" + ($fail -join '；'); exit 1 }
"PASS：全新环境（无缓存、无网络）发布目录可启动；缺 Sqlitecore.dll 时快速报错并说明原因"
