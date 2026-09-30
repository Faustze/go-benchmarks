package site

import (
	"errors"
	"fmt"
	"html/template"
	"math"
	"slices"
	"strconv"

	"github.com/Faustze/go-benchmarks/internal/data"
	"github.com/Faustze/go-benchmarks/internal/i18n"
)

// pageCtx: то, что функциям шаблона нужно знать о странице.
type pageCtx struct {
	lang    string
	strings *i18n.Strings
	fmt     i18n.Format
	data    *data.Data // nil, если у замера нет data.json
}

var errNoData = errors.New("у статьи нет data.json")

// funcs: функции шаблонов и шорткодов. Для разбора шаблонов вызывается с пустым pageCtx,
// перед выполнением на каждой странице клон шаблона получает функции с её контекстом.
func funcs(c *pageCtx) template.FuncMap {
	return template.FuncMap{
		"T":       c.T,
		"TV":      c.T, // строка на языке интерфейса страницы; на странице-замене перевода подменяется
		"num":     c.num,
		"ratio":   c.ratio,
		"fmt":     c.format,
		"cfg":     c.cfg,
		"configs": c.configs,
		"nsRange": c.nsRange,
		"maxSTW":  c.maxSTW,
		"single":  c.single,
		"list":    func(s ...string) []string { return s },
		"errorf":  func(format string, a ...any) (string, error) { return "", fmt.Errorf(format, a...) },
	}
}

func (c *pageCtx) T(key string) (string, error) { return c.strings.T(c.lang, key) }

// format: число по спецификации. ms: длительность; n0…n3: знаков после запятой; count: «25 млн»;
// x0, x1: кратность «×812»; r10: округление до десятков.
func (c *pageCtx) format(v float64, spec string) (string, error) {
	switch spec {
	case "ms":
		return c.fmt.Ms(v), nil
	case "count":
		return c.fmt.Count(v), nil
	case "r10":
		return c.fmt.Num(i18n.Round(v, 10), 0), nil
	}
	if len(spec) == 2 && (spec[0] == 'n' || spec[0] == 'x') {
		d, err := strconv.Atoi(spec[1:])
		if err == nil && d >= 0 && d <= 3 {
			s := c.fmt.Num(v, d)
			if spec[0] == 'x' {
				s = "×" + s
			}
			return s, nil
		}
	}
	return "", fmt.Errorf("неизвестный формат числа %q", spec)
}

func (c *pageCtx) value(path string) (float64, error) {
	if c.data == nil {
		return 0, errNoData
	}
	return c.data.Value(path)
}

// num: значение из data.json по адресу, отформатированное по spec.
func (c *pageCtx) num(path, spec string) (string, error) {
	v, err := c.value(path)
	if err != nil {
		return "", err
	}
	return c.format(v, spec)
}

// ratio: во сколько раз значение a больше b.
func (c *pageCtx) ratio(a, b, spec string) (string, error) {
	va, err := c.value(a)
	if err != nil {
		return "", err
	}
	vb, err := c.value(b)
	if err != nil {
		return "", err
	}
	if vb == 0 {
		return "", fmt.Errorf("ratio: %s равно нулю", b)
	}
	return c.format(va/vb, spec)
}

func (c *pageCtx) cfg(path string) (*data.Config, error) {
	if c.data == nil {
		return nil, errNoData
	}
	cfg, _, err := c.data.Config(path)
	return cfg, err
}

// configs: конфигурации режима, в которых есть сборки в зачёте.
func (c *pageCtx) configs(mode string) ([]*data.Config, error) {
	if c.data == nil {
		return nil, errNoData
	}
	return slices.DeleteFunc(c.data.Filter(mode), func(x *data.Config) bool { return x.GCs == 0 }), nil
}

// nsRange: минимум и максимум CPU на живой объект среди конфигураций на 10 млн записей.
func (c *pageCtx) nsRange() ([]float64, error) {
	cs, err := c.configs("")
	if err != nil {
		return nil, err
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, x := range cs {
		if x.N != data.DefaultN || x.Objects == 0 {
			continue
		}
		lo, hi = min(lo, x.NsPerObject()), max(hi, x.NsPerObject())
	}
	return []float64{lo, hi}, nil
}

// maxSTW: самая длинная пауза STW среди конфигураций режима.
func (c *pageCtx) maxSTW(mode string) (float64, error) {
	cs, err := c.configs(mode)
	if err != nil {
		return 0, err
	}
	m := 0.0
	for _, x := range cs {
		m = max(m, x.STW.Max)
	}
	return m, nil
}

// single: сколько сборок шли на одном фоновом воркере и сколько всего.
func (c *pageCtx) single(path string) ([]int, error) {
	cfg, err := c.cfg(path)
	if err != nil {
		return nil, err
	}
	n, total := cfg.SingleWorker()
	return []int{n, total}, nil
}
