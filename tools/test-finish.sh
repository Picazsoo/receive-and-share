#!/bin/sh
set -eu
APP=/mnt/onboard/.adds/kobo-receive
CHECK=/tmp/kobo-receive-finish-check
umask 077
PORT=$(cat "$APP/active-port" 2>/dev/null || echo 8080)
case "$PORT" in ''|*[!0-9]*) echo 'Invalid active port'; exit 1 ;; esac
wget -T 3 -q -O "$CHECK-screen.html" http://127.0.0.1:$PORT/screen
token=$(sed -n 's/.*name="token" value="\([^"]*\)".*/\1/p' "$CHECK-screen.html")
case "$token" in ''|*[!a-f0-9]*) echo 'No valid finish token on the QR page'; exit 1 ;; esac
/usr/bin/qndb -t 7000 -s pfmDoneProcessing -m ndbVersion > /tmp/kobo-receive-import-signal.txt 2>&1 &
signal_pid=$!
sleep 1
wget -T 20 -O "$CHECK-result.html" --header='Origin: http://www.kobo.com' --header='Sec-Fetch-Site: cross-site' --post-data="token=$token" http://127.0.0.1:$PORT/finish
wait "$signal_pid"
echo 'Kobo native import signal:'
cat /tmp/kobo-receive-import-signal.txt
echo 'Finish page result:'
grep -o '<h1>[^<]*' "$CHECK-result.html"
echo 'Receiver log:'
tail -20 "$APP/receiver.log"
