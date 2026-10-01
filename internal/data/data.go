// Package data читает data.json замера и отдаёт значения по адресу вида
// «timer.a.green.mark_clock_ms.median»: режим, вариант, сборка, [число записей], поле, [статистика].
package data

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultN: размер кэша, если в адресе его нет. Все сборки по таймеру шли на 10 млн записей.
const DefaultN = 10_000_000

// DefaultBuild: сборка Go по умолчанию. Сайт показывает только её, остальные сборки
// (например, nogreen) остаются в data.json и логах для воспроизведения.
const DefaultBuild = "green"

// Stat: медиана, минимум и максимум по всем сборкам конфигурации.
type Stat struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// GC: одна сборка из gctrace.
type GC struct {
	Num     int     `json:"num"`
	At      float64 `json:"at"`
	Mark    float64 `json:"mark"`
	MarkCPU float64 `json:"mark_cpu"`
	BG      float64 `json:"bg"`   // CPU фоновых воркеров, ms
	Idle    float64 `json:"idle"` // CPU idle-воркеров, ms
	STW     float64 `json:"stw"`
}

// Run: один прогон.
type Run struct {
	GCs []GC `json:"gcs"`
}

// Config: сводка по одной конфигурации (режим, вариант, сборка, размер).
type Config struct {
	Mode      string  `json:"mode"`
	Variant   string  `json:"variant"`
	Build     string  `json:"build"`
	N         int     `json:"n"`
	Runs      int     `json:"runs"`
	GCs       int     `json:"gcs"`
	MarkClock Stat    `json:"mark_clock_ms"`
	MarkCPU   Stat    `json:"mark_cpu_ms"`
	STW       Stat    `json:"stw_ms"`
	Live      Stat    `json:"live_mb"`
	Interval  Stat    `json:"interval_s"`
	Objects   float64 `json:"objects"`
	HeapMB    float64 `json:"heap_mb"`
	RSSMB     float64 `json:"rss_mb"`
	Raw       []Run   `json:"raw"`
}

// Records: число записей как float64, для форматирования в шаблонах.
func (c *Config) Records() float64 { return float64(c.N) }

// NsPerObject: CPU разметки на один живой объект, ns.
func (c *Config) NsPerObject() float64 { return c.MarkCPU.Median * 1e6 / c.Objects }

// Data: весь data.json.
type Data struct {
	Configs []*Config `json:"configs"`
}

// Load читает data.json.
func Load(path string) (*Data, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &d, nil
}

// Find: конфигурация по режиму, варианту, сборке и размеру.
func (d *Data) Find(mode, variant, build string, n int) (*Config, error) {
	for _, c := range d.Configs {
		if c.Mode == mode && c.Variant == variant && c.Build == build && c.N == n {
			return c, nil
		}
	}
	return nil, fmt.Errorf("нет конфигурации %s.%s.%s.%d", mode, variant, build, n)
}

// Filter: конфигурации режима в порядке data.json. Пустой mode: все.
func (d *Data) Filter(mode string) []*Config {
	var out []*Config
	for _, c := range d.Configs {
		if mode == "" || c.Mode == mode {
			out = append(out, c)
		}
	}
	return out
}

// Config по первой части адреса: «timer.a.green» или «forced.a.green.1000000». Возвращает остаток адреса.
func (d *Data) Config(path string) (*Config, []string, error) {
	parts := strings.Split(path, ".")
	if len(parts) < 3 {
		return nil, nil, fmt.Errorf("адрес %q: нужны режим, вариант и сборка", path)
	}
	n, rest := DefaultN, parts[3:]
	if len(rest) > 0 {
		if v, err := strconv.Atoi(rest[0]); err == nil {
			n, rest = v, rest[1:]
		}
	}
	c, err := d.Find(parts[0], parts[1], parts[2], n)
	return c, rest, err
}

