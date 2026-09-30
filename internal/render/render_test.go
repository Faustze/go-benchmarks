package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "перезаписать golden-файлы testdata/*.html")

// Каждый testdata/<имя>.md рендерится и сравнивается с testdata/<имя>.html.
func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("нет testdata/*.md")
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".md")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Markdown(src)
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(in, ".md") + ".html"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (запусти go test -update)", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s не совпал с golden\n--- got\n%s\n--- want\n%s", in, got, want)
			}
		})
	}
}
