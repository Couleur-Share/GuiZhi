# 回归：渲染进程 localStorage 必须跨启动保留（来源 origin 必须稳定）。
# 起因：渲染资源曾由 127.0.0.1:<随机端口> 的本地 HTTP 服务提供，端口每次不同 → origin 每次不同 →
#       zustand 持久化的设置、界面偏好、首次引导标记在 Go 壳里每次启动都丢。
# 做法：同一隔离沙箱启动两次，第一次写入探针键，第二次读回；并要求两次 origin 相同。
# 隔离：沙箱在 %TEMP%\guizhi-verify 下；mygo 把 WebView2 数据放在 %LOCALAPPDATA%\<应用名>，这里给子进程改写 LOCALAPPDATA 指进沙箱。
param([string]$Exe)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
if (-not $Exe) { $Exe = $script:DefaultExe }

$setJs = Join-Path $env:TEMP 'ls-persist-set.js'
$getJs = Join-Path $env:TEMP 'ls-persist-get.js'
Set-Content $setJs 'localStorage.setItem("zz-ls-probe", "v1"); return {origin: location.origin};' -Encoding UTF8
Set-Content $getJs 'return {origin: location.origin, probe: localStorage.getItem("zz-ls-probe")};' -Encoding UTF8

$sb = New-Sandbox -Name 'ls-persist'
$env2 = @{ LOCALAPPDATA = (Join-Path $sb.Root 'local') }
$r1 = Invoke-Shot -Sandbox $sb -Exe $Exe -ScriptFile $setJs -Wait 5s -After 1s -Png a.png -ExtraEnv $env2
$o1 = $r1.Results[-1].value
Remove-Item "$($sb.Root)\*.result.json" -ErrorAction SilentlyContinue
$r2 = Invoke-Shot -Sandbox $sb -Exe $Exe -ScriptFile $getJs -Wait 5s -After 1s -Png b.png -ExtraEnv $env2
$o2 = $r2.Results[-1].value

"第一次 origin = $($o1.origin)"
"第二次 origin = $($o2.origin)  读回探针 = $($o2.probe)"
$ok = ($o1.origin -eq $o2.origin) -and ($o2.probe -eq 'v1')
if ($ok) { "PASS：localStorage 跨启动保留" } else { "FAIL：origin 不稳定或探针丢失"; exit 1 }
