<#
  MyGo 壳安装包的虚拟机验收取证脚本（只读采集 + 断言，不安装、不卸载、不删除任何东西）。
  使用方式见同目录 installer-vm-checklist.md。兼容 Windows PowerShell 5.1（Windows 11 自带）。

  Snapshot：采集安装相关状态到 JSON。
  Check   ：对两份快照做场景断言，输出 PASS/FAIL，任何一项 FAIL 退出码为 1。
            场景 fresh（全新安装）/ overwrite（覆盖安装）/ uninstall（卸载）。

  安全护栏：固定 guid 与真实安装共用同一个卸载登记项，所以脚本检测不到虚拟机时直接拒绝运行，
  除非显式加 -AllowPhysicalHost（不建议；这台开发机就不应该加）。
  Probe for MyGo-shell installer acceptance in a disposable VM. Read-only; refuses on physical hosts by default.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Snapshot', 'Check')][string]$Action,
    [string]$Out,                                       # Snapshot：输出文件
    [string]$Before, [string]$After,                    # Check：前后快照
    [ValidateSet('fresh', 'overwrite', 'uninstall')][string]$Scenario,
    [string]$ExpectVersion = '0.28.0',                  # Check：安装后应登记的版本
    [switch]$AllowPhysicalHost
)

$ErrorActionPreference = 'Stop'
$Guid = 'fa5aa350-68bd-43b9-b440-a7ecb8fcc78a'

function Test-IsVirtualMachine {
    $cs = Get-CimInstance Win32_ComputerSystem
    $text = "$($cs.Manufacturer) $($cs.Model)"
    return [bool]($text -match 'VMware|VirtualBox|Virtual Machine|Hyper-V|KVM|QEMU|Parallels|Xen|Bochs')
}

function Get-UninstallEntries {
    $roots = @(
        'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall',
        'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall',
        'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall'
    )
    foreach ($root in $roots) {
        if (-not (Test-Path $root)) { continue }
        foreach ($k in Get-ChildItem $root) {
            if ($k.PSChildName -notlike "*$Guid*") { continue }
            $p = Get-ItemProperty $k.PSPath
            [pscustomobject]@{
                key = $k.Name; displayName = $p.DisplayName; displayVersion = $p.DisplayVersion
                installLocation = $p.InstallLocation; uninstallString = $p.UninstallString
            }
        }
    }
}

