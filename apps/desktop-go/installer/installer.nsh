!define GUIZHI_REGISTRY_KEY "Software\\GuiZhi"
!define GUIZHI_INSTALL_STATE_KEY "Software\\GuiZhi\\InstallerState"
!ifdef APP_INSTALLER_STORE_FILE
  !searchreplace GUIZHI_UPDATER_CACHE_DIR "${APP_INSTALLER_STORE_FILE}" "\installer.exe" ""
  ; 卸载时会递归删除这个目录：推导不出单独的目录名就让打包失败
  !if "${GUIZHI_UPDATER_CACHE_DIR}" == ""
    !error "更新缓存目录名为空"
  !endif
  !if "${GUIZHI_UPDATER_CACHE_DIR}" == "${APP_INSTALLER_STORE_FILE}"
    !error "APP_INSTALLER_STORE_FILE 格式已变化，无法推导更新缓存目录"
  !endif
!endif

!macro customInit
  ReadRegStr $R0 HKCU "${INSTALL_REGISTRY_KEY}" InstallLocation
  ReadRegStr $R1 HKLM "${INSTALL_REGISTRY_KEY}" InstallLocation

  ${if} $R0 == ""
  ${andIf} $R1 == ""
    ReadRegStr $R2 HKLM "${GUIZHI_INSTALL_STATE_KEY}" InstallLocation
    ReadRegStr $R3 HKCU "${GUIZHI_INSTALL_STATE_KEY}" InstallLocation

    ${if} $R2 != ""
      StrCpy $installMode all
      SetShellVarContext all
      StrCpy $INSTDIR "$R2"
    ${elseif} $R3 != ""
      StrCpy $installMode CurrentUser
      SetShellVarContext current
      StrCpy $INSTDIR "$R3"
    ${endif}
  ${endif}

  ; MyGo 壳构建额外检查（WebView2 运行时、覆盖安装前备份提示），Electron 版打包不定义该开关
  !ifdef GUIZHI_MYGO_SHELL
    !insertmacro guizhiMygoPreInstallChecks
  !endif
!macroend

!macro customInstall
  WriteRegStr SHELL_CONTEXT "${GUIZHI_INSTALL_STATE_KEY}" InstallLocation "$INSTDIR"
  WriteRegStr SHELL_CONTEXT "${GUIZHI_INSTALL_STATE_KEY}" InstallMode "$installMode"
!macroend

!macro customUnInstall
  ${ifNot} ${isUpdated}
    DeleteRegKey SHELL_CONTEXT "${GUIZHI_INSTALL_STATE_KEY}"
    DeleteRegKey /ifempty SHELL_CONTEXT "${GUIZHI_REGISTRY_KEY}"
    !ifdef GUIZHI_UPDATER_CACHE_DIR
      ; 安装时复制的安装程序副本与自动更新下载缓存固定在当前用户的 LocalAppData，模板卸载时不清理
      ${if} $installMode == "all"
        SetShellVarContext current
      ${endif}
      RMDir /r "$LOCALAPPDATA\${GUIZHI_UPDATER_CACHE_DIR}"
      ${if} $installMode == "all"
        SetShellVarContext all
      ${endif}
    !endif
  ${endif}
!macroend
