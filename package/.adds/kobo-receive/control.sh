#!/bin/sh
APP=/mnt/onboard/.adds/kobo-receive
PIDFILE="$APP/receiver.pid"
READY="$APP/ready"

running() {
    [ -f "$PIDFILE" ] || return 1
    read -r receiver_pid < "$PIDFILE"
    case "$receiver_pid" in ''|*[!0-9]*) return 1 ;; esac
    [ -r "/proc/$receiver_pid/cmdline" ] || return 1
    tr '\000' '\n' < "/proc/$receiver_pid/cmdline" | grep -qx "$APP/kobo-receive" || return 1
    kill -0 "$receiver_pid" 2>/dev/null
}

# Keep a running session on its original port when configuration changes.
PORT=8080
PORTFILE="$APP/port.txt"
if running && [ -f "$APP/active-port" ]; then
    PORTFILE="$APP/active-port"
fi
if [ -f "$PORTFILE" ]; then
    PORT=$(tr -d '\r\n' < "$PORTFILE")
    case "$PORT" in ''|0*|*[!0-9]*) echo 'Invalid port: use 1-65535.'; exit 1 ;; esac
    if [ "${#PORT}" -gt 5 ] || [ "$PORT" -gt 65535 ]; then
        echo 'Invalid port: use 1-65535.'; exit 1
    fi
fi

check_local() {
    echo '=== Loopback after setup ==='
    /sbin/ifconfig lo
    echo '=== HTTP health check ==='
    if command -v wget >/dev/null 2>&1; then
        wget -T 2 -O /tmp/kobo-receive-health.txt http://127.0.0.1:$PORT/health || return 1
        grep -qx 'kobo-receive' /tmp/kobo-receive-health.txt || return 1
        echo 'Local HTTP connection works.'
    else
        echo 'wget is not available; cannot verify the browser connection.'
        return 1
    fi
}

wifi_connected() {
    [ "$(cat /sys/class/net/wlan0/carrier 2>/dev/null)" = 1 ] &&
        awk '$1 == "wlan0" && $2 == "00000000" { found=1 } END { exit !found }' /proc/net/route
}

prepare_browser() {
    token=$(wget -T 2 -qO- http://127.0.0.1:$PORT/screen | sed -n 's/.*name="token" value="\([^"]*\)".*/\1/p')
    case "$token" in ''|*[!a-f0-9]*) return 1 ;; esac
    wget -T 2 -qO /tmp/kobo-receive-reopen.txt --post-data="token=$token" http://127.0.0.1:$PORT/local-reopen
}

