; Inno Setup 6 installer script for Optical Analyzer (Windows x64)
; Build with:
;   iscc /DAppVersion=1.0.0 packaging\windows\optical-analyzer.iss

#ifndef AppVersion
  #define AppVersion "1.0.0"
#endif

#ifndef SourceDir
  #define SourceDir "..\..\dist\optical-analyzer-windows-amd64"
#endif

#ifndef OutputDir
  #define OutputDir "..\.."
#endif

[Setup]
AppId={{8E9B7F5A-4C2D-4F1E-9B61-2D8C4A7E5F01}
AppName=Optical Analyzer
AppVersion={#AppVersion}
AppPublisher=John Southworth
AppPublisherURL=https://github.com/jsouthworth/optical-analyzer
AppSupportURL=https://github.com/jsouthworth/optical-analyzer/issues
AppUpdatesURL=https://github.com/jsouthworth/optical-analyzer/releases
DefaultDirName={autopf}\Optical Analyzer
DefaultGroupName=Optical Analyzer
AllowNoIcons=yes
LicenseFile=..\..\LICENSE
OutputDir={#OutputDir}
OutputBaseFilename=optical-analyzer-windows-amd64-setup
SetupIconFile=optical-analyzer.ico
UninstallDisplayIcon={app}\optical-analyzer.exe
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ChangesEnvironment=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "addtopath"; Description: "Add Optical Analyzer CLI tools to PATH"; GroupDescription: "Command-line tools:"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\Optical Analyzer"; Filename: "{app}\optical-analyzer.exe"
Name: "{autodesktop}\Optical Analyzer"; Filename: "{app}\optical-analyzer.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\optical-analyzer.exe"; Description: "{cm:LaunchProgram,Optical Analyzer}"; Flags: nowait postinstall skipifsilent

[Code]
function EnvKeyRoot(): Integer;
begin
  if IsAdminInstallMode then
    Result := HKLM
  else
    Result := HKCU;
end;

function EnvKeyPath(): string;
begin
  if IsAdminInstallMode then
    Result := 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment'
  else
    Result := 'Environment';
end;

procedure AddDirToPath(Dir: string);
var
  OldPath, NewPath: string;
begin
  if not RegQueryStringValue(EnvKeyRoot(), EnvKeyPath(), 'Path', OldPath) then
    OldPath := '';

  if Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(OldPath) + ';') > 0 then
    Exit;

  if (OldPath = '') or (OldPath[Length(OldPath)] = ';') then
    NewPath := OldPath + Dir
  else
    NewPath := OldPath + ';' + Dir;

  RegWriteExpandStringValue(EnvKeyRoot(), EnvKeyPath(), 'Path', NewPath);
end;

procedure RemoveDirFromPath(Dir: string);
var
  OldPath, NewPath, Entry: string;
  P: Integer;
begin
  if not RegQueryStringValue(EnvKeyRoot(), EnvKeyPath(), 'Path', OldPath) then
    Exit;

  NewPath := '';
  while OldPath <> '' do
  begin
    P := Pos(';', OldPath);
    if P > 0 then
    begin
      Entry := Copy(OldPath, 1, P - 1);
      Delete(OldPath, 1, P);
    end
    else
    begin
      Entry := OldPath;
      OldPath := '';
    end;

    if (Entry <> '') and (Uppercase(Entry) <> Uppercase(Dir)) then
    begin
      if NewPath <> '' then
        NewPath := NewPath + ';';
      NewPath := NewPath + Entry;
    end;
  end;

  RegWriteExpandStringValue(EnvKeyRoot(), EnvKeyPath(), 'Path', NewPath);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('addtopath') then
    AddDirToPath(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveDirFromPath(ExpandConstant('{app}'));
end;
