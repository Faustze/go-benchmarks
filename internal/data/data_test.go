package data

import (
	"math"
	"testing"
)

// Проверяем на настоящем data.json замера 001: числа сверены со страницей и README.
func TestValue001(t *testing.T) {
	d, err := Load("../../articles/001-gc-pointers/data.json")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]float64{
		"timer.a.green.mark_clock_ms.median": 974,
		"timer.a.green.mark_clock_ms":        974,
		"timer.b.green.mark_clock_ms.median": 1.2,
		"timer.a.green.mark_clock_ms.max":    1297,
		"timer.a.nogreen.mark_clock_ms":      2668,
		"timer.a.green.objects":              25015425,
		"forced.b2.green.ns_per_object":      202.2,
		"forced.a.green.1000000.gcs":         7,
	} {
		got, err := d.Value(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if math.Abs(got-want) > 0.05 {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	for _, bad := range []string{"timer.a", "timer.x.green.objects", "timer.a.green", "timer.a.green.foo", "timer.a.green.objects.median", "timer.a.green.stw_ms.p90"} {
		if _, err := d.Value(bad); err == nil {
			t.Errorf("%s: ошибки нет", bad)
		}
	}
	c, _, err := d.Config("timer.a.nogreen")
	if err != nil {
		t.Fatal(err)
	}
	if n, total := c.SingleWorker(); n != 14 || total != 15 {
		t.Errorf("SingleWorker = %d из %d, в README 14 из 15", n, total)
	}
}
