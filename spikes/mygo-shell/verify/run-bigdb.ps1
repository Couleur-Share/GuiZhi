# 大库（默认 3 万条 / 约 750 MB）启动与 IPC 耗时，冷启动（无干净关闭标记，触发 quick_check）与热启动各一次。
# 先生成夹具：
#   $env:GUIZHI_MAKE_BIG_DB="$env:TEMP\guizhi-verify\big\knowledge.db"; go test -count=1 -timeout 30m -run TestMakeBigDatabaseFixture .
param([string]$Db = (Join-Path $env:TEMP 'guizhi-verify\big\knowledge.db'))
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
if (-not (Test-Path $Db)) { throw "缺少大库夹具 $Db，见脚本头部注释" }
$sb = New-Sandbox -Name 'bigdb' -SourceDb $Db
"库大小 $([int]((Get-Item $sb.Db).Length / 1MB)) MB"
foreach ($mode in '冷启动（无 .clean 标记）', '热启动（有 .clean 标记）') {
  if ($mode -like '冷*') { Remove-Item "$($sb.Db).clean" -ErrorAction SilentlyContinue }
  $r = Invoke-Shot -Sandbox $sb -ScriptFile "$PSScriptRoot\bigdb.js" -Wait 8s -After 1s -TimeoutSec 900
  "=== $mode：退出码 $($r.ExitCode)，进程总用时 $($r.Seconds)s（含固定等待 8s+1s）"
  ($r.Stdout + "`n" + $r.Stderr) -split "`n" | Where-Object { $_ -match 'Database initialized|完整性检查耗时|\[DB\]|FAIL' -and $_ -notmatch '已应用' } | ForEach-Object { "  $_" }
  $res = $r.Results | Select-Object -Last 1
  if ($res.error) { "  脚本错误：$($res.error)" }
  $res.value.t | ConvertTo-Json -Compress
  "  条目 $($res.value.counts.all)；首页 $($res.value.firstPage.n)/$($res.value.firstPage.total)；稀有词命中 $($res.value.rareHits)；常见词命中 $($res.value.commonHits.n)/$($res.value.commonHits.total)；备份 $($res.value.backup | ConvertTo-Json -Compress)"
  Get-ChildItem "$($sb.Root)\*.result.json" | Remove-Item
  Wait-SandboxExit $sb -TimeoutSec 30 | Out-Null
}
