package main

import (
	"bufio"
	"gonum.org/v1/gonum/floats"
	"gonum.org/v1/gonum/stat"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// gc 5 @121.053s 0%: 0.040+5.5+0.004 ms clock, 0.32+0/11/0+0.036 ms cpu, 10->10->10 MB, 20 MB goal, ...
var gcRe = regexp.MustCompile(`^gc (\d+) @([\d.]+)s \d+%: ` +
	`([\d.]+)\+([\d.]+)\+([\d.]+) ms clock, ` +
	`[\d.]+\+([\d.]+)/([\d.]+)/([\d.]+)\+[\d.]+ ms cpu, ` +
	`\d+->\d+->(\d+) MB`)

var (
	filledRe = regexp.MustCompile(`=== FILLED (.*) ===`)
	rssRe    = regexp.MustCompile(`Maximum resident set size \(kbytes\): (\d+)`)
)

// GC — одна строка gctrace после маркера
type GC struct {
	Num     int     `json:"num"`
	At      float64 `json:"at"`       // @…s, секунды от старта
	Mark    float64 `json:"mark"`     // mark, wall-clock, ms
	MarkCPU float64 `json:"mark_cpu"` // assist + фон + idle, ms CPU
	Assist  float64 `json:"assist"`
	BG      float64 `json:"bg"`
	Idle    float64 `json:"idle"`
	STW     float64 `json:"stw"`      // большая из двух пауз, ms
	Live    float64 `json:"live"`     // живой хип, MB
	ByTimer bool    `json:"by_timer"` // перед строкой был «GC forced»: сборку запустил таймер
}

// Run — один прогон, один лог
type Run struct {
	File    string  `json:"file"`
	Variant string  `json:"variant"`
	Build   string  `json:"build"`
	Mode    string  `json:"mode"`
	N       int     `json:"n"`
	Objects int     `json:"objects"`
	HeapMB  float64 `json:"heap_mb"`
	Fill    string  `json:"fill"`
	RSSMB   float64 `json:"rss_mb"`
	GCs     []GC    `json:"gcs"`
}

func num(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// parseLog читает лог прогона. ok=false, если маркера FILLED нет (прогон упал)
func parseLog(path string) (Run, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Run{}, false
	}
	defer f.Close()

	var r Run
	filled, timer := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if m := filledRe.FindStringSubmatch(line); m != nil {
			filled = true
			for _, kv := range strings.Fields(m[1]) {
				k, v, _ := strings.Cut(kv, "=")
				switch k {
				case "variant":
					r.Variant = v
				case "mode":
					r.Mode = v
				case "n":
					r.N, _ = strconv.Atoi(v)
				case "objects":
					r.Objects, _ = strconv.Atoi(v)
				case "heap":
					r.HeapMB = num(v) / (1 << 20)
				case "fill":
					r.Fill = v
				}
			}
			continue
		}
		if !filled {
			continue // всё до маркера: заливка и её сборки
		}
		if strings.HasPrefix(line, "GC forced") {
			timer = true // следующую сборку запустил таймер
			continue
		}
		if m := gcRe.FindStringSubmatch(line); m != nil {
			g := GC{At: num(m[2]), Mark: num(m[4]), Assist: num(m[6]), BG: num(m[7]), Idle: num(m[8]), Live: num(m[9]), ByTimer: timer}
			g.Num, _ = strconv.Atoi(m[1])
			g.MarkCPU = g.Assist + g.BG + g.Idle
			g.STW = max(num(m[3]), num(m[5]))
			r.GCs = append(r.GCs, g)
			timer = false
		}
	}
	if !filled {
		return Run{}, false
	}

	name := strings.TrimSuffix(filepath.Base(path), ".log") // a-green-1
	r.File = filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
	r.Build = strings.Split(name, "-")[1]
	if b, err := os.ReadFile(strings.TrimSuffix(path, ".log") + "_time.log"); err == nil {
		if m := rssRe.FindSubmatch(b); m != nil {
			r.RSSMB = num(string(m[1])) / 1024
		}
	}
	return r, true
}

// Stat — медиана и разброс по всем сборкам конфигурации
type Stat struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func summary(xs []float64) Stat {
	if len(xs) == 0 {
		return Stat{}
	}
	s := slices.Clone(xs)
	slices.Sort(s) // stat.Quantile требует отсортированный срез
	return Stat{Median: stat.Quantile(0.5, stat.Empirical, s, nil), Min: floats.Min(s), Max: floats.Max(s)}
}

// Config — сводка по одной конфигурации: режим × вариант × сборка × n
type Config struct {
	Mode       string   `json:"mode"`
	Variant    string   `json:"variant"`
	Build      string   `json:"build"`
	N          int      `json:"n"`
	Runs       int      `json:"runs"`
	GCs        int      `json:"gcs"`          // сборок в зачёте
	AllByTimer bool     `json:"all_by_timer"` // только для timer: все сборки запущены таймером
	MarkClock  Stat     `json:"mark_clock_ms"`
	MarkCPU    Stat     `json:"mark_cpu_ms"`
	STW        Stat     `json:"stw_ms"`
	Live       Stat     `json:"live_mb"`
	Interval   Stat     `json:"interval_s"` // разница соседних @…s
	Objects    int      `json:"objects"`
	HeapMB     float64  `json:"heap_mb"`
	RSSMB      float64  `json:"rss_mb"`
	Fill       []string `json:"fill"`
	Raw        []Run    `json:"raw"`
}

// summarize группирует прогоны и считает статистику по правилам README:
// первая сборка после маркера — прогрев, в зачёт не идёт
func summarize(runs []Run) []Config {
	type key struct {
		mode, variant, build string
		n                    int
	}
	groups := map[key][]Run{}
	var keys []key
	for _, r := range runs {
		k := key{r.Mode, r.Variant, r.Build, r.N}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], r)
	}

	var out []Config
	for _, k := range keys {
		rs := groups[k]
		c := Config{Mode: k.mode, Variant: k.variant, Build: k.build, N: k.n, Runs: len(rs), AllByTimer: k.mode == "timer", Raw: rs}
		var mark, cpu, stw, live, iv, objs, heap []float64
		for _, r := range rs {
			for i, g := range r.GCs {
				if i > 0 {
					iv = append(iv, g.At-r.GCs[i-1].At)
				}
				if i == 0 {
					continue // прогрев
				}
				mark, cpu, stw, live = append(mark, g.Mark), append(cpu, g.MarkCPU), append(stw, g.STW), append(live, g.Live)
				c.AllByTimer = c.AllByTimer && g.ByTimer
			}
			objs, heap = append(objs, float64(r.Objects)), append(heap, r.HeapMB)
			c.RSSMB = max(c.RSSMB, r.RSSMB)
			c.Fill = append(c.Fill, r.Fill)
		}
		c.GCs = len(mark)
		c.MarkClock, c.MarkCPU, c.STW, c.Live, c.Interval = summary(mark), summary(cpu), summary(stw), summary(live), summary(iv)
		c.Objects, c.HeapMB = int(summary(objs).Median), summary(heap).Median
		out = append(out, c)
	}

	order := map[string]int{"a": 0, "p": 1, "b2": 2, "b": 3}
	slices.SortFunc(out, func(x, y Config) int {
		for _, d := range []int{strings.Compare(y.Mode, x.Mode), strings.Compare(x.Build, y.Build), order[x.Variant] - order[y.Variant], x.N - y.N} {
			if d != 0 {
				return d
			}
		}
		return 0
	})
	return out
}
