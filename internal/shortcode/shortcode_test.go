package shortcode

import (
	"reflect"
	"testing"
)

func TestExtractRestore(t *testing.T) {
	src := "Эффект: {{< num \"timer.a.green.mark_clock_ms\" ms >}} на сборку.\n\n{{< chart hero >}}\n"
	out, calls, err := Extract([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []Call{{"num", []string{"timer.a.green.mark_clock_ms", "ms"}}, {"chart", []string{"hero"}}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v", calls)
	}
	if string(out) != "Эффект: GBSHORTCODE0000END на сборку.\n\nGBSHORTCODE0001END\n" {
		t.Fatalf("out = %q", out)
	}
	html := "<p>Эффект: GBSHORTCODE0000END на сборку.</p>\n<p>GBSHORTCODE0001END</p>\n"
	got, err := Restore([]byte(html), []string{"974 ms", `<div class="chart"></div>`})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<p>Эффект: 974 ms на сборку.</p>\n<div class=\"chart\"></div>\n" {
		t.Errorf("got %q", got)
	}
}

func TestExtractErrors(t *testing.T) {
	if _, _, err := Extract([]byte("текст\n{{< num \"x\"\n")); err == nil {
		t.Error("незакрытый вызов прошёл")
	}
	if _, err := Restore([]byte("<p>метки нет</p>"), []string{"x"}); err == nil {
		t.Error("пропавшая метка прошла")
	}
}
