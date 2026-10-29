param([string]$Go, [switch]$SkipBuild)
$ErrorActionPreference = 'Stop'
if (-not $Go) {
    $candidate = Join-Path $PSScriptRoot '..\.toolchain\go\bin\go.exe'
    if (Test-Path -LiteralPath $candidate) { $Go = (Resolve-Path $candidate).Path }
    else { $Go = (Get-Command go -ErrorAction Stop).Source }
}
if (-not $SkipBuild) {
    & "$PSScriptRoot\build.ps1" -Go $Go
    if ($LASTEXITCODE) { throw 'Application build failed' }
}
$inputs = Join-Path $PSScriptRoot 'dist\installer-inputs'
New-Item -ItemType Directory -Force $inputs | Out-Null
$downloads = @{
    'nickelmenu-v0.6.0.tgz' = 'https://github.com/pgaskin/NickelMenu/releases/download/v0.6.0/KoboRoot.tgz'
    'nickeldbus-0.2.0.tgz' = 'https://github.com/shermp/NickelDBus/releases/download/0.2.0/KoboRoot.tgz'
    'NickelMenu-LICENSE' = 'https://raw.githubusercontent.com/pgaskin/NickelMenu/v0.6.0/LICENSE'
    'NickelDBus-LICENSE' = 'https://raw.githubusercontent.com/shermp/NickelDBus/0.2.0/LICENSE'
}
foreach ($name in $downloads.Keys) {
    Invoke-WebRequest -Uri $downloads[$name] -OutFile (Join-Path $inputs $name)
}
$env:GOPATH = "$PSScriptRoot\receiver\.gomodcache"
$env:GOCACHE = "$PSScriptRoot\receiver\.gocache"
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
$env:INSTALLER_INPUTS = $inputs
Push-Location "$PSScriptRoot\installer"
try {
    & $Go fmt ./...
    & $Go test -count=1 ./...
    if ($LASTEXITCODE) { throw 'Installer checks failed' }
    foreach ($variant in @(
        @{Mode='none'; Output='../dist/KoboRoot.tgz'},
        @{Mode='nickeldbus'; Output='../dist/with-nickeldbus/KoboRoot.tgz'},
        @{Mode='all'; Output='../dist/first-install/KoboRoot.tgz'}
    )) {
        & $Go run . -dependencies $variant.Mode -output $variant.Output
        if ($LASTEXITCODE) { throw "Failed to package $($variant.Mode)" }
    }
} finally { Pop-Location; Remove-Item Env:\INSTALLER_INPUTS -ErrorAction SilentlyContinue }
Get-FileHash "$PSScriptRoot\dist\KoboRoot.tgz", "$PSScriptRoot\dist\with-nickeldbus\KoboRoot.tgz", "$PSScriptRoot\dist\first-install\KoboRoot.tgz" -Algorithm SHA256
