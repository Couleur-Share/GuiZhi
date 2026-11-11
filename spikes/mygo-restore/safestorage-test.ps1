# Electron safeStorage 与 Go 的互通：
#  1) Electron 加密仓库密钥 → 写进仓库头的 safeStorageWrappedKey → Go 无人值守解锁并读完整个快照
#  2) Go 加密 → Electron 解密，明文一致
# 全部在隔离的临时 userData 里进行，不触碰真实的 Electron 数据目录。
param([Parameter(Mandatory)][string]$GoExe, [Parameter(Mandatory)][string]$Electron, [Parameter(Mandatory)][string]$SrcRepo, [Parameter(Mandatory)][string]$Snapshot, [string]$Password = 'correct horse battery')
$ErrorActionPreference = 'Continue'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$work = Join-Path $env:TEMP ("guizhi-ss-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
$eu = "$work\electron-userdata"; New-Item -ItemType Directory -Force $eu | Out-Null
$repo = "$work\repo"; Copy-Item $SrcRepo $repo -Recurse
$noBom = New-Object Text.UTF8Encoding $false
function Ok($n, $c, $d = '') { Write-Host ("{0} {1,-48} {2}" -f $(if ($c) { '[ OK ]' } else { '[FAIL]' }), $n, $d); if (-not $c) { $script:bad++ } }
$script:bad = 0
& $GoExe ss-key $repo $Password "$work\key.b64"
$keyB64 = [IO.File]::ReadAllText("$work\key.b64")
Start-Process $Electron -ArgumentList @("$here\safestorage-main.cjs", '--', $eu, 'encrypt', $keyB64, "$work\wrapped.b64") -Wait -WindowStyle Hidden
Ok 'Electron safeStorage 加密了仓库密钥' (Test-Path "$work\wrapped.b64") ('前缀 ' + [Text.Encoding]::ASCII.GetString([Convert]::FromBase64String([IO.File]::ReadAllText("$work\wrapped.b64")))[0..2] -join '')
$hp = "$repo\src\backups\repository\repository-header.json"
$h = [IO.File]::ReadAllText($hp) | ConvertFrom-Json
$h.safeStorageWrappedKey = [IO.File]::ReadAllText("$work\wrapped.b64"); $h.keyStorageBackend = 'dpapi'
[IO.File]::WriteAllText($hp, ($h | ConvertTo-Json -Depth 6), $noBom)
& $GoExe read-auto $repo $Snapshot $eu "$work\auto.json"
Ok 'Go 用旧版 Local State + DPAPI 无人值守解锁并读完快照' (Test-Path "$work\auto.json") ((Get-Content "$work\auto.json" -ErrorAction SilentlyContinue) -join '')
& $GoExe ss-encrypt $eu 'Go 加密的明文-123' "$work\go-enc.b64"
Start-Process $Electron -ArgumentList @("$here\safestorage-main.cjs", '--', $eu, 'decrypt', "$work\go-enc.b64", "$work\electron-dec.txt") -Wait -WindowStyle Hidden
$dec = if (Test-Path "$work\electron-dec.txt") { [IO.File]::ReadAllText("$work\electron-dec.txt") } else { '' }
Ok 'Electron 解开 Go 加密的 v10 密文' ($dec -eq 'Go 加密的明文-123') "[$dec]"
Remove-Item (Join-Path $eu 'Local State') -Force
& $GoExe read-auto $repo $Snapshot $eu "$work\auto2.json"
Ok '没有 Local State（新机器/换账户）时解锁失败而不是明文回落' (-not (Test-Path "$work\auto2.json"))
exit $script:bad
