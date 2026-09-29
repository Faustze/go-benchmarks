package main

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/font"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

func init() {
	sans := font.Font{Typeface: "Liberation", Variant: "Sans"}
	plot.DefaultFont, plotter.DefaultFont = sans, sans
}

// порядок вариантов: от самого «указательного» к плоскому
var order = []string{"a", "p", "b2", "b"}

var names = map[string]string{
	"a":  "A: LRU, map[string]*entry",
	"p":  "P: map[uint64]*ReadState",
	"b2": "B2: map[string]ReadState",
	"b":  "B: map[uint64]ReadState",
}

func rgb(hex uint32) color.RGBA {
	return color.RGBA{uint8(hex >> 16), uint8(hex >> 8), uint8(hex), 255}
}

// цвет закреплён за вариантом, а не за местом на графике
var (
	colors = map[string]color.RGBA{"a": rgb(0x2a78d6), "p": rgb(0xeb6834), "b2": rgb(0x1baf7a), "b": rgb(0xeda100)}
	ink    = rgb(0x0b0b0b)
	muted  = rgb(0x52514e)
	grid   = rgb(0xe1e0d9)
	paper  = rgb(0xfcfcfb)
)

// pale смешивает цвет с фоном пополам: тот же вариант, вторая серия
func pale(c color.RGBA) color.RGBA {
	mix := func(a, b uint8) uint8 { return uint8((int(a) + int(b)) / 2) }
	return color.RGBA{mix(c.R, paper.R), mix(c.G, paper.G), mix(c.B, paper.B), 255}
}

func find(cs []Config, mode, variant, build string, n int) *Config {
	for i := range cs {
		c := &cs[i]
		if c.Mode == mode && c.Variant == variant && c.Build == build && (n == 0 || c.N == n) {
			return c
		}
	}
	return nil
}

func newPlot(title, x, y string) *plot.Plot {
	p := plot.New()
	p.BackgroundColor = paper
	p.Title.Text = title
	p.Title.TextStyle.Font.Size = vg.Points(13)
	p.Title.TextStyle.Color = ink
	p.Title.Padding = vg.Points(8)
	p.X.Label.Text, p.Y.Label.Text = x, y
	for _, a := range []*plot.Axis{&p.X, &p.Y} {
		a.Color, a.Label.TextStyle.Color, a.Tick.Label.Color, a.Tick.Color = grid, muted, muted, grid
		a.Label.TextStyle.Font.Size = vg.Points(10)
		a.Tick.Label.Font.Size = vg.Points(9)
	}
	g := plotter.NewGrid()
	g.Vertical.Color, g.Horizontal.Color = grid, grid
	g.Vertical.Width, g.Horizontal.Width = vg.Points(0.5), vg.Points(0.5)
	p.Add(g)
	p.Legend.TextStyle.Color = ink
	p.Legend.TextStyle.Font.Size = vg.Points(9)
	return p
}

// logTicks — подписи 0,1 / 1 / 10 / 100 без научной записи
type logTicks struct{ plain bool }

func (lt logTicks) Ticks(min, max float64) []plot.Tick {
	var ts []plot.Tick
	for e := math.Floor(math.Log10(min)); e <= math.Ceil(math.Log10(max)); e++ {
		v := math.Pow(10, e)
		label := human(v)
		if lt.plain {
			label = strconv.FormatFloat(v, 'f', -1, 64)
		}
		ts = append(ts, plot.Tick{Value: v, Label: label})
		for k := 2.0; k < 10; k++ {
			ts = append(ts, plot.Tick{Value: v * k})
		}
	}
	return ts
}

func human(v float64) string {
	switch {
	case v >= 1e6:
		return strconv.FormatFloat(math.Round(v/1e5)/10, 'f', -1, 64) + " млн"
	case v >= 1e3:
		return strconv.FormatFloat(math.Round(v/1e2)/10, 'f', -1, 64) + " тыс."
	default:
		return fmt.Sprintf("%g", v)
	}
}

func ms(v float64) string {
	switch {
	case v >= 1000:
		return fmt.Sprintf("%.1f s", v/1000)
	case v >= 10:
		return fmt.Sprintf("%.0f ms", v)
	default:
		return fmt.Sprintf("%.1f ms", v)
	}
}

// logLog: по X записи или объекты, по Y миллисекунды или мегабайты
func logLog(p *plot.Plot, plainX bool) {
	p.X.Scale, p.Y.Scale = plot.LogScale{}, plot.LogScale{}
	p.X.Tick.Marker, p.Y.Tick.Marker = logTicks{plain: plainX}, logTicks{plain: true}
}

