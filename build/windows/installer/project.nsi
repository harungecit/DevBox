Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Start ${INFO_PRODUCTNAME}"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchApp
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}" # Program Files\DevBox — the product, not the author, names the folder. Existing installs keep their folder via InstallDirRegKey.
# Upgrades land on the existing installation, wherever it was installed to.
# (The section below writes InstallLocation on every install.)
InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
ShowInstDetails show # This will always show the installation details.

; Append one line to %TEMP%\DevBox-update.log. Silent (in-app) updates are
; otherwise invisible — when something goes wrong on a user machine this log
; is the only evidence. The elevated installer writes to the admin's TEMP.
!macro Log Text
    FileOpen $R9 "$TEMP\DevBox-update.log" a
    FileSeek $R9 0 END
    FileWrite $R9 "${Text}$\r$\n"
    FileClose $R9
!macroend

Function .onInit
   !insertmacro wails.checkArchitecture
   !insertmacro Log "---- DevBox ${INFO_PRODUCTVERSION} installer started ----"
FunctionEnd

; Close a running DevBox before touching its files (in-app update, or the
; user forgot to quit). Waits until the executable can be written; if it
; stays locked (antivirus holding it, taskkill blocked) the install ABORTS
; with a distinct exit code instead of continuing into a broken half-install.
Function CloseRunningApp
    nsExec::ExecToLog 'taskkill /IM "${PRODUCT_EXECUTABLE}" /T /F'
    Pop $0
    !insertmacro Log "taskkill ${PRODUCT_EXECUTABLE} -> rc=$0"
    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 done
    StrCpy $0 0
    retry:
        ClearErrors
        FileOpen $1 "$INSTDIR\${PRODUCT_EXECUTABLE}" a
        IfErrors 0 opened
        IntOp $0 $0 + 1
        IntCmp $0 30 locked        ; ~15 s
        Sleep 500
        Goto retry
    locked:
        !insertmacro Log "ERROR: $INSTDIR\${PRODUCT_EXECUTABLE} still locked after 15s - aborting"
        SetErrorLevel 5
        Abort "DevBox could not be closed - its executable is still locked."
    opened:
        FileClose $1
        !insertmacro Log "app closed, executable is writable"
    done:
FunctionEnd

; Relaunch DevBox as the logged-in user (via explorer, not elevated) — used
; by the finish page and automatically after a silent (in-app) update.
Function LaunchApp
    !insertmacro Log "relaunching $INSTDIR\${PRODUCT_EXECUTABLE}"
    ClearErrors
    Exec '"$WINDIR\explorer.exe" "$INSTDIR\${PRODUCT_EXECUTABLE}"'
    IfErrors 0 launched
    !insertmacro Log "explorer relaunch failed - starting directly"
    Exec '"$INSTDIR\${PRODUCT_EXECUTABLE}"'
    launched:
FunctionEnd

Function .onInstSuccess
    !insertmacro Log "install SUCCEEDED into $INSTDIR"
    IfSilent 0 +2
        Call LaunchApp
FunctionEnd

Function .onInstFailed
    !insertmacro Log "install FAILED (INSTDIR=$INSTDIR)"
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro Log "installing to $INSTDIR"

    Call CloseRunningApp

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    !insertmacro Log "files extracted"

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller

    ; Remember where this install lives so the next (silent) upgrade targets
    ; the same directory even for custom install locations.
    WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    nsExec::ExecToLog 'taskkill /IM "${PRODUCT_EXECUTABLE}" /T /F'
    Pop $0

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
