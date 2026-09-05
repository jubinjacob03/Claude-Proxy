Unicode true
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

!define APP_NAME    "Claude Proxy"
!define APP_ID      "Claude-Proxy"
!define PUBLISHER   "Jubin"
!define APP_VERSION "2.1.3"
!define TRAY_EXE    "claude-tray.exe"
!define PROXY_EXE   "claude-proxy.exe"
!define UNINST_KEY  "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_ID}"
!define RUN_KEY     "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${APP_NAME}"
OutFile "out\Claude-Proxy-Setup.exe"
InstallDir "$LOCALAPPDATA\Programs\${APP_ID}"
InstallDirRegKey HKCU "Software\${APP_ID}" "InstallDir"
RequestExecutionLevel user
ShowInstDetails show
ShowUninstDetails show

!define MUI_ICON   "..\cmd\claude-tray\icon.ico"
!define MUI_UNICON "..\cmd\claude-tray\icon.ico"

VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey "ProductName"     "${APP_NAME}"
VIAddVersionKey "CompanyName"     "${PUBLISHER}"
VIAddVersionKey "LegalCopyright"  "Copyright (c) 2026 ${PUBLISHER}"
VIAddVersionKey "FileDescription" "${APP_NAME} Setup"
VIAddVersionKey "FileVersion"     "${APP_VERSION}"
VIAddVersionKey "ProductVersion"  "${APP_VERSION}"

Var LicenseKey
Var ExistingKey
Var KeyBox
Var RemoveData
Var RemoveDataBox

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
Page custom KeyPageCreate KeyPageLeave
!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_RUN "$INSTDIR\${TRAY_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Start ${APP_NAME} now"
!define MUI_FINISHPAGE_TEXT "${APP_NAME} is installed and will start automatically every time you log in.$\r$\n$\r$\nDefault endpoints:$\r$\n    Anthropic   http://127.0.0.1:3009$\r$\n    OpenAI      http://127.0.0.1:3009/v1$\r$\n$\r$\nYour licence is bound to this computer on first use. Use the tray menu for Monitor Logs and settings."
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
UninstPage custom un.DataPageCreate un.DataPageLeave
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Function ReadExistingKey
  StrCpy $ExistingKey ""
  ${IfNot} ${FileExists} "$INSTDIR\.env"
    Return
  ${EndIf}
  ClearErrors
  FileOpen $R0 "$INSTDIR\.env" r
  ${If} ${Errors}
    Return
  ${EndIf}
  ReadLoop:
    ClearErrors
    FileRead $R0 $R1
    ${If} ${Errors}
      Goto ReadDone
    ${EndIf}
    StrCpy $R2 $R1 12
    ${If} $R2 == "LICENSE_KEY="
      StrCpy $R3 $R1 "" 12
      TrimLoop:
        StrCpy $R4 $R3 1 -1
        ${If} $R4 == "$\r"
        ${OrIf} $R4 == "$\n"
          StrCpy $R3 $R3 -1
          Goto TrimLoop
        ${EndIf}
      StrCpy $ExistingKey $R3
      Goto ReadDone
    ${EndIf}
    Goto ReadLoop
  ReadDone:
  FileClose $R0
FunctionEnd

Function TrimKey
  TrimAgain:
  TrimLead:
    StrCpy $R0 $LicenseKey 1
    ${If} $R0 == " "
    ${OrIf} $R0 == "$\t"
    ${OrIf} $R0 == "$\r"
    ${OrIf} $R0 == "$\n"
      StrCpy $LicenseKey $LicenseKey "" 1
      Goto TrimLead
    ${EndIf}
  TrimTail:
    StrCpy $R0 $LicenseKey 1 -1
    ${If} $R0 == " "
    ${OrIf} $R0 == "$\t"
    ${OrIf} $R0 == "$\r"
    ${OrIf} $R0 == "$\n"
      StrCpy $LicenseKey $LicenseKey -1
      Goto TrimTail
    ${EndIf}
  StrCpy $R0 $LicenseKey 1
  StrCpy $R1 $LicenseKey 1 -1
  ${If} $R0 == '"'
  ${AndIf} $R1 == '"'
    StrCpy $LicenseKey $LicenseKey -1 1
    Goto TrimAgain
  ${EndIf}
