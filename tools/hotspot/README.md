# Hotspot helpers

Receive & Share starts unmodified hostapd 2.11 and BusyBox 1.37.0 (`udhcpd`
only). They are linked statically with musl 1.2.5; hostapd also uses libnl
3.11.0. No Kobo system libraries are replaced.

Licenses: hostapd BSD, BusyBox GPLv2, libnl LGPL-2.1, musl MIT. Original
notices are in the app's `licenses` folder and in these source archives.
The app's original code remains MIT. The archives, configuration and build
script are included so the helpers and their static libraries can be rebuilt
or modified. Keep this source bundle with redistributed helper binaries.

On Ubuntu 24.04, install `gcc-arm-linux-gnueabihf make gcc flex bison bzip2 curl`.
From the project root, run `sh tools/hotspot/build.sh`, or `./build-hotspot.ps1`
on Windows with WSL Ubuntu. Sources are downloaded over HTTPS and checked
against `SHA256SUMS`. Build output goes to `dist/hotspot-native`.

To rebuild from just this source bundle, place `build.sh`, `hostapd.config`
and `SHA256SUMS` in `tools/hotspot` under an empty working directory. Place
the four source archives in `dist/hotspot-native`, then run the build script.
The generated binaries are `dist/hotspot-native/hostapd` and `hotspot-dhcp`.

Hotspot mode is experimental and device-dependent. It has been tested on a
Kobo Libra 2 with firmware 4.38.23697 and the stock RTL8723DS driver.
