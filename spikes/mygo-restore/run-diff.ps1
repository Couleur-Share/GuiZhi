# 备份仓库与恢复流程的差分测试：产品真实的 TS 代码 vs Go 移植，互读互写、对拍恢复结果。
# 用法（仓库根目录）：powershell -File spikes\mygo-restore\run-diff.ps1 -GoExe <mygo-restore.exe> -FixtureDb <fixture 数据库>
# 对拍壳层里的实现：-GoExe <mygo-shell.exe> -GoPrefix -backupcli（壳层的 -backupcli 子命令与 spike 的命令行同名同参）
# 所有操作都在临时目录里，不碰真实用户数据；fixture 数据库只读复制。
param([Parameter(Mandatory)][string]$GoExe, [Parameter(Mandatory)][string]$FixtureDb, [string[]]$GoPrefix = @())
$ErrorActionPreference = 'Continue'
$PSDefaultParameterValues['Get-Content:Encoding'] = 'UTF8'   # PowerShell 5.1 默认按 ANSI 读文件，中文会乱码
$noBom = New-Object Text.UTF8Encoding $false
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$ts = Join-Path $here 'ts-repo.mts'
$cmp = Join-Path $here 'cmp.mjs'
$work = Join-Path $env:TEMP ("guizhi-restore-diff-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $work | Out-Null
$env:GUIZHI_FIXTURE_DB = $FixtureDb
$env:GUIZHI_SCHEMA_VERSION = (& pnpm dlx tsx $ts schema 2>&1 | Select-Object -Last 1)
$script:fails = 0
$PW = 'correct horse battery'
function T { & pnpm dlx tsx $ts @args 2>&1 | Where-Object { $_ -notmatch 'starship|^\[DB\]|Database initialized' } }
function G { & $GoExe @GoPrefix @args 2>&1 }
function Check($name, $ok, $detail = '') {
  $ok = [bool]$ok
  if (-not $ok) { $script:fails++ }
  Write-Host ("{0} {1,-52} {2}" -f $(if ($ok) { '[ OK ]' } else { '[FAIL]' }), $name, $detail)
}
function Same($name, $a, $b, $ignore = '') {
  $out = & node $cmp $a $b $ignore
  Check $name ($LASTEXITCODE -eq 0) ($out -join '; ')
}
function Fresh($n) { $p = Join-Path $work $n; New-Item -ItemType Directory -Force $p | Out-Null; $p }
function CopyRepo($from, $to) { Copy-Item $from $to -Recurse -Force; $to }
$cur = Join-Path $work 'cur.json'
[IO.File]::WriteAllText($cur, '{"ytDlpPath":"D:\\live\\yt.exe","theme":"light","launchAtStartup":true,"closeAction":"exit"}', $noBom)
$rend = Join-Path $work 'rend.json'
[IO.File]::WriteAllText($rend, '{"theme":"dark","fontSize":15,"ytDlpPath":"C:\\snapshot\\yt.exe","dataPath":"Z:\\old","closeAction":"tray","custom":{"a":[1,2,3]}}', $noBom)
Write-Host "结构版本 $env:GUIZHI_SCHEMA_VERSION，工作目录 $work`n"

# ---------- 1. TS 生成的仓库，Go 读取 ----------
Write-Host '== 1. TS 创建的加密仓库，Go 能否读 =='
$R = Fresh 'r-ts'; T make $R $PW | Out-Null
$mk = Get-Content "$R\make.json" -Raw | ConvertFrom-Json
$s1 = $mk.s1.snapshot.fileName; $s2 = $mk.s2.snapshot.fileName
Check 'TS 快照 2 复用旧对象' ($mk.s2.reusedObjects -ge 6) "reused=$($mk.s2.reusedObjects) created=$($mk.s2.createdObjects)"
foreach ($s in @($s1, $s2)) {
  T read $R $s $PW "$work\ts-read-$s.json" | Out-Null; G read $R $s $PW "$work\go-read-$s.json" | Out-Null
  Same "读取清单与全部对象（$($s.Substring(0,13))）" "$work\ts-read-$s.json" "$work\go-read-$s.json"
}

# ---------- 2. 口令：NFKC、Unicode、错误提示 ----------
Write-Host "`n== 2. 口令 =="
$uni = 'ｐａｓｓ　ｗｏｒｄ😀１２３４５６'
$R2 = Fresh 'r-uni'; T make $R2 $uni | Out-Null
$id = (Get-Content "$R2\make.json" -Raw | ConvertFrom-Json).s1.snapshot.fileName
G read $R2 $id $uni "$work\go-uni.json" | Out-Null
Check 'Go 用全角/emoji 口令解开 TS 仓库' (Test-Path "$work\go-uni.json")
$nfkc = 'pass word😀123456'
G read $R2 $id $nfkc "$work\go-uni2.json" | Out-Null
Check 'NFKC 等价的半角口令同样能解开' (Test-Path "$work\go-uni2.json")
foreach ($bad in @('wrong password 123456', 'short')) {
  T preview $R $s1 $bad "$work\p-ts.json" | Out-Null; $o1 = (Get-Content "$work\p-ts.json" -Raw | ConvertFrom-Json).error
  G preview $R $s1 $bad "$work\p-go.json" | Out-Null; $o2 = (Get-Content "$work\p-go.json" -Raw | ConvertFrom-Json).error
  Check "错误口令提示一致（$bad）" ($o1 -eq $o2) "TS=[$o1] Go=[$o2]"
}

# ---------- 3. 恢复结果对拍 ----------
Write-Host "`n== 3. 恢复：TS 与 Go 各恢复到同样的“现场”，结果对拍 =="
$tT = Join-Path $work 't-ts'; $tG = Join-Path $work 't-go'
T mk-target $tT | Out-Null; T mk-target $tG | Out-Null
T dump $tT "$work\before.json" | Out-Null
T restore $R $s1 $PW $tT | Out-Null
G restore $R $s1 $PW $tG $cur | Out-Null
T dump $tT "$work\dump-ts.json" | Out-Null; T dump $tG "$work\dump-go.json" | Out-Null
Same '恢复后的文件树、设置、条目（TS vs Go）' "$work\dump-ts.json" "$work\dump-go.json"
$d = Get-Content "$work\dump-go.json" -Raw | ConvertFrom-Json
Check '机器相关设置保留本机值' (($d.settings | Where-Object key -eq 'ytDlpPath').value -eq '"D:\\live\\yt.exe"') (($d.settings | Where-Object key -eq 'ytDlpPath').value)
Check '暂存/回滚目录已清理、.clean 已删除' (-not ($d.files.PSObject.Properties.Name | Where-Object { $_ -match 'restore-(stage|rollback)|knowledge.db.clean' }))
$pend = Get-Content "$tG\config\pending-renderer-settings.json" -Raw
Write-Host "      pending-renderer-settings: $pend"

# ---------- 4. Go 创建的仓库，TS 读取并恢复 ----------
Write-Host "`n== 4. Go 创建的加密仓库，TS 能否读、能否恢复 =="
$RG = Fresh 'r-go'
Copy-Item "$R\src" "$RG\src" -Recurse; Remove-Item "$RG\src\backups" -Recurse -Force
G init $RG $PW | Out-Null
G snapshot $RG $PW $rend manual "$work\go-snap.json" | Out-Null
$gs = (Get-Content "$work\go-snap.json" -Raw | ConvertFrom-Json)
$gid = $gs.snapshot.fileName
Check 'Go 创建快照' $gs.success "created=$($gs.createdObjects) reused=$($gs.reusedObjects)"
T read $RG $gid $PW "$work\ts-read-go.json" | Out-Null
Check 'TS 解开并校验 Go 的快照' (Test-Path "$work\ts-read-go.json")
T preview $RG $gid $PW "$work\ts-prev-go.json" | Out-Null
Check 'TS 恢复预检通过' ((Get-Content "$work\ts-prev-go.json" -Raw | ConvertFrom-Json).success)
$t3 = Join-Path $work 't-from-go'; $t4 = Join-Path $work 't-from-ts'
T mk-target $t3 | Out-Null; T mk-target $t4 | Out-Null
T restore $RG $gid $PW $t3 | Out-Null
T restore $R $s2 $PW $t4 | Out-Null
T dump $t3 "$work\dump-from-go.json" | Out-Null; T dump $t4 "$work\dump-from-ts.json" | Out-Null
Same 'TS 恢复 Go 快照 与 恢复 TS 快照 结果一致' "$work\dump-from-go.json" "$work\dump-from-ts.json"

# ---------- 5. 混合：Go 往 TS 仓库里加快照 ----------
Write-Host "`n== 5. 混合仓库（对象去重跨实现） =="
$RM = CopyRepo $R (Join-Path $work 'r-mixed')
G snapshot $RM $PW $rend auto "$work\go-mixed.json" | Out-Null
$gm = Get-Content "$work\go-mixed.json" -Raw | ConvertFrom-Json
Check 'Go 快照复用 TS 写入的对象' ($gm.reusedObjects -ge 6) "reused=$($gm.reusedObjects) created=$($gm.createdObjects)"
T read $RM $gm.snapshot.fileName $PW "$work\ts-read-mixed.json" | Out-Null
Check 'TS 读取混合仓库里的 Go 快照' (Test-Path "$work\ts-read-mixed.json")

# ---------- 6. 预检矩阵 ----------
Write-Host "`n== 6. 恢复预检：损坏/缺失/篡改，TS 与 Go 的判定对拍 =="
function Preview($label, $repoDir, $id, $pw) {
  T preview $repoDir $id $pw "$work\pv-ts.json" | Out-Null
  G preview $repoDir $id $pw "$work\pv-go.json" | Out-Null
  Same "预检：$label" "$work\pv-ts.json" "$work\pv-go.json"
}
Preview '完好' $R $s1 $PW
Preview '快照不存在' $R '1-aaaa' $PW
Preview '快照 id 非法' $R '../x' $PW
$P = CopyRepo $R (Join-Path $work 'r-bad')
$obj = Get-ChildItem "$P\src\backups\repository\objects" | Where-Object { $_.Length -eq 5000 + 34 } | Select-Object -First 1
if (-not $obj) { $obj = Get-ChildItem "$P\src\backups\repository\objects" | Sort-Object Length | Select-Object -Skip 1 -First 1 }
$bytes = [IO.File]::ReadAllBytes($obj.FullName); $bytes[$bytes.Length - 3] = $bytes[$bytes.Length - 3] -bxor 0xFF; [IO.File]::WriteAllBytes($obj.FullName, $bytes)
Preview '某个对象被改一个字节' $P $s1 $PW
Remove-Item (Get-ChildItem "$P\src\backups\repository\objects" | Sort-Object Length -Descending | Select-Object -First 1).FullName
Preview '最大的对象缺失' $P $s1 $PW
$mp = "$P\src\backups\repository\snapshots\$s2\manifest.enc"
$mb = [IO.File]::ReadAllBytes($mp); $mb[$mb.Length - 1] = $mb[$mb.Length - 1] -bxor 1; [IO.File]::WriteAllBytes($mp, $mb)
Preview '清单被篡改' $P $s2 $PW
$P2 = CopyRepo $R (Join-Path $work 'r-ver')
$hp = "$P2\src\backups\repository\repository-header.json"
$h = Get-Content $hp -Raw; [IO.File]::WriteAllText($hp, ($h -replace '"version": 1', '"version": 2'), $noBom)
Preview '仓库头版本不支持' $P2 $s1 $PW

# ---------- 7. 改口令、删除与回收 ----------
Write-Host "`n== 7. 改口令、删除快照与对象回收 =="
$C = CopyRepo $R (Join-Path $work 'r-chpw')
G changepw $C $PW 'a brand new password' | Out-Null
T read $C $s1 'a brand new password' "$work\chpw-ts.json" | Out-Null
Check 'Go 改口令后 TS 用新口令可读' (Test-Path "$work\chpw-ts.json")
Remove-Item "$work\chpw-old.json" -ErrorAction SilentlyContinue
T read $C $s1 $PW "$work\chpw-old.json" | Out-Null
Check 'Go 改口令后旧口令失效' (-not (Test-Path "$work\chpw-old.json"))
T changepw $C 'a brand new password' 'yet another password' | Out-Null
G read $C $s1 'yet another password' "$work\chpw-go.json" | Out-Null
Check 'TS 改口令后 Go 用新口令可读' (Test-Path "$work\chpw-go.json")
$D1 = CopyRepo $R (Join-Path $work 'r-del-ts'); $D2 = CopyRepo $R (Join-Path $work 'r-del-go')
$o1 = T delete $D1 $s1 $PW; $o2 = G delete $D2 $s1 $PW
$l1 = (Get-ChildItem "$D1\src\backups\repository\objects").Name | Sort-Object; $l2 = (Get-ChildItem "$D2\src\backups\repository\objects").Name | Sort-Object
Check '删除快照后回收的对象一致' (($l1 -join ',') -eq ($l2 -join ',') -and "$o1" -eq "$o2") "TS:$o1 Go:$o2 剩余 $($l1.Count)/$($l2.Count)"

# ---------- 8. 便携包 ----------
Write-Host "`n== 8. 便携包导出 =="
T portable $R $s2 $PW "$work\ts.zip" | Out-Null; G portable $R $s2 $PW "$work\go.zip" | Out-Null
& node (Join-Path $here 'unzip-hash.mjs') "$work\ts.zip" "$work\ts-zip.json"; & node (Join-Path $here 'unzip-hash.mjs') "$work\go.zip" "$work\go-zip.json"
Same '两个便携包解开后的条目与内容一致' "$work\ts-zip.json" "$work\go-zip.json"
$gz = G portable $R $s2 $PW "$work\go.zip"
Check '目标文件已存在时拒绝覆盖' ($LASTEXITCODE -ne 0)

# ---------- 9. 故障注入：换库中途失败必须完整回滚 ----------
Write-Host "`n== 9. 换库中途失败的回滚（Go 注入失败，TS 的 dump 校验） =="
foreach ($n in 1, 2, 3, 5, 7) {
  $tf = Join-Path $work "t-fail$n"; T mk-target $tf | Out-Null; T dump $tf "$work\fail-before-$n.json" | Out-Null
  $env:GUIZHI_FAIL_AFTER = "$n"; $out = G restore $R $s1 $PW $tf $cur; $code = $LASTEXITCODE; Remove-Item Env:GUIZHI_FAIL_AFTER
  T dump $tf "$work\fail-after-$n.json" | Out-Null
  $o = & node $cmp "$work\fail-before-$n.json" "$work\fail-after-$n.json"
  Check "第 $n 次替换后失败，回滚到换库前" ($code -ne 0 -and $LASTEXITCODE -eq 0) (($o -join '; '))
}
Write-Host "`n失败 $script:fails 项。结果目录：$work"
exit $script:fails