function Get-DirSummary([string]$dir) {
    if (-not $dir -or -not (Test-Path -LiteralPath $dir)) { return $null }
    $files = Get-ChildItem -LiteralPath $dir -Recurse -File -Force -ErrorAction SilentlyContinue
    $list = @($files | Sort-Object FullName | ForEach-Object {
            [pscustomobject]@{ path = $_.FullName.Substring($dir.Length).TrimStart('\'); size = $_.Length }
        })
    [pscustomobject]@{ dir = $dir; fileCount = $list.Count; totalBytes = ($list | Measure-Object size -Sum).Sum; files = $list }
}

function Get-WebView2Version {
    $id = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    foreach ($k in "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$id", "HKLM:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$id", "HKCU:\Software\Microsoft\EdgeUpdate\Clients\$id") {
        if (Test-Path $k) { $v = (Get-ItemProperty $k).pv; if ($v) { return $v } }
    }
    return $null
}

function New-Snapshot {
    $entries = @(Get-UninstallEntries)
    $installDir = ($entries | Where-Object { $_.installLocation } | Select-Object -First 1).installLocation
    $exe = if ($installDir) { Join-Path $installDir 'GuiZhi.exe' } else { $null }
    $exeInfo = $null
    if ($exe -and (Test-Path -LiteralPath $exe)) {
        $f = Get-Item -LiteralPath $exe
        $exeInfo = [pscustomobject]@{
            path = $exe; size = $f.Length; productVersion = $f.VersionInfo.ProductVersion
            sha256 = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash
            signature = (Get-AuthenticodeSignature -LiteralPath $exe).Status.ToString()
        }
    }
    $state = $null
    foreach ($k in 'HKLM:\Software\GuiZhi\InstallerState', 'HKCU:\Software\GuiZhi\InstallerState') {
        if (Test-Path $k) { $state = [pscustomobject]@{ key = $k; values = (Get-ItemProperty $k | Select-Object InstallLocation, InstallMode) } }
    }
    $shortcuts = @(
        [Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('CommonDesktopDirectory'),
        [Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('CommonPrograms')
    ) | ForEach-Object { Get-ChildItem -LiteralPath $_ -Filter '*.lnk' -ErrorAction SilentlyContinue | Where-Object { $_.Name -like '*GuiZhi*' -or $_.Name -like '*归知*' } } |
        ForEach-Object { $_.FullName }
    $appData = Join-Path $env:APPDATA 'GuiZhi'
    $local = Get-ChildItem -LiteralPath $env:LOCALAPPDATA -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -like '*guizhi*' } | ForEach-Object { $_.FullName }
    [pscustomobject]@{
        takenAt = (Get-Date).ToString('o'); machine = $env:COMPUTERNAME
        isVm = (Test-IsVirtualMachine); webview2 = (Get-WebView2Version)
        uninstallEntries = $entries; installerState = $state; exe = $exeInfo
        installDir = (Get-DirSummary $installDir)
        shortcuts = @($shortcuts)
        appData = (Get-DirSummary $appData)       # 用户数据：安装、覆盖、卸载都必须保持不变
        localCacheDirs = @($local)
        runningProcs = @(Get-Process -Name 'GuiZhi*' -ErrorAction SilentlyContinue | ForEach-Object { $_.Id })
    }
}

$script:fail = 0
function Assert([bool]$ok, [string]$name, [string]$detail = '') {
    if ($ok) { Write-Host "PASS  $name" -ForegroundColor Green }
    else { $script:fail++; Write-Host "FAIL  $name  $detail" -ForegroundColor Red }
}

if ($Action -eq 'Snapshot') {
    if (-not $Out) { throw 'Snapshot 需要 -Out <文件>' }
    if (-not (Test-IsVirtualMachine) -and -not $AllowPhysicalHost) {
        throw '未检测到虚拟机：该安装包与真实安装共用卸载登记项，拒绝在物理机上采集验收快照。'
    }
    New-Snapshot | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $Out -Encoding UTF8
    Write-Host "快照已写入 $Out"
    return
}

# ---- Check ----
if (-not $Before -or -not $After -or -not $Scenario) { throw 'Check 需要 -Before -After -Scenario' }
$b = Get-Content -LiteralPath $Before -Raw -Encoding UTF8 | ConvertFrom-Json
$a = Get-Content -LiteralPath $After -Raw -Encoding UTF8 | ConvertFrom-Json

# 用户数据：三种场景下都必须逐文件（路径+大小）不变
$bd = if ($b.appData) { ($b.appData.files | ForEach-Object { "$($_.path)|$($_.size)" }) -join "`n" } else { '' }
$ad = if ($a.appData) { ($a.appData.files | ForEach-Object { "$($_.path)|$($_.size)" }) -join "`n" } else { '' }
if ($b.appData -or $Scenario -ne 'fresh') {
    Assert ($bd -eq $ad) '%APPDATA%\GuiZhi 下的用户数据文件（路径与大小）前后一致' "前 $($b.appData.fileCount) 个 / 后 $($a.appData.fileCount) 个"
}
if ($Scenario -eq 'uninstall') { Assert (@($a.runningProcs).Count -eq 0) '卸载后没有残留的 GuiZhi 进程' "PID: $(@($a.runningProcs) -join ',')" }

switch ($Scenario) {
    { $_ -in 'fresh', 'overwrite' } {
        Assert (@($a.uninstallEntries).Count -ge 1) '已写入卸载登记项'
        Assert (@($a.uninstallEntries | Where-Object { $_.displayVersion -eq $ExpectVersion }).Count -ge 1) "登记版本为 $ExpectVersion" "实际 $((@($a.uninstallEntries) | ForEach-Object displayVersion) -join ',')"
        Assert ($null -ne $a.exe) '安装目录下存在 GuiZhi.exe'
        if ($a.exe) {
            Assert ($a.exe.size -gt 1MB -and $a.exe.size -lt 60MB) 'GuiZhi.exe 体积在预期量级（MyGo 壳，不是 Electron 的百兆级）' "实际 $($a.exe.size) 字节"
            Assert ($a.exe.signature -eq 'NotSigned') '签名状态为 NotSigned（本版本按约定不签名）' "实际 $($a.exe.signature)"
        }
        Assert ($null -ne $a.installerState) '写入了 Software\GuiZhi\InstallerState（供自动更新与下次安装定位）'
        Assert (@($a.shortcuts).Count -ge 1) '桌面或开始菜单有快捷方式'
        Assert (-not $a.installDir -or -not (@($a.installDir.files | Where-Object { $_.path -like 'electron*' -or $_.path -like 'resources\app.asar*' -or $_.path -eq 'chrome_100_percent.pak' }).Count)) '安装目录里没有 Electron 残留（electron*、app.asar、chrome_100_percent.pak）'
    }
    'uninstall' {
        Assert (@($a.uninstallEntries).Count -eq 0) '卸载登记项已移除'
        Assert ($null -eq $a.exe) 'GuiZhi.exe 已删除'
        Assert ($null -eq $a.installerState) 'Software\GuiZhi\InstallerState 已清除'
        Assert (@($a.shortcuts).Count -eq 0) '快捷方式已移除'
        Assert ($null -eq $a.installDir -or $a.installDir.fileCount -eq 0) '安装目录已清空（允许目录本身因占用残留，但不得有文件）' "残留 $($a.installDir.fileCount) 个文件"
        Assert (@($a.localCacheDirs).Count -eq 0) '%LOCALAPPDATA% 下的更新缓存目录已清除'
    }
}

if ($script:fail -gt 0) { Write-Host "共 $($script:fail) 项失败" -ForegroundColor Red; exit 1 }
Write-Host '全部通过' -ForegroundColor Green
