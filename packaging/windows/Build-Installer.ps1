[CmdletBinding()]
param(
    [ValidatePattern('^\d+\.\d+\.\d+(?:\.\d+)?$')]
    [string] $Version = '0.1.0',
    [string] $OutputDirectory = ''
)

$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'The Windows installer must be built on Windows with PowerShell 7.' }

$repo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $repo 'dist\windows-installer' }
$output = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $output | Out-Null

$iscc = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
if (-not $iscc) {
    $iscc = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
    ) | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
}
if (-not $iscc) { throw 'Install Inno Setup 6, or add ISCC.exe to PATH.' }

function Save-MicrosoftInstaller([string] $Url, [string] $Destination) {
    Invoke-WebRequest -Uri $Url -OutFile $Destination -MaximumRetryCount 3 -RetryIntervalSec 3
    $signature = Get-AuthenticodeSignature -LiteralPath $Destination
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch '(?:^|,\s*)O=Microsoft Corporation(?:,|$)') {
        throw "Microsoft signature could not be verified for $(Split-Path $Destination -Leaf). Status: $($signature.Status)"
    }
}

$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\')
$stage = Join-Path $tempRoot ('BUCT-Package-' + [Guid]::NewGuid().ToString('N'))
$publish = Join-Path $stage 'publish'
$prerequisites = Join-Path $stage 'prerequisites'
New-Item -ItemType Directory -Path $prerequisites | Out-Null

try {
    $project = Join-Path $repo 'desktop\BuctCourseLocal.csproj'
    & dotnet publish $project -c Release -r win-x64 --self-contained true -p:Version=$Version -o $publish
    if ($LASTEXITCODE -ne 0) { throw "dotnet publish failed ($LASTEXITCODE)." }

    Save-MicrosoftInstaller 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' (Join-Path $prerequisites 'MicrosoftEdgeWebview2Setup.exe')
    Save-MicrosoftInstaller 'https://aka.ms/vc14/vc_redist.x64.exe' (Join-Path $prerequisites 'vc_redist.x64.exe')

    & $iscc /Qp "/DAppVersion=$Version" "/DInstallerOutput=$output" "/DPublishDirectory=$publish" `
        "/DPrerequisiteDirectory=$prerequisites" (Join-Path $PSScriptRoot 'Setup.iss')
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed ($LASTEXITCODE)." }

    $installer = Join-Path $output "BUCT-Course-Companion-$Version-Setup-x64.exe"
    Get-Item -LiteralPath $installer
}
finally {
    if (Test-Path -LiteralPath $stage) {
        $resolvedStage = (Resolve-Path -LiteralPath $stage).Path
        if ((Split-Path $resolvedStage -Parent) -eq $tempRoot -and (Split-Path $resolvedStage -Leaf) -match '^BUCT-Package-[0-9a-f]{32}$') {
            Remove-Item -LiteralPath $resolvedStage -Recurse -Force
        }
    }
}
