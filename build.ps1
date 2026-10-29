param([string]$Go)
$ErrorActionPreference = 'Stop'
if (-not $Go) {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue
    if ($goCommand) {
        $Go = $goCommand.Source
    } else {
        foreach ($candidate in @("$PSScriptRoot\.toolchain\go\bin\go.exe", "$PSScriptRoot\..\.toolchain\go\bin\go.exe")) {
            if (Test-Path -LiteralPath $candidate) { $Go = (Resolve-Path -LiteralPath $candidate).Path; break }
        }
    }
}
if (-not $Go) { throw 'Install Go or supply -Go with the compiler path.' }
$env:GOPATH = "$PSScriptRoot\receiver\.gomodcache"
$env:GOCACHE = "$PSScriptRoot\receiver\.gocache"
$env:CGO_ENABLED = '0'
New-Item -ItemType Directory -Force "$PSScriptRoot\dist" | Out-Null
Push-Location "$PSScriptRoot\receiver"
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
    & $Go test ./...
    if ($LASTEXITCODE) { throw 'Tests failed' }
    & $Go vet ./...
    if ($LASTEXITCODE) { throw 'Vet failed' }
    & $Go build -o ../dist/kobo-receive-host.exe .
    if ($LASTEXITCODE) { throw 'Host build failed' }
    & node smoke.mjs
    if ($LASTEXITCODE) { throw 'HTTP smoke test failed' }
    $env:GOOS = 'linux'; $env:GOARCH = 'arm'; $env:GOARM = '7'
    & $Go build -trimpath -ldflags '-s -w' -o ../package/.adds/kobo-receive/kobo-receive .
    if ($LASTEXITCODE) { throw 'ARM build failed' }
    $licenseDir = "$PSScriptRoot\package\.adds\kobo-receive\licenses"
    New-Item -ItemType Directory -Force $licenseDir | Out-Null
    & $Go mod download all
    if ($LASTEXITCODE) { throw 'Could not download dependency licenses' }
    $moduleLines = & $Go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' all
    if ($LASTEXITCODE) { throw 'Could not list dependency licenses' }
    $notices = @()
    foreach ($moduleLine in $moduleLines) {
        $moduleParts = $moduleLine -split '\|', 3
        if ($moduleParts[0] -eq 'kobo-receive') { continue }
        $notices += "$($moduleParts[0]) $($moduleParts[1])"
        Get-ChildItem -LiteralPath $moduleParts[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING)' } | ForEach-Object {
            $licenseName = $moduleParts[0].Replace('/', '_') + '-' + $_.Name
            Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $licenseDir $licenseName) -Force
        }
    }
    [IO.File]::WriteAllLines((Join-Path $licenseDir 'DEPENDENCIES.txt'), $notices)
} finally { Pop-Location }
Copy-Item "$PSScriptRoot\README.md" "$PSScriptRoot\package\README.md" -Force
New-Item -ItemType Directory -Force "$PSScriptRoot\package\docs\images" | Out-Null
Copy-Item "$PSScriptRoot\docs\images\kobo-screen.png" "$PSScriptRoot\package\docs\images\kobo-screen.png" -Force
Copy-Item "$PSScriptRoot\receiver\.gomodcache\pkg\mod\github.com\skip2\go-qrcode@v0.0.0-20200617195104-da1b6568686e\LICENSE" "$PSScriptRoot\package\.adds\kobo-receive\QR-LICENSE.txt" -Force
# ZipArchive includes dot directories, which Compress-Archive may omit.
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zipPath = "$PSScriptRoot\dist\kobo-receive-libra2.zip"
$stream = [IO.File]::Open($zipPath, [IO.FileMode]::Create)
$archive = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create)
try {
    $packageRoot = (Resolve-Path "$PSScriptRoot\package").Path
    Get-ChildItem -LiteralPath $packageRoot -Recurse -File -Force | ForEach-Object {
        $entry = $_.FullName.Substring($packageRoot.Length + 1).Replace('\','/')
        [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, $_.FullName, $entry) | Out-Null
    }
} finally { $archive.Dispose(); $stream.Dispose() }
Get-FileHash -LiteralPath $zipPath -Algorithm SHA256
