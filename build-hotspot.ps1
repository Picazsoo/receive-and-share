$ErrorActionPreference = 'Stop'
$native = Join-Path $PSScriptRoot 'dist/hotspot-native'
$inputs = @('tools/hotspot/build.sh', 'tools/hotspot/hostapd.config', 'tools/hotspot/SHA256SUMS')
$fingerprint = ($inputs | ForEach-Object { (Get-FileHash (Join-Path $PSScriptRoot $_) -Algorithm SHA256).Hash }) -join ':'
$stamp = Join-Path $native 'build-inputs.txt'
if (!(Test-Path $stamp) -or (Get-Content $stamp -Raw).Trim() -ne $fingerprint -or !(Test-Path "$native/hostapd") -or !(Test-Path "$native/hotspot-dhcp")) {
    Push-Location $PSScriptRoot
    try {
        & wsl -d Ubuntu -- sh tools/hotspot/build.sh
        if ($LASTEXITCODE) { throw 'Hotspot helper build failed; see dist/hotspot-native logs. Requires WSL Ubuntu and the prerequisites in tools/hotspot/build.sh.' }
    } finally { Pop-Location }
    [IO.File]::WriteAllText($stamp, $fingerprint)
}
$app = Join-Path $PSScriptRoot 'package/.adds/kobo-receive'
Copy-Item "$native/hostapd", "$native/hotspot-dhcp" $app -Force
# Complete upstream source, build scripts and configuration accompany the native helpers.
$source = Join-Path $app 'hotspot-sources'
New-Item -ItemType Directory -Force $source | Out-Null
Copy-Item "$native/musl-1.2.5.tar.gz", "$native/libnl-3.11.0.tar.gz", "$native/hostapd-2.11.tar.gz", "$native/busybox-1.37.0.tar.bz2" $source -Force
Copy-Item ($inputs | ForEach-Object { Join-Path $PSScriptRoot $_ }) $source -Force
Copy-Item "$PSScriptRoot/tools/hotspot/README.md" $source -Force
$licenses = Join-Path $app 'licenses'
New-Item -ItemType Directory -Force $licenses | Out-Null
Copy-Item "$native/LICENSE-musl" "$licenses/hotspot-musl-COPYRIGHT" -Force
Copy-Item "$native/LICENSE-libnl" "$licenses/hotspot-libnl-COPYING" -Force
Copy-Item "$native/LICENSE-hostapd" "$licenses/hotspot-hostapd-COPYING" -Force
Copy-Item "$native/LICENSE-busybox" "$licenses/hotspot-busybox-LICENSE" -Force
Write-Output 'Hotspot helpers and corresponding upstream source packaged.'
