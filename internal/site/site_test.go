package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, path, s string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Минимальный репозиторий: одна статья на двух языках, главная только на русском.
func fixture(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "web/layouts/base.html", `{{define "base"}}<title>{{.Meta.Title}}</title>{{range .Langs}}[{{.Lang}}:{{.URL}}]{{end}}{{if .Home}}{{template "home" .}}{{else}}{{template "article" .}}{{end}}{{if .Chart}}<script id="gb-data">{{.Chart}}</script>{{end}}{{end}}`)
	write(t, root, "web/layouts/article.html", `{{define "article"}}<main>{{.Content}}</main><a href="{{.URL}}">self</a>{{end}}`)
	write(t, root, "web/layouts/home.html", `{{define "home"}}{{range .Articles}}<li>{{.Number}} {{.Meta.Title}} {{.URL}}</li>{{end}}{{end}}`)
	write(t, root, "web/layouts/shortcodes/num.html", `{{num (index .Args 0) (index .Args 1)}}`)
	write(t, root, "web/css/site.css", "@import \"a.css\";\n@import \"parts/b.css\";\nbody{}\n")
	write(t, root, "web/css/a.css", ".a{}\n")
	write(t, root, "web/css/parts/b.css", ".b{}\n")
	write(t, root, "web/js/charts.js", "//")
	write(t, root, "web/fonts/x.woff2", "font")
	write(t, root, "web/static/CNAME", "go.faustze.tech\n")
	write(t, root, "i18n/ru.toml", "[ui]\nof = \"из\"\n")
	write(t, root, "i18n/en.toml", "")
	write(t, root, "articles/_index.ru.md", "+++\ntitle = \"go-benchmarks\"\n+++\n\nЗамеры.\n")
	write(t, root, "articles/001-a/index.ru.md", "+++\ntitle = \"Указатель <b>\"\n+++\n\n## Раздел\n\nмедиана {{< num \"timer.a.green.mark_clock_ms\" ms >}}\n")
	write(t, root, "articles/001-a/index.en.md", "+++\ntitle = \"Pointer\"\n+++\n\ntext\n")
	write(t, root, "articles/001-a/data.json", `{"configs":[{"mode":"timer","variant":"a","build":"green","n":10000000,"mark_clock_ms":{"median":974},"fill":"6s"}]}`)
	write(t, root, "articles/001-a/img/01.png", "png")
	write(t, root, "articles/002-b/index.ru.md", "+++\ntitle = \"b\"\n+++\n\nТолько русский.\n")
	write(t, root, "articles/001-a/main.go", "package main\n")
	write(t, root, "articles/001-a/logs/run.log", "gc 1")
	return root
}

func TestBuild(t *testing.T) {
	root := fixture(t)
	out := filepath.Join(root, "public")
	write(t, root, "public/stale.html", "от прошлой сборки")
	if err := Build(Config{Root: root, Out: out}); err != nil {
		t.Fatal(err)
	}

	ru := read(t, filepath.Join(out, "001-a", "index.html"))
	for _, want := range []string{
		"<title>Указатель &lt;b&gt;</title>", // title экранируется шаблоном
		`<h2 id="раздел">Раздел</h2>`,
		"<p>медиана 974 ms</p>",
		`<a href="/001-a/">`,
		"[ru:/001-a/][en:/en/001-a/]", // переключатель: перевод есть
		`"mark_clock_ms":{"median":974`,
	} {
		if !strings.Contains(ru, want) {
			t.Errorf("нет %q в\n%s", want, ru)
		}
	}
	if strings.Contains(ru, "fill") {
		t.Error("в данные страницы попало поле, которое графикам не нужно")
	}
	if en := read(t, filepath.Join(out, "en", "001-a", "index.html")); !strings.Contains(en, `<a href="/en/001-a/">`) {
		t.Errorf("английская версия: %s", en)
	}
	home := read(t, filepath.Join(out, "index.html"))
	if !strings.Contains(home, "<li>001 Указатель &lt;b&gt; /001-a/</li>") || !strings.Contains(home, "[ru:/][en:/en/]") {
		t.Errorf("главная: %s", home)
	}
	// Статья 002 есть только на русском: на /en/002-b/ её оригинал, картинки берутся у русской версии.
	fb := read(t, filepath.Join(out, "en", "002-b", "index.html"))
	if !strings.Contains(fb, "Только русский") || !strings.Contains(fb, "[ru:/002-b/][en:/en/002-b/]") {
		t.Errorf("страница-замена перевода: %s", fb)
	}
	if css := read(t, filepath.Join(out, "assets", "site.css")); css != ".a{}\n.b{}\nbody{}\n" {
		t.Errorf("site.css = %q", css)
	}

	for path, want := range map[string]bool{
		"CNAME":                true,
		"assets/js/charts.js":  true,
		"assets/fonts/x.woff2": true,
		"001-a/img/01.png":     true,
		"001-a/data.json":      false, // данные вшиты в страницу
		"001-a/main.go":        false,
		"001-a/logs/run.log":   false,
		"en/001-a/img/01.png":  false,
		"en/index.html":        true, // перевода главной нет: на /en/ оригинал с пометкой
		"stale.html":           false,
		"001-a/index.ru.md":    false,
		"assets/css/site.css":  false,
	} {
		if got := exists(filepath.Join(out, path)); got != want {
			t.Errorf("%s: есть = %v, ждали %v", path, got, want)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	root := fixture(t)
	for _, out := range []string{root, filepath.Dir(root), filepath.Join(root, "..", "elsewhere")} {
		if err := Build(Config{Root: root, Out: out}); err == nil {
			t.Errorf("out = %s: ошибки нет", out)
		}
	}
	write(t, root, "articles/001-a/index.ru.md", "+++\ntitle = \"x\"\n+++\n\n{{< nope >}}\n")
	if err := Build(Config{Root: root, Out: filepath.Join(root, "public")}); err == nil || !strings.Contains(err.Error(), "нет шорткода nope") {
		t.Errorf("неизвестный шорткод: %v", err)
	}
	write(t, root, "web/css/a.css", "@import \"site.css\";\n")
	write(t, root, "articles/001-a/index.ru.md", "+++\ntitle = \"x\"\n+++\n")
	if err := Build(Config{Root: root, Out: filepath.Join(root, "public")}); err == nil || !strings.Contains(err.Error(), "циклический") {
		t.Errorf("цикл в @import: %v", err)
	}
}