// save рисует один или несколько графиков в ряд в PNG с двойной плотностью
func save(name string, w, h vg.Length, ps ...*plot.Plot) error {
	c := vgimg.NewWith(vgimg.UseWH(w, h), vgimg.UseDPI(192))
	dc := draw.New(c)
	dc.SetColor(paper)
	dc.Fill(dc.Rectangle.Path())
	if len(ps) == 1 {
		ps[0].Draw(dc)
	} else {
		t := draw.Tiles{Rows: 1, Cols: len(ps), PadX: vg.Points(12)}
		cs := plot.Align([][]*plot.Plot{ps}, t, dc)
		for i, p := range ps {
			p.Draw(cs[0][i])
		}
	}
	f, err := os.Create(filepath.Join(imgDir, name))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = vgimg.PngCanvas{Canvas: c}.WriteTo(f)
	return err
}

func line(xys plotter.XYs, c color.RGBA) (*plotter.Line, *plotter.Scatter) {
	l, _ := plotter.NewLine(xys)
	l.Color, l.Width = c, vg.Points(2)
	s, _ := plotter.NewScatter(xys)
	s.GlyphStyle = draw.GlyphStyle{Color: c, Radius: vg.Points(3.5), Shape: draw.CircleGlyph{}}
	return l, s
}

func drawAll(cs []Config) error {
	for _, f := range []func([]Config) error{drawHero, drawScaling, drawPredictor, drawGreenTea, drawTimeline} {
		if err := f(cs); err != nil {
			return err
		}
	}
	return nil
}

// 01: одна сборка на 10 млн записей, CPU разметки по вариантам
func drawHero(cs []Config) error {
	const n = 10_000_000
	p := newPlot("Сколько CPU стоит одна разметка кэша на 10 млн записей", "mark CPU, ms (assist + фон + idle)", "")
	var labels []string
	for i := len(order) - 1; i >= 0; i-- { // A сверху
		v := order[i]
		c := find(cs, "forced", v, "green", n)
		if c == nil {
			return nil
		}
		pos := float64(len(labels))
		labels = append(labels, names[v])
		b, _ := plotter.NewBarChart(plotter.Values{c.MarkCPU.Median}, vg.Points(22))
		b.Horizontal, b.XMin = true, pos
		b.Color, b.LineStyle.Width = colors[v], 0
		p.Add(b)
		lb, _ := plotter.NewLabels(plotter.XYLabels{
			XYs:    plotter.XYs{{X: c.MarkCPU.Median, Y: pos}},
			Labels: []string{fmt.Sprintf("  %s · %s объектов", ms(c.MarkCPU.Median), human(float64(c.Objects)))},
		})
		lb.TextStyle[0].Color = ink
		lb.TextStyle[0].YAlign = draw.YCenter
		lb.TextStyle[0].Font.Size = vg.Points(9)
		p.Add(lb)
	}
	p.NominalY(labels...)
	p.X.Min, p.X.Max = 0, p.X.Max*1.45 // место под подписи справа
	return save("01-mark-cpu-10m.png", vg.Points(720), vg.Points(260), p)
}

// 02: как растёт цена разметки с числом записей
func drawScaling(cs []Config) error {
	p := newPlot("Цена разметки растёт с числом записей, если в них есть указатели", "записей в кэше", "mark CPU, ms")
	logLog(p, false)
	for _, v := range order {
		var xys plotter.XYs
		for _, c := range cs {
			if c.Mode == "forced" && c.Variant == v && c.Build == "green" {
				xys = append(xys, plotter.XY{X: float64(c.N), Y: c.MarkCPU.Median})
			}
		}
		if len(xys) == 0 {
			continue
		}
		l, s := line(xys, colors[v])
		p.Add(l, s)
		p.Legend.Add(names[v], l, s)
	}
	p.Legend.Top, p.Legend.Left = true, true
	p.Legend.XOffs, p.Legend.YOffs = vg.Points(8), -vg.Points(4)
	return save("02-scaling.png", vg.Points(720), vg.Points(420), p)
}

