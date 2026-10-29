#!/bin/sh
set -eu
cd /mnt/onboard/.adds/kobo-receive
sha256sum -c /tmp/kobo-receive-new.sha256
/bin/sh control.sh stop
cp kobo-receive kobo-receive.previous
mv kobo-receive.new kobo-receive
chmod 755 kobo-receive
/bin/sh control.sh start
echo 'Receiver updated, verified, and restarted.'
