param([string]$Version = '0.1', [string]$Go, [switch]$SkipInstallers)
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^\d+\.\d+(?:\.\d+)?$') { throw 'Use a numeric release version, for example 0.1.' }
$notes = Join-Path $PSScriptRoot "docs\releases\v$Version.md"
if (-not (Test-Path -LiteralPath $notes)) { throw "Missing release notes: $notes" }
if (-not $SkipInstallers) {
    & "$PSScriptRoot\build-installers.ps1" -Go $Go
    if ($LASTEXITCODE) { throw 'Installer build failed' }
}
$dist = Join-Path $PSScriptRoot 'dist'
$installers = @(
    @{ Source = 'KoboRoot.tgz'; Target = 'app-only/KoboRoot.tgz' },
    @{ Source = 'with-nickeldbus/KoboRoot.tgz'; Target = 'with-nickeldbus/KoboRoot.tgz' },
    @{ Source = 'first-install/KoboRoot.tgz'; Target = 'first-install/KoboRoot.tgz' }
)
$checksums = foreach ($item in $installers) {
    $hash = (Get-FileHash -LiteralPath (Join-Path $dist $item.Source) -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $($item.Target)"
}
$utf8 = [Text.UTF8Encoding]::new($false)
$checksumText = ($checksums -join "`n") + "`n"
[IO.File]::WriteAllText((Join-Path $dist "RELEASE-v$Version-SHA256SUMS.txt"), $checksumText, $utf8)
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zipPath = Join-Path $dist "Receive-Share-v$Version.zip"
$temporary = "$zipPath.tmp"
$stream = [IO.File]::Open($temporary, [IO.FileMode]::Create)
$archive = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create)
try {
    foreach ($item in $installers) {
        [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, (Join-Path $dist $item.Source), $item.Target) | Out-Null
    }
    [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, $notes, 'README.md') | Out-Null
    [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, (Join-Path $PSScriptRoot 'LICENSE'), 'LICENSE') | Out-Null
    $entry = $archive.CreateEntry('SHA256SUMS.txt')
    $writer = [IO.StreamWriter]::new($entry.Open(), $utf8)
    try { $writer.Write($checksumText) } finally { $writer.Dispose() }
} finally { $archive.Dispose(); $stream.Dispose() }
# Verify the packaged installers against their original hashes before publishing the ZIP locally.
$archive = [IO.Compression.ZipFile]::OpenRead($temporary)
try {
    foreach ($item in $installers) {
        $inputStream = $archive.GetEntry($item.Target).Open()
        $sha = [Security.Cryptography.SHA256]::Create()
        try { $actual = [Convert]::ToHexString($sha.ComputeHash($inputStream)).ToLowerInvariant() }
        finally { $inputStream.Dispose(); $sha.Dispose() }
        $expected = (Get-FileHash -LiteralPath (Join-Path $dist $item.Source) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { throw "ZIP verification failed: $($item.Target)" }
    }
} finally { $archive.Dispose() }
Move-Item -LiteralPath $temporary -Destination $zipPath -Force
$zipHash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
[IO.File]::WriteAllText("$zipPath.sha256", "$zipHash  $([IO.Path]::GetFileName($zipPath))`n", $utf8)
Write-Output "Release bundle: $zipPath"
Write-Output "SHA256: $zipHash"