// Value: число по полному адресу.
func (d *Data) Value(path string) (float64, error) {
	c, rest, err := d.Config(path)
	if err != nil {
		return 0, err
	}
	if len(rest) == 0 {
		return 0, fmt.Errorf("адрес %q: нет поля", path)
	}
	field, stat := rest[0], ""
	if len(rest) > 1 {
		stat = rest[1]
	}
	if len(rest) > 2 {
		return 0, fmt.Errorf("адрес %q: лишние части", path)
	}
	scalar := map[string]float64{
		"objects": c.Objects, "heap_mb": c.HeapMB, "rss_mb": c.RSSMB,
		"runs": float64(c.Runs), "gcs": float64(c.GCs), "n": float64(c.N),
		"ns_per_object": c.NsPerObject(),
	}
	if v, ok := scalar[field]; ok {
		if stat != "" {
			return 0, fmt.Errorf("адрес %q: у %s нет статистики", path, field)
		}
		return v, nil
	}
	stats := map[string]Stat{
		"mark_clock_ms": c.MarkClock, "mark_cpu_ms": c.MarkCPU, "stw_ms": c.STW,
		"live_mb": c.Live, "interval_s": c.Interval,
	}
	s, ok := stats[field]
	if !ok {
		return 0, fmt.Errorf("адрес %q: неизвестное поле %s", path, field)
	}
	switch stat {
	case "median", "":
		return s.Median, nil
	case "min":
		return s.Min, nil
	case "max":
		return s.Max, nil
	}
	return 0, fmt.Errorf("адрес %q: неизвестная статистика %s", path, stat)
}

// SingleWorker: сколько сборок конфигурации шли фактически на одном фоновом воркере
// (CPU разметки меньше 1,25 часов) и сколько сборок всего, включая прогревочные.
func (c *Config) SingleWorker() (n, total int) {
	for _, r := range c.Raw {
		for _, g := range r.GCs {
			total++
			if g.MarkCPU < 1.25*g.Mark {
				n++
			}
		}
	}
	return n, total
}

// Chart: облегчённый data.json для островов графиков, вшивается в страницу вместо отдельного запроса.
// Только сборка по умолчанию. Сборки по отдельности нужны только графику таймера, у остальных конфигураций хватает сводки.
func (d *Data) Chart() map[string]any {
	type gc struct {
		Num     int     `json:"num"`
		At      float64 `json:"at"`
		Mark    float64 `json:"mark"`
		MarkCPU float64 `json:"mark_cpu"`
		BG      float64 `json:"bg"`
		Idle    float64 `json:"idle"`
	}
	type run struct {
		GCs []gc `json:"gcs"`
	}
	type cfg struct {
		Mode      string  `json:"mode"`
		Variant   string  `json:"variant"`
		Build     string  `json:"build"`
		N         int     `json:"n"`
		Runs      int     `json:"runs"`
		GCs       int     `json:"gcs"`
		MarkClock Stat    `json:"mark_clock_ms"`
		MarkCPU   Stat    `json:"mark_cpu_ms"`
		Live      Stat    `json:"live_mb"`
		Objects   float64 `json:"objects"`
		Raw       []run   `json:"raw,omitempty"`
	}
	out := make([]cfg, 0, len(d.Configs))
	for _, c := range d.Configs {
		if c.Build != DefaultBuild {
			continue
		}
		x := cfg{c.Mode, c.Variant, c.Build, c.N, c.Runs, c.GCs, c.MarkClock, c.MarkCPU, c.Live, c.Objects, nil}
		if c.Mode == "timer" {
			for _, r := range c.Raw {
				var rr run
				for _, g := range r.GCs {
					rr.GCs = append(rr.GCs, gc{g.Num, g.At, g.Mark, g.MarkCPU, g.BG, g.Idle})
				}
				x.Raw = append(x.Raw, rr)
			}
		}
		out = append(out, x)
	}
	return map[string]any{"configs": out}
}
