#!/bin/sh
interval="${1:-5}"
printf "%-8s %-20s %10s %6s %6s\n" "TIME" "PROCESS" "RSS(MB)" "THR" "FD"
echo "-------------------------------------------------------"
while true; do
    pids=$(pgrep -f "headless-" 2>/dev/null)
    if [ -z "$pids" ]; then
        printf "%-8s %s\n" "$(date +%H:%M:%S)" "no processes found"
    else
        for pid in $pids; do
            name=$(ps -o comm= -p "$pid" 2>/dev/null | xargs basename 2>/dev/null)
            rss=$(ps -o rss= -p "$pid" 2>/dev/null | tr -d ' ')
            rss_mb=$(awk "BEGIN{printf \"%.3f\", $rss/1024}")
            thr=$(ps -M "$pid" 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
            fd=$(lsof -p "$pid" 2>/dev/null | wc -l | tr -d ' ')
            printf "%-8s %-20s %10s %6s %6s\n" "$(date +%H:%M:%S)" "$name($pid)" "${rss_mb}" "$thr" "$fd"
        done
    fi
    echo ""
    sleep "$interval"
done
