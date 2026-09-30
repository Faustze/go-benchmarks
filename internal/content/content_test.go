package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	src := "+++\ntitle = \"Сколько стоит указатель\"\ndate = 2026-09-30\ngo = \"1.27.1\"\n+++\n\nтекст\n"
	meta, body, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Сколько стоит указатель" || meta.Go != "1.27.1" {
		t.Errorf("meta = %+v", meta)
	}
	if want := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local); !meta.Date.Equal(want) {
		t.Errorf("date = %v, want %v", meta.Date, want)
	}
	if string(body) != "\nтекст\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParseErrors(t *testing.T) {
	for name, src := range map[string]string{
		"без frontmatter":  "текст\n",
		"не закрыт":        "+++\ntitle = \"x\"\n",
		"неизвестное поле": "+++\ntitle = \"x\"\ntitel = \"y\"\n+++\n",
		"пустой title":     "+++\ngo = \"1.27.1\"\n+++\n",
		"битый TOML":       "+++\ntitle = \n+++\n",
	} {
		if _, _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: ошибки нет", name)
		}
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	write := func(path, s string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("002-b/index.ru.md", "+++\ntitle = \"b\"\n+++\n")
	write("001-a/index.en.md", "+++\ntitle = \"a en\"\n+++\n")
	write("001-a/index.ru.md", "+++\ntitle = \"a ru\"\n+++\n")
	write("001-a/main.go", "package main\n")
	write("no-bundle/README.md", "# нет index.*.md\n")
	write(".git/index.ru.md", "+++\ntitle = \"skip\"\n+++\n")

	pages, err := Load(root, []string{"ru", "en"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pages {
		got = append(got, p.Slug+"/"+p.Lang)
	}
	if want := "001-a/ru 001-a/en 002-b/ru"; strings.Join(got, " ") != want {
		t.Errorf("pages = %v, want %s", got, want)
	}
	if n := pages[0].Number(); n != "001" {
		t.Errorf("Number = %q", n)
	}
}

func TestLoadSection(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "_index.ru.md"), []byte("+++\ntitle = \"go-benchmarks\"\n+++\n\nЗамеры.\n"), 0o644)
	pages, err := LoadSection(root, []string{"ru", "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Lang != "ru" || pages[0].Meta.Title != "go-benchmarks" {
		t.Errorf("pages = %+v", pages)
	}
}
