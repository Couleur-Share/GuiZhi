# 真实进程 + 真实损坏文件：逐个场景离屏启动 GuiZhi.exe，记录退出码、error.log、完整性备份与修复后的数据量。
# 用法：.\run-corrupt-db.ps1 [-Db <knowledge.db>]；默认用 %TEMP%\guizhi-golden\knowledge.db（22 条黄金库）。
param([string]$Db = (Join-Path $env:TEMP 'guizhi-golden\knowledge.db'))
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"

function Patch-Bytes([string]$Path, [long]$Offset, [byte[]]$Bytes) {
  $fs = [IO.File]::Open($Path, 'Open', 'ReadWrite'); $fs.Seek($Offset, 'Begin') | Out-Null
  $fs.Write($Bytes, 0, $Bytes.Length); $fs.Close()
}
$scenarios = [ordered]@{
  'freelist 计数被改写（可自动修复）' = { param($p)
    # 在当前值上加 2（金库自带 3 个空闲页，写死成 3 等于没改）
    $b = [IO.File]::ReadAllBytes($p); $cur = ([uint32]$b[36] -shl 24) + ([uint32]$b[37] -shl 16) + ([uint32]$b[38] -shl 8) + $b[39]
    $n = $cur + 2; Patch-Bytes $p 36 ([byte[]]@((($n -shr 24) -band 255), (($n -shr 16) -band 255), (($n -shr 8) -band 255), ($n -band 255))) }
  '中间 5 页被随机数据覆盖'           = { param($p) $b = New-Object byte[] 20000; (New-Object Random 7).NextBytes($b); Patch-Bytes $p (4096 * 8) $b }
  '文件被截断到一半'                   = { param($p) $fs = [IO.File]::Open($p, 'Open', 'ReadWrite'); $fs.SetLength([long]($fs.Length / 2)); $fs.Close() }
  '整个文件被文本替换（不是数据库）'   = { param($p) [IO.File]::WriteAllText($p, ('not a database ' * 600)) }
  '文件长度为 0（磁盘写满后的典型残留）' = { param($p) [IO.File]::WriteAllBytes($p, [byte[]]@()) }
}
$i = 0
foreach ($name in $scenarios.Keys) {
  $i++
  $sb = New-Sandbox -Name "corrupt-$i" -SourceDb $Db
  & $scenarios[$name] $sb.Db
  $r = Invoke-Shot -Sandbox $sb -ScriptFile "$PSScriptRoot\counts.js" -Wait 6s -After 1s -Png "o.png"
  $res = ($r.Results | Select-Object -First 1)
  $log = Get-Content "$($sb.UserData)\logs\error.log" -ErrorAction SilentlyContinue
  $integrityBackups = @(Get-ChildItem "$($sb.UserData)\data" -Filter 'knowledge.db.integrity-backup-*' -ErrorAction SilentlyContinue).Count
  [pscustomobject]@{
    场景        = $name
    退出码      = $r.ExitCode
    用时秒      = $r.Seconds
    条目数      = if ($res -and $res.value) { $res.value.all } else { '-' }
    完整性备份  = $integrityBackups
    errorlog    = if ($log) { (($log | Select-Object -Last 1) | ConvertFrom-Json).message } else { '' }
  }
  Wait-SandboxExit $sb -TimeoutSec 20 | Out-Null
}