FunctionEnd

Function KeyPageCreate
  !insertmacro MUI_HEADER_TEXT "Licence key" "Enter the licence key you were given."
  Call ReadExistingKey
  ${If} $LicenseKey == ""
    StrCpy $LicenseKey $ExistingKey
  ${EndIf}

  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == error
    Abort
  ${EndIf}

  ${If} $ExistingKey != ""
    ${NSD_CreateLabel} 0 0 100% 58u "The existing licence key is prefilled below. Keep it or replace it.$\r$\n$\r$\nYour licence is locked to this computer the first time it is used, and cannot be moved without an administrator reset."
  ${Else}
    ${NSD_CreateLabel} 0 0 100% 58u "Paste the licence key you were given.$\r$\n$\r$\nIt is locked to this computer the first time it is used, and cannot be moved without an administrator reset. You will not need to enter it again."
  ${EndIf}
  Pop $0

  ${NSD_CreateText} 0 64u 100% 12u "$LicenseKey"
  Pop $KeyBox

  nsDialogs::Show
FunctionEnd

Function KeyPageLeave
  ${NSD_GetText} $KeyBox $LicenseKey
  Call TrimKey

  ${If} $LicenseKey == ""
    MessageBox MB_ICONEXCLAMATION|MB_OK "A licence key is required to continue."
    Abort
  ${EndIf}
FunctionEnd

Function un.DataPageCreate
  !insertmacro MUI_HEADER_TEXT "Application data" "Keep your settings and logs, or wipe everything."
  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == error
    Abort
  ${EndIf}

  ${NSD_CreateLabel} 0 0 100% 56u "By default your settings and logs are kept.$\r$\n$\r$\nKept: .env and logs folder.$\r$\n$\r$\nTick the box below to remove all of it."
  Pop $0

  ${NSD_CreateCheckbox} 0 62u 100% 10u "Delete all application data (clean wipe)"
  Pop $RemoveDataBox
  ${If} $RemoveData == 1
    ${NSD_Check} $RemoveDataBox
  ${EndIf}

  nsDialogs::Show
FunctionEnd

Function un.DataPageLeave
  ${NSD_GetState} $RemoveDataBox $RemoveData
FunctionEnd

Section "Install"
  nsExec::Exec 'taskkill /F /IM ${TRAY_EXE} /IM ${PROXY_EXE}'
  Pop $0

  SetOutPath "$INSTDIR"
  File "staging\${PROXY_EXE}"
  File "staging\${TRAY_EXE}"
  File "..\cmd\claude-tray\icon.ico"
  File "..\LICENSE"

  ${If} $ExistingKey == ""
    Call ReadExistingKey
  ${EndIf}

  ${If} $LicenseKey == $ExistingKey
  ${AndIf} $ExistingKey != ""
    DetailPrint "Licence key unchanged; keeping the existing .env."
  ${ElseIf} $LicenseKey != ""
    Call UpdateEnvKey
  ${ElseIf} ${FileExists} "$INSTDIR\.env"
    DetailPrint "No key entered; keeping the existing .env."
  ${Else}
    Call WriteEnv
  ${EndIf}

  DetailPrint "Activating licence on this machine..."
  nsExec::ExecToLog '"$INSTDIR\${PROXY_EXE}" activate'
  Pop $0
  ${If} $0 != 0
    MessageBox MB_ICONSTOP|MB_OK "Licence activation failed during installation. Check your key and relay settings, then try again."
    Abort
  ${EndIf}

  CreateDirectory "$INSTDIR\logs"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}.lnk" "$INSTDIR\${TRAY_EXE}" "" "$INSTDIR\icon.ico"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  WriteRegStr HKCU "${RUN_KEY}" "${APP_ID}" '"$INSTDIR\${TRAY_EXE}"'

  WriteRegStr HKCU "Software\${APP_ID}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName"     "${APP_NAME}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion"  "${APP_VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher"       "${PUBLISHER}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon"     "$INSTDIR\icon.ico"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1

  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/RESTART" $R1
  ${IfNot} ${Errors}
    Exec '"$INSTDIR\${TRAY_EXE}"'
  ${EndIf}