case "$1" in
    open|open-hotspot)
        exec > "$APP/open.log" 2>&1 < /dev/null
        MODE=wifi
        [ "$1" = open-hotspot ] && MODE=hotspot
        if running && [ "$(cat "$APP/active-mode" 2>/dev/null)" != "$MODE" ]; then
            /usr/bin/qndb -m mwcToast 5000 'Finish the current session before switching Wi-Fi mode.'
            exit 1
        fi
        if [ "$MODE" = wifi ] && ! wifi_connected; then
            /usr/bin/qndb -m mwcToast 5000 'Turn on Wi-Fi, connect, then try again.'
            exit 1
        fi
        /usr/bin/qndb -m mwcToast 5000 'Starting Receive & Share...'
        START=start
        [ "$MODE" = hotspot ] && START=start-hotspot
        if ! /bin/sh "$APP/control.sh" "$START"; then
            if [ "$MODE" = hotspot ]; then
                /usr/bin/qndb -m mwcToast 5000 'Could not start hotspot. See receiver.log.'
            else
                /usr/bin/qndb -m mwcToast 5000 'Could not start. Check Wi-Fi and try again.'
            fi
            exit 1
        fi
        if ! prepare_browser; then
            /usr/bin/qndb -m mwcToast 5000 'Session is finishing. Wait a moment and try again.'
            exit 1
        fi
        if [ "$MODE" = hotspot ]; then
            if ! check_local; then exit 1; fi
            exec wget -T 3 -qO /tmp/kobo-receive-hotspot-open.txt --post-data="token=$token" http://127.0.0.1:$PORT/local-hotspot-open
        fi
        # Native browser startup itself requests Wi-Fi. Wait for the temporary
        # ForceWifiOn reconnect to finish so it does not open a connection dialog.
        stable=0
        for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
            if [ "$(cat /sys/class/net/wlan0/carrier 2>/dev/null)" = 1 ] &&
                awk '$1 == "wlan0" && $2 == "00000000" { found=1 } END { exit !found }' /proc/net/route; then
                stable=$((stable + 1))
            else
                stable=0
            fi
            if [ "$stable" -ge 3 ]; then
                if ! check_local; then
                    /usr/bin/qndb -m mwcToast 5000 'The session stopped. Launch Receive & Share again.'
                    exit 1
                fi
                exec /usr/bin/qndb -m bwmOpenBrowser true http://127.0.0.1:$PORT/screen
            fi
            sleep 1
        done
        /usr/bin/qndb -m mwcToast 5000 'Wi-Fi is still connecting. Connect, then try again.'
        exit 1
        ;;
    start|start-hotspot)
        exec > /tmp/kobo-receive-start.log 2>&1
        date
        echo 'Launcher revision: hotspot-1'
        MODE=wifi
        [ "$1" = start-hotspot ] && MODE=hotspot
        if [ "$MODE" = wifi ] && ! wifi_connected; then
            echo 'Turn on Wi-Fi, connect to a network, then launch Receive & Share again.'
            exit 1
        fi
        # Stock Kobo firmware may leave loopback down and without an IPv4 address.
        # The QR browser uses 127.0.0.1, independently of the Wi-Fi upload address.
        /sbin/ifconfig lo 127.0.0.1 up || {
            echo 'Could not enable the local browser connection.'
            exit 1
        }
        if running && [ -f "$READY" ]; then
            if [ "$(cat "$APP/active-mode" 2>/dev/null)" != "$MODE" ]; then
                echo 'Finish the current session before switching Wi-Fi mode.'
                exit 1
            fi
            echo 'Using existing receiver.'
            check_local
            exit $?
        fi
        if running; then
            echo 'The session is starting or stopping. Try again in a moment.'
            exit 1
        fi
        rm -f "$READY"
        chmod +x "$APP/kobo-receive" || exit 1
        printf '%s\n' "$PORT" > "$APP/active-port"
        printf '%s\n' "$MODE" > "$APP/active-mode"
        if [ "$MODE" = hotspot ]; then
            "$APP/kobo-receive" -hotspot -port "$PORT" -ready "$READY" > "$APP/receiver.log" 2>&1 < /dev/null &
        else
            "$APP/kobo-receive" -port "$PORT" -ready "$READY" > "$APP/receiver.log" 2>&1 < /dev/null &
        fi
        receiver_pid=$!
        echo "$receiver_pid" > "$PIDFILE"
        attempt=0
        while [ "$attempt" -lt 60 ]; do
            attempt=$((attempt + 1))
            if [ -f "$READY" ] && kill -0 "$receiver_pid" 2>/dev/null; then
                check_local
                exit $?
            fi
            kill -0 "$receiver_pid" 2>/dev/null || break
            if [ "$attempt" = 15 ] || [ "$attempt" = 30 ] || [ "$attempt" = 45 ]; then
                /usr/bin/qndb -m mwcToast 5000 'Still starting Receive & Share...'
            fi
            sleep 1
        done
        echo 'Receiver could not start. Connect Wi-Fi first and try again.'
        cat "$APP/receiver.log"
        exit 1
        ;;
    stop)
        if running; then
            kill "$receiver_pid" || exit 1
            for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do
                kill -0 "$receiver_pid" 2>/dev/null || break
                sleep 1
            done
            if kill -0 "$receiver_pid" 2>/dev/null; then
                echo 'Receiver is still stopping. Try Finish & refresh library again.'
                exit 1
            fi
        fi
        rm -f "$PIDFILE" "$READY" "$APP/active-port" "$APP/active-mode"
        sync
        ;;
    *) echo 'Use open, open-hotspot, start, start-hotspot or stop'; exit 1 ;;
esac
