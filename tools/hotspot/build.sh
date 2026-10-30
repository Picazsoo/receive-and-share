#!/bin/sh
# Ubuntu/WSL prerequisites: gcc-arm-linux-gnueabihf make gcc flex bison bzip2 curl
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
BUILD="$ROOT/dist/hotspot-native"
mkdir -p "$BUILD"
cd "$BUILD"
fetch() { [ -f "$1" ] || curl -fL "$2" -o "$1"; }
fetch musl-1.2.5.tar.gz https://musl.libc.org/releases/musl-1.2.5.tar.gz
fetch libnl-3.11.0.tar.gz https://github.com/thom311/libnl/releases/download/libnl3_11_0/libnl-3.11.0.tar.gz
fetch hostapd-2.11.tar.gz https://w1.fi/releases/hostapd-2.11.tar.gz
fetch busybox-1.37.0.tar.bz2 https://busybox.net/downloads/busybox-1.37.0.tar.bz2
sha256sum -c "$ROOT/tools/hotspot/SHA256SUMS"
WORK=$(mktemp -d /tmp/receive-share-native.XXXXXX)
printf '%s\n' "$WORK" > "$BUILD/build-work.txt"
for archive in musl-1.2.5.tar.gz libnl-3.11.0.tar.gz hostapd-2.11.tar.gz busybox-1.37.0.tar.bz2; do tar xf "$archive" -C "$WORK"; done
cd "$WORK"
cd musl-1.2.5
./configure --target=arm-linux-gnueabihf --prefix="$WORK/musl" --disable-shared CC=arm-linux-gnueabihf-gcc > "$WORK/musl-config.log" 2>&1
make -j4 > "$WORK/musl-build.log" 2>&1
make install > "$WORK/musl-install.log" 2>&1
cp -a /usr/arm-linux-gnueabihf/include/linux /usr/arm-linux-gnueabihf/include/asm /usr/arm-linux-gnueabihf/include/asm-generic "$WORK/musl/include/"
export HOTSPOT_CC="$WORK/musl/bin/musl-gcc"
cd "$WORK/libnl-3.11.0"
./configure --host=arm-linux-gnueabihf --prefix="$WORK/sysroot" --disable-shared --enable-static --disable-cli --disable-pthreads CC="$HOTSPOT_CC" > "$BUILD/libnl-config.log" 2>&1
make -j4 > "$BUILD/libnl-build.log" 2>&1
make install > "$BUILD/libnl-install.log" 2>&1
cd "$WORK/hostapd-2.11/hostapd"
cp "$ROOT/tools/hotspot/hostapd.config" .config
make -j4 > "$BUILD/hostapd-build.log" 2>&1
cp hostapd "$BUILD/hostapd"
cd "$WORK/busybox-1.37.0"
make allnoconfig > "$BUILD/busybox-config.log" 2>&1
sed -i 's/# CONFIG_UDHCPD is not set/CONFIG_UDHCPD=y/;s/# CONFIG_STATIC is not set/CONFIG_STATIC=y/;s/# CONFIG_LFS is not set/CONFIG_LFS=y/' .config
yes '' | make oldconfig >> "$BUILD/busybox-config.log" 2>&1
make -j4 CROSS_COMPILE=arm-linux-gnueabihf- CC="$HOTSPOT_CC" > "$BUILD/busybox-build.log" 2>&1
cp busybox "$BUILD/hotspot-dhcp"
arm-linux-gnueabihf-strip "$BUILD/hostapd" "$BUILD/hotspot-dhcp"
cp "$WORK/musl-1.2.5/COPYRIGHT" "$BUILD/LICENSE-musl"
cp "$WORK/libnl-3.11.0/COPYING" "$BUILD/LICENSE-libnl"
cp "$WORK/hostapd-2.11/COPYING" "$BUILD/LICENSE-hostapd"
cp "$WORK/busybox-1.37.0/LICENSE" "$BUILD/LICENSE-busybox"
echo 'Static musl ARM hotspot helpers built.'
