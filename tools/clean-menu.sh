#!/bin/sh
set -eu
APP=/mnt/onboard/.adds/kobo-receive
MENU=/mnt/onboard/.adds/nm
[ -s "$APP/control.sh.new" ] && [ -s "$APP/menu-clean.new" ]
/bin/sh -n "$APP/control.sh.new"
mkdir -p "$APP/retired-menu"
stamp=$(date +%Y%m%d%H%M%S)
if [ -f "$APP/control.sh" ]; then cp "$APP/control.sh" "$APP/retired-menu/control-$stamp.sh"; fi
chmod 755 "$APP/control.sh.new"
mv "$APP/control.sh.new" "$APP/control.sh"
mv "$APP/menu-clean.new" "$MENU/kobo-receive"
for name in airdrop-diagnostics receive-check receiver-inline-test receive-books-v4; do
    if [ -f "$MENU/$name" ]; then mv "$MENU/$name" "$APP/retired-menu/$name-$stamp"; fi
done
if [ -f "$APP/control-v4.sh" ]; then mv "$APP/control-v4.sh" "$APP/retired-menu/control-v4-$stamp.sh"; fi
echo 'Remaining NickelMenu files:'
ls "$MENU"