// 03: что предсказывает цену — мегабайты или объекты
func drawPredictor(cs []Config) error {
	byMB := newPlot("по объёму живого хипа: разброс в 1000 раз", "живой хип, MB", "mark CPU, ms")
	byObj := newPlot("по числу объектов в куче: одна линия", "объектов в куче", "")
	logLog(byMB, true)
	logLog(byObj, false)
	for _, v := range order {
		var mb, obj plotter.XYs
		for _, c := range cs {
			if c.Mode == "forced" && c.Variant == v && c.Build == "green" && c.Objects > 0 {
				mb = append(mb, plotter.XY{X: max(c.Live.Median, 1), Y: c.MarkCPU.Median})
				obj = append(obj, plotter.XY{X: float64(c.Objects), Y: c.MarkCPU.Median})
			}
		}
		if len(mb) == 0 {
			continue
		}
		_, s1 := line(mb, colors[v])
		_, s2 := line(obj, colors[v])
		byMB.Add(s1)
		byObj.Add(s2)
		byMB.Legend.Add(names[v], s1)
	}
	byMB.Legend.Top, byMB.Legend.Left = true, true
	byMB.Legend.XOffs = vg.Points(6)
	for _, p := range []*plot.Plot{byMB, byObj} {
		p.Title.TextStyle.Font.Size = vg.Points(11)
	}
	return save("03-objects-not-bytes.png", vg.Points(900), vg.Points(400), byMB, byObj)
}

// 04: Green Tea включён и выключен, 10 млн записей: медиана и разброс по 7 сборкам
func drawGreenTea(cs []Config) error {
	const n = 10_000_000
	p := newPlot("Green Tea GC, 10 млн записей: точка — медиана, линия — min…max", "", "mark CPU, ms")
	var labels []string
	for i, v := range order {
		g, ng := find(cs, "forced", v, "green", n), find(cs, "forced", v, "nogreen", n)
		if g == nil || ng == nil {
			return nil
		}
		labels = append(labels, names[v])
		for j, c := range []*Config{g, ng} {
			x := float64(i) - 0.14 + 0.28*float64(j)
			l, _ := line(plotter.XYs{{X: x, Y: c.MarkCPU.Min}, {X: x, Y: c.MarkCPU.Max}}, colors[v])
			l.Width = vg.Points(3)
			dot, _ := plotter.NewScatter(plotter.XYs{{X: x, Y: c.MarkCPU.Median}})
			dot.GlyphStyle = draw.GlyphStyle{Color: colors[v], Radius: vg.Points(6), Shape: draw.CircleGlyph{}}
			if j == 1 {
				l.Color = pale(colors[v])
				dot.GlyphStyle.Color = pale(colors[v])
			}
			lb, _ := plotter.NewLabels(plotter.XYLabels{XYs: plotter.XYs{{X: x, Y: c.MarkCPU.Median}},
				Labels: []string{[]string{"green ", "nogreen "}[j] + ms(c.MarkCPU.Median)}})
			lb.TextStyle[0].Color, lb.TextStyle[0].Font.Size, lb.TextStyle[0].YAlign = ink, vg.Points(8), draw.YCenter
			lb.TextStyle[0].XAlign = []draw.XAlignment{draw.XRight, draw.XLeft}[j]
			lb.Offset = vg.Point{X: vg.Points(float64(j*18 - 9))}
			p.Add(l, dot, lb)
		}
	}
	p.NominalX(labels...)
	p.X.Min, p.X.Max = -0.6, float64(len(order))-0.4
	p.X.Tick.Label.Font.Size = vg.Points(8)
	p.Y.Scale, p.Y.Tick.Marker = plot.LogScale{}, logTicks{plain: true}
	p.Y.Min, p.Y.Max = 0.5, 10000
	return save("04-green-tea.png", vg.Points(760), vg.Points(400), p)
}

// 05: сборки по таймеру в простое, первый прогон каждого варианта
func drawTimeline(cs []Config) error {
	p := newPlot("Сборки по таймеру в простое: 10 млн записей, 11 минут", "секунды с запуска", "mark CPU, ms")
	p.Y.Scale, p.Y.Tick.Marker = plot.LogScale{}, logTicks{plain: true}
	found := false
	for _, v := range order {
		c := find(cs, "timer", v, "green", 0)
		if c == nil || len(c.Raw) == 0 {
			continue
		}
		var xys plotter.XYs
		for _, g := range c.Raw[0].GCs {
			xys = append(xys, plotter.XY{X: g.At, Y: max(g.MarkCPU, 0.01)})
		}
		_, s := line(xys, colors[v])
		s.GlyphStyle.Radius = vg.Points(5)
		p.Add(s)
		p.Legend.Add(names[v], s)
		found = true
	}
	if !found {
		return nil
	}
	p.Legend.Top = true
	return save("05-timer-timeline.png", vg.Points(720), vg.Points(360), p)
}
