# 真实 UI 备份 / 恢复 / 重启整链路（离屏、隔离沙箱）。
# 用法：.\run-backup-chain.ps1 [-Db <knowledge.db>]；默认用 %TEMP%\guizhi-golden\knowledge.db（22 条黄金库）。
param([string]$Db = (Join-Path $env:TEMP 'guizhi-golden\knowledge.db'))
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
$sb = New-Sandbox -Name 'backup-chain' -SourceDb $Db
$r = Invoke-Shot -Sandbox $sb -ScriptFile "$PSScriptRoot\backup-chain.js" -Wait 8s -After 4s
"[阶段 0] exit=$($r.ExitCode) 用时 $($r.Seconds)s"
$r.Stdout -split "`n" | Where-Object { $_ -match 'shotjs|FAIL|\[ipc\]' } | ForEach-Object { "  $_" }
"等待重启后的子进程退出…"
if (-not (Wait-SandboxExit $sb -TimeoutSec 90)) { "[WARN] 子进程超时，已按精确 PID 结束" }
$results = Get-ChildItem "$($sb.Root)\*.result.json" | Sort-Object LastWriteTime
# 结果文件里 byTag 等字段太长，只摘要打印
$phase0 = $null; $phase1 = $null
foreach ($f in $results) {
  $v = (Get-Content $f.FullName -Raw -Encoding UTF8 | ConvertFrom-Json).value
  if ($null -eq $v) { continue }
  if ($v.phase -eq 0) { $phase0 = $v } else { $phase1 = $v }
}
function StepData($v, $n) { ($v.steps | Where-Object name -eq $n).data }
"阶段 0：备份后 UI 出现快照用时 $((StepData $phase0 'ui-backup-created').ms) ms；改库后总数 $((StepData $phase0 'counts-after-mutation').all)；恢复前全文命中 $((StepData $phase0 'fts-hit-before-restore').n)"
"阶段 1：重启后总数 $((StepData $phase1 'counts').all)；被回滚条目的全文命中 $((StepData $phase1 'fts-hit-after-restore').n)；已有条目全文命中 $((StepData $phase1 'fts-known-item-after-restore').n)；ok=$($phase1.ok)"
"--- 备份目录"
Get-ChildItem "$($sb.UserData)\backups" -ErrorAction SilentlyContinue | Select-Object Name, Length | Format-Table -AutoSize | Out-String
"--- pre-restore 快照是否保留了被回滚的条目（字节搜索标题）"
$pre = Get-ChildItem "$($sb.UserData)\backups\knowledge-pre-restore-*.db" | Select-Object -First 1
if ($pre) {
  $bytes = [IO.File]::ReadAllBytes($pre.FullName)
  $needle = [Text.Encoding]::UTF8.GetBytes('VERIFY-AFTER-BACKUP')
  $found = $false
  for ($i = 0; $i -le $bytes.Length - $needle.Length -and -not $found; $i++) {
    if ($bytes[$i] -eq $needle[0]) {
      $m = $true; for ($j = 1; $j -lt $needle.Length; $j++) { if ($bytes[$i + $j] -ne $needle[$j]) { $m = $false; break } }
      if ($m) { $found = $true }
    }
  }
  "pre-restore 含 VERIFY-AFTER-BACKUP：$found"
} else { "没有 pre-restore 快照" }
"--- error.log"
Get-Content "$($sb.UserData)\logs\error.log" -ErrorAction SilentlyContinue