SectionEnd

Function UpdateEnvKey
  ClearErrors
  FileOpen $R0 "$INSTDIR\.env" r
  ${If} ${Errors}
    Call WriteEnv
    Return
  ${EndIf}
  FileOpen $R1 "$INSTDIR\.env.new" w
  ${If} ${Errors}
    FileClose $R0
    Return
  ${EndIf}
  StrCpy $R5 0

  CopyLoop:
    ClearErrors
    FileRead $R0 $R2
    ${If} ${Errors}
      Goto CopyDone
    ${EndIf}
    StrCpy $R3 $R2 12
    ${If} $R3 == "LICENSE_KEY="
      FileWrite $R1 "LICENSE_KEY=$LicenseKey$\r$\n"
      StrCpy $R5 1
    ${Else}
      FileWrite $R1 $R2
    ${EndIf}
    Goto CopyLoop
  CopyDone:

  ${If} $R5 == 0
    FileWrite $R1 "LICENSE_KEY=$LicenseKey$\r$\n"
  ${EndIf}
  FileClose $R0
  FileClose $R1

  ClearErrors
  Delete "$INSTDIR\.env"
  ${If} ${Errors}
    Delete "$INSTDIR\.env.new"
    DetailPrint "Could not replace .env; the key was left unchanged."
    Return
  ${EndIf}
  ClearErrors
  Rename "$INSTDIR\.env.new" "$INSTDIR\.env"
  ${If} ${Errors}
    DetailPrint "WARNING: .env was removed but .env.new could not be renamed into place."
  ${EndIf}
FunctionEnd

Function WriteEnv
  ClearErrors
  FileOpen $0 "$INSTDIR\.env" w
  ${If} ${Errors}
    MessageBox MB_ICONEXCLAMATION|MB_OK "Setup could not write $INSTDIR\.env"
    Return
  ${EndIf}
  FileWrite $0 "HOST=127.0.0.1$\r$\n"
  FileWrite $0 "PORT=3009$\r$\n"
  FileWrite $0 "RELAY_BASE_URL=http://68.233.112.166:43219$\r$\n"
  FileWrite $0 "LICENSE_KEY=$LicenseKey$\r$\n"
  FileWrite $0 "UPSTREAM_FORMAT=anthropic$\r$\n"
  FileWrite $0 "AUTH_TOKEN=$\r$\n"
  FileWrite $0 "DEFAULT_MODEL=claude-opus-4-8$\r$\n"
  FileWrite $0 "DEFAULT_MAX_TOKENS=4096$\r$\n"
  FileWrite $0 "STREAM_IDLE_PING_SECONDS=15$\r$\n"
  FileWrite $0 "TIMEOUT=0$\r$\n"
  FileWrite $0 "LOG_LEVEL=info$\r$\n"
  FileWrite $0 "LOG_FORMAT=text$\r$\n"
  FileWrite $0 "LOG_BODIES=false$\r$\n"
  FileClose $0
FunctionEnd

Function .onInstFailed
  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegValue HKCU "${RUN_KEY}" "${APP_ID}"
FunctionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /F /IM ${TRAY_EXE} /IM ${PROXY_EXE}'
  Pop $0

  DeleteRegValue HKCU "${RUN_KEY}" "${APP_ID}"
  DeleteRegKey HKCU "${UNINST_KEY}"

  Delete "$SMPROGRAMS\${APP_NAME}.lnk"

  ${If} $RemoveData == 1
    DeleteRegKey HKCU "Software\${APP_ID}"
    RMDir /r "$INSTDIR"
  ${Else}
    Delete "$INSTDIR\${PROXY_EXE}"
    Delete "$INSTDIR\${TRAY_EXE}"
    Delete "$INSTDIR\icon.ico"
    Delete "$INSTDIR\LICENSE"
    Delete "$INSTDIR\uninstall.exe"
    Delete "$INSTDIR\.env.new"
    RMDir /r "$INSTDIR\update-staging"
    RMDir "$INSTDIR"
  ${EndIf}
SectionEnd
