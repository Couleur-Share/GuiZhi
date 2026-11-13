# 真实进程验证：从旧 Electron 版升级后，Go 壳首次启动导入旧 localStorage，且只导入一次。
# 沙箱：userData 里放真实 Electron 写出的 Local Storage 夹具（testdata/legacy-localstorage）；
#       LOCALAPPDATA 指进沙箱，WebView2 数据不落到真实目录。
# 断言：①第一次启动读到旧键值（含中文）、且 .machine 里出现确认标记；
#       ②用户改了设置后第二次启动不被旧值覆盖（只导入一次）；
#       ③旧 Local Storage 目录的文件没有被改动。
param([string]$Exe)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
if (-not $Exe) { $Exe = $script:DefaultExe }

$fixture = Join-Path $script:ShellRoot 'testdata\legacy-localstorage\Local Storage'
$readJs = Join-Path $env:TEMP 'legacy-ls-read.js'
$editJs = Join-Path $env:TEMP 'legacy-ls-edit.js'
$keys = '["guizhi-settings","guizhi-setup-dismissed","视图-列配置","emoji","guizhi-legacy-ls-imported"]'
Set-Content $readJs "const keys = $keys; const o = {origin: location.origin}; for (const k of keys) o[k] = localStorage.getItem(k); return o;" -Encoding UTF8
Set-Content $editJs 'localStorage.setItem("视图-列配置", "用户后来改的值"); return {ok: true};' -Encoding UTF8

$sb = New-Sandbox -Name 'legacy-ls' -SourceDb (Join-Path $env:TEMP 'guizhi-sandbox\in\golden-knowledge.db')
Copy-Item $fixture (Join-Path $sb.UserData 'Local Storage') -Recurse
$srcDir = Join-Path $sb.UserData 'Local Storage\leveldb'
$hashBefore = (Get-ChildItem $srcDir -File | Sort-Object Name | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Path | Split-Path -Leaf):$($_.Hash)" }) -join ';'
$env2 = @{ LOCALAPPDATA = (Join-Path $sb.Root 'local') }
$marker = Join-Path $sb.UserData '.machine\legacy-localstorage-imported'
$fail = @()

# ① 首次启动
$r1 = Invoke-Shot -Sandbox $sb -Exe $Exe -ScriptFile $readJs -Wait 6s -After 1s -Png a.png -ExtraEnv $env2
$v1 = $r1.Results[-1].value
Remove-Item "$($sb.Root)\*.result.json" -ErrorAction SilentlyContinue
"① 首次启动：settings.language=$(($v1.'guizhi-settings' | ConvertFrom-Json).state.language)  dismissed=$($v1.'guizhi-setup-dismissed')  视图=$($v1.'视图-列配置')  emoji=$($v1.emoji)"
# 应用启动后会把设置补全默认值再写回，所以不比逐字相等；language 的默认值是 en，旧设置里是 zh，是否导入一眼可辨。
if (($v1.'guizhi-settings' | ConvertFrom-Json).state.language -ne 'zh') { $fail += '首次启动没有读到旧设置（language 应为 zh）' }
if ($v1.'guizhi-setup-dismissed' -ne '1') { $fail += '首次引导标记没有导入' }
if ($v1.'视图-列配置' -ne '标题,标签,来源' -or $v1.emoji -ne 'a😀b') { $fail += '中文/emoji 键值不一致' }
if (-not $v1.'guizhi-legacy-ls-imported') { $fail += '页面一侧的标记键缺失' }
if (-not (Test-Path $marker)) { $fail += '主进程标记文件没有写入（页面确认未送达）' }
($r1.Stdout + "`n" + $r1.Stderr) -split "`n" | Where-Object { $_ -match '\[legacy-ls\]' } | ForEach-Object { "   日志：$($_.Trim())" }

# ② 用户改设置后重启
$null = Invoke-Shot -Sandbox $sb -Exe $Exe -ScriptFile $editJs -Wait 5s -After 1s -Png b.png -ExtraEnv $env2
Remove-Item "$($sb.Root)\*.result.json" -ErrorAction SilentlyContinue
$r3 = Invoke-Shot -Sandbox $sb -Exe $Exe -ScriptFile $readJs -Wait 5s -After 1s -Png c.png -ExtraEnv $env2
$v3 = $r3.Results[-1].value
"② 改后重启：视图-列配置=$($v3.'视图-列配置')"
if ($v3.'视图-列配置' -ne '用户后来改的值') { $fail += '重启后用户的改动被旧值覆盖或丢失' }
if (($r3.Stdout + $r3.Stderr) -match '\[legacy-ls\] 从旧') { $fail += '已导入后仍在读取旧库' }

# ③ 旧数据没被动
$hashAfter = (Get-ChildItem $srcDir -File | Sort-Object Name | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Path | Split-Path -Leaf):$($_.Hash)" }) -join ';'
"③ 旧 Local Storage 文件未变：$($hashBefore -eq $hashAfter)"
if ($hashBefore -ne $hashAfter) { $fail += '旧 Local Storage 被改动' }

if ($fail.Count) { "FAIL：" + ($fail -join '；'); exit 1 }
"PASS：旧 localStorage 导入一次、之后保留用户改动、旧数据原样"
