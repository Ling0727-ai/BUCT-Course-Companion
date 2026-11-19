#define ProductName "北化课伴"
#define ExeName "BuctCourseLocal.exe"

[Setup]
AppId={{4BB2BD43-EEB2-4CCB-AD6A-5D5013CEB999}
AppName={#ProductName}
AppVersion={#AppVersion}
AppPublisher=LingXin
AppPublisherURL=https://github.com/Ling0727-ai/BUCT-Course-Companion
VersionInfoVersion={#AppVersion}
VersionInfoDescription=BUCT Course Companion Setup
VersionInfoCompany=LingXin
DefaultDirName={autopf}\BUCT Course Companion
DefaultGroupName={#ProductName}
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#ExeName}
SetupIconFile=..\..\desktop\Assets\CourseFlow.ico
OutputDir={#InstallerOutput}
OutputBaseFilename=BUCT-Course-Companion-{#AppVersion}-Setup-x64
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
PrivilegesRequired=admin
AppMutex=Local\BuctCourseLocal
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "{#PublishDirectory}\*"; DestDir: "{app}"; Excludes: "*.pdb,x86\*"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#PrerequisiteDirectory}\MicrosoftEdgeWebview2Setup.exe"; Flags: dontcopy
Source: "{#PrerequisiteDirectory}\vc_redist.x64.exe"; Flags: dontcopy

[Icons]
Name: "{group}\{#ProductName}"; Filename: "{app}\{#ExeName}"; WorkingDir: "{app}"
Name: "{autodesktop}\{#ProductName}"; Filename: "{app}\{#ExeName}"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#ExeName}"; Description: "Launch {#ProductName}"; Flags: nowait postinstall skipifsilent

[Code]
const
  WebView2Key = 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}';
  VcRuntimeKey = 'Software\Microsoft\VisualStudio\14.0\VC\Runtimes\x64';

var
  PrerequisiteNeedsRestart: Boolean;

function HasWebView2At(Root: Integer): Boolean;
var
  Version: String;
begin
  Result := RegQueryStringValue(Root, WebView2Key, 'pv', Version) and
    (Version <> '') and (Version <> '0.0.0.0');
end;

function HasWebView2: Boolean;
begin
  Result := HasWebView2At(HKLM32) or HasWebView2At(HKLM64) or HasWebView2At(HKCU);
end;

function HasVcRuntimeAt(Root: Integer): Boolean;
var
  Installed, Major, Minor: Cardinal;
begin
  Result := RegQueryDWordValue(Root, VcRuntimeKey, 'Installed', Installed) and (Installed = 1) and
    RegQueryDWordValue(Root, VcRuntimeKey, 'Major', Major) and
    RegQueryDWordValue(Root, VcRuntimeKey, 'Minor', Minor) and
    ((Major > 14) or ((Major = 14) and (Minor >= 29)));
end;

function RunPrerequisite(const FileName, Parameters: String; var ResultCode: Integer): Boolean;
begin
  ExtractTemporaryFile(FileName);
  Result := Exec(ExpandConstant('{tmp}\') + FileName, Parameters, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  Result := '';
  if not (HasVcRuntimeAt(HKLM64) or HasVcRuntimeAt(HKLM32)) then begin
    if not RunPrerequisite('vc_redist.x64.exe', '/install /quiet /norestart', Code) then
      Result := 'Could not start the Microsoft Visual C++ Redistributable installer.'
    else if (Code = 3010) or (Code = 1641) then
      PrerequisiteNeedsRestart := True
    else if (Code <> 0) and (Code <> 1638) then
      Result := 'Microsoft Visual C++ Redistributable installation failed (' + IntToStr(Code) + ').';
    if Result <> '' then Exit;
  end;

  if not HasWebView2 then begin
    if not RunPrerequisite('MicrosoftEdgeWebview2Setup.exe', '/silent /install', Code) then
      Result := 'Could not start the Microsoft Edge WebView2 Runtime installer.'
    else if not HasWebView2 then
      Result := 'Microsoft Edge WebView2 Runtime installation did not finish (' + IntToStr(Code) + '). Check your internet connection and retry.';
  end;
end;

function NeedRestart: Boolean;
begin
  Result := PrerequisiteNeedsRestart;
end;
