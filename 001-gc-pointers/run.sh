#!/usr/bin/env bash
# Воспроизводимый запуск замера 001. Скрипт сам переходит в свою папку, запускать можно откуда угодно:
#   ./001-gc-pointers/run.sh build    # два бинарника: green и nogreen
#   ./001-gc-pointers/run.sh env      # окружение в logs/env.txt
#   ./001-gc-pointers/run.sh scaling  # быстрая серия: runtime.GC() на разных n (~10 мин)
#   ./001-gc-pointers/run.sh timer    # матрица сборок по таймеру, 10 млн (~2,5 ч)
#   ./001-gc-pointers/run.sh timer-extra  # догон до трёх прогонов: p, b2, b·nogreen (~70 мин)
#   ./001-gc-pointers/run.sh report   # data.json и картинки в img/
# Прогоны идут строго по очереди, машину в это время не нагружать.
set -euo pipefail
cd "$(dirname "$0")"

BIN=bin
LOGS=logs

run() { # run <сборка> <имя лога> <аргументы программы...>
	local build=$1 name=$2
	shift 2
	echo "$(date +%T) $name"
	GODEBUG=gctrace=1 /usr/bin/time -v -o "$LOGS/${name}_time.log" \
		"$BIN/gcexp-$build" "$@" 2>"$LOGS/$name.log"
}

case "${1:-}" in
build)
	mkdir -p "$BIN"
	go build -o "$BIN/gcexp-green" .
	GOEXPERIMENT=nogreenteagc go build -o "$BIN/gcexp-nogreen" .
	;;
env)
	mkdir -p "$LOGS"
	{
		lscpu | grep "Model name"
		echo "nproc: $(nproc)"
		free -h | head -2
		uname -sr
		go version
		go env GOOS GOARCH GOAMD64
		grep -qi microsoft /proc/version && echo "WSL2" || echo "нативный Linux"
	} >"$LOGS/env.txt"
	;;
scaling)
	mkdir -p "$LOGS/scaling"
	for build in green nogreen; do
		for v in a p b2 b; do
			for n in 100000 300000 1000000 3000000 10000000; do
				run "$build" "scaling/$v-$build-$n" -variant "$v" -n "$n" -mode forced -forced 8
			done
		done
	done
	;;
timer)
	mkdir -p "$LOGS/timer"
	for i in 1 2 3; do
		run green "timer/a-green-$i" -variant a
		run green "timer/b-green-$i" -variant b
		run nogreen "timer/a-nogreen-$i" -variant a
	done
	run nogreen timer/b-nogreen-1 -variant b
	run green timer/p-green-1 -variant p
	run green timer/b2-green-1 -variant b2
	;;
timer-extra)
	mkdir -p "$LOGS/timer"
	for i in 2 3; do
		run green "timer/p-green-$i" -variant p
		run green "timer/b2-green-$i" -variant b2
		run nogreen "timer/b-nogreen-$i" -variant b
	done
	;;
report)
	(cd report && go run .)
	;;
*)
	echo "использование: $0 build | env | scaling | timer | timer-extra | report" >&2
	exit 2
	;;
esac
