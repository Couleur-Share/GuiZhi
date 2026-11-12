; MyGo 壳安装器的额外检查（仅 GUIZHI_MYGO_SHELL 构建引入，见 electron-builder.mygo.cjs）。
; 注意：本文件含中文字符串，必须保存为带 BOM 的 UTF-8，NSIS 才会按 Unicode 读取。
; 自动更新走静默安装（/S），所有对话框在静默模式下都跳过，不阻塞更新。

!define GUIZHI_WEBVIEW2_CLIENT "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
!define GUIZHI_WEBVIEW2_URL "https://developer.microsoft.com/microsoft-edge/webview2/"

; 由 installer.nsh 的 customInit 末尾调用。使用寄存器 $R4（WebView2 版本号）；$R0/$R1 是 customInit 已读出的既有安装位置。
!macro guizhiMygoPreInstallChecks
  ${ifNot} ${Silent}
    ; 1) WebView2 运行时：机器级（含 WOW6432Node）或用户级任一存在即可；Windows 11 自带
    ReadRegStr $R4 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${GUIZHI_WEBVIEW2_CLIENT}" "pv"
    ${if} $R4 == ""
      ReadRegStr $R4 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\${GUIZHI_WEBVIEW2_CLIENT}" "pv"
    ${endif}
    ${if} $R4 == ""
      ReadRegStr $R4 HKCU "Software\Microsoft\EdgeUpdate\Clients\${GUIZHI_WEBVIEW2_CLIENT}" "pv"
    ${endif}
    ${if} $R4 == ""
    ${orIf} $R4 == "0.0.0.0"
      MessageBox MB_YESNO|MB_ICONEXCLAMATION "未检测到 Microsoft Edge WebView2 运行时，归知需要它才能运行。$\r$\n$\r$\n是否打开微软的下载页面？安装完 WebView2 后，请重新运行本安装程序。" IDNO +2
      ExecShell "open" "${GUIZHI_WEBVIEW2_URL}"
      Abort
    ${endif}

    ; 2) 覆盖安装前提示备份：已登记过安装位置（含旧的 Electron 版）就提醒一次，可取消
    ${if} $R0 != ""
    ${orIf} $R1 != ""
      MessageBox MB_OKCANCEL|MB_ICONINFORMATION "检测到已安装的归知，本次安装会覆盖它。$\r$\n$\r$\n你的资料保存在 %APPDATA%\GuiZhi，安装与卸载都不会删除。仍建议先在归知里打开「设置」→「备份」创建一份备份，再继续安装。$\r$\n$\r$\n现在继续安装吗？" IDOK +2
      Abort
    ${endif}
  ${endif}
!macroend