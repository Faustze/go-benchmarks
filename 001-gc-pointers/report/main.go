// Команда report разбирает логи замера 001 и рисует графики для README.
//
// Запуск: ./001-gc-pointers/run.sh report (или go run . из этой папки).
// Пишет ../data.json (его читает страница замера) и ../img/*.png, сводку печатает в stdout.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	logsDir  = "../logs"
	imgDir   = "../img"
	dataFile = "../data.json"
)

func main() {
	var runs []Run
	for _, sub := range []string{"scaling", "timer"} { // smoke-логи в зачёт не идут
		paths, _ := filepath.Glob(filepath.Join(logsDir, sub, "*.log"))
		for _, p := range paths {
			if strings.HasSuffix(p, "_time.log") {
				continue
			}
			if r, ok := parseLog(p); ok {
				runs = append(runs, r)
			}
		}
	}
	configs := summarize(runs)

	env, _ := os.ReadFile(filepath.Join(logsDir, "env.txt"))
	b, err := json.MarshalIndent(map[string]any{"env": string(env), "configs": configs}, "", " ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(dataFile, b, 0o644); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%-7s %-3s %-8s %9s %4s %9s %9s %7s %5s %10s %6s %12s\n",
		"mode", "var", "build", "n", "gcs", "mark ms", "cpu ms", "stw max", "live", "objects", "rss", "interval, s")
	for _, c := range configs {
		iv := ""
		if c.Mode == "timer" {
			iv = fmt.Sprintf("%.0f..%.0f", c.Interval.Min, c.Interval.Max)
		}
		fmt.Printf("%-7s %-3s %-8s %9d %4d %9.2f %9.2f %7.3f %5.0f %10d %6.0f %12s\n",
			c.Mode, c.Variant, c.Build, c.N, c.GCs, c.MarkClock.Median, c.MarkCPU.Median, c.STW.Max,
			c.Live.Median, c.Objects, c.RSSMB, iv)
	}

	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := drawAll(configs); err != nil {
		log.Fatal(err)
	}
}
