// Package site собирает статический сайт. Серверная часть (этот пакет) читает статьи из articles/,
// строки из i18n/ и клиентскую часть из web/: шаблоны, стили, скрипты, шрифты и статику.
//
//	articles/<NNN-тема>/index.<lang>.md  статья, рядом data.json и img/
//	articles/_index.<lang>.md             главная
//	web/layouts/                          шаблоны страниц и шорткодов
//	web/css/site.css                      точка входа стилей, @import склеиваются в один файл
//	web/js/, web/fonts/                   копируются в /assets/
//	web/static/                           копируется в корень сайта как есть
package site

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/Faustze/go-benchmarks/internal/content"
	"github.com/Faustze/go-benchmarks/internal/data"
	"github.com/Faustze/go-benchmarks/internal/i18n"
	"github.com/Faustze/go-benchmarks/internal/render"
	"github.com/Faustze/go-benchmarks/internal/shortcode"
	"github.com/Faustze/go-benchmarks/internal/translate"
)

// DefaultLang живёт в корне сайта, остальные языки в /<lang>/.
const DefaultLang = "ru"

// Langs: порядок языков при обходе бандлов и в переключателе.
var Langs = []string{"ru", "en"}

// Из бандла замера публикуется белый список: код, логи и run.sh открыты на GitHub, на сайте они не нужны.
var bundleFiles = []string{"img"}

// Config: откуда собирать и куда класть результат.
type Config struct {
	Root string               // корень репозитория
	Out  string               // папка результата, её содержимое удаляется перед сборкой
	Warn func(string, ...any) // предупреждения, которые не ломают сборку; nil: молчать
}

func (c Config) warn(format string, a ...any) {
	if c.Warn != nil {
		c.Warn(format, a...)
	}
}

func (c Config) articles() string { return filepath.Join(c.Root, "articles") }
func (c Config) web() string      { return filepath.Join(c.Root, "web") }

// LangLink: пункт переключателя языка.
type LangLink struct {
	Lang    string
	URL     string
	Current bool
	Real    bool // есть настоящий перевод; без него по ссылке оригинал с пометкой, hreflang не ставится
}

// Page: данные шаблона любой страницы.
type Page struct {
	content.Page
	Home     bool
	View     string // язык интерфейса и адреса; отличается от Lang, если перевода нет и показан оригинал
	Fallback bool   // перевода на View нет, текст на Lang
	Content  template.HTML
	URL      string     // путь от корня сайта: /001-gc-pointers/ или /en/001-gc-pointers/
	HomeURL  string     // главная на языке страницы
	Bundle   string     // путь к картинкам бандла: всегда у версии на языке по умолчанию
	Langs    []LangLink // переключатель языка и hreflang
	Articles []*Page    // только у главной: статьи её языка
	Chart    any        // облегчённый data.json для островов, nil если данных нет
	Labels   map[string]string
}

// Shortcode: данные шаблона шорткода.
type Shortcode struct {
	Args []string
	Page *Page
}

// Build собирает сайт целиком.
func Build(cfg Config) error {
	if err := checkOut(cfg); err != nil {
		return err
	}
	layouts, err := parseLayouts(filepath.Join(cfg.web(), "layouts"))
	if err != nil {
		return err
	}
	strs, err := i18n.Load(filepath.Join(cfg.Root, "i18n"), Langs, DefaultLang)
	if err != nil {
		return err
	}
	articles, err := content.Load(cfg.articles(), Langs)
	if err != nil {
		return err
	}
	homes, err := content.LoadSection(cfg.articles(), Langs)
	if err != nil {
		return err
	}
	checkTranslations(cfg, append(homes, articles...))
	if err := os.RemoveAll(cfg.Out); err != nil {
		return err
	}
	if err := writeAssets(cfg); err != nil {
		return err
	}

	// Какие языки есть у каждой страницы: из этого строится переключатель.
	have := map[string]map[string]bool{}
	for _, p := range append(homes, articles...) {
		if have[p.Slug] == nil {
			have[p.Slug] = map[string]bool{}
		}
		have[p.Slug][p.Lang] = true
	}
	byLang := map[string][]*Page{}
	for _, v := range views(articles) {
		pg, err := buildPage(cfg, layouts, strs, v.page, v.lang, have[v.page.Slug], nil)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", v.page.Slug, v.lang, err)
		}
		byLang[v.lang] = append(byLang[v.lang], pg)
	}
	for _, v := range views(homes) {
		if _, err := buildPage(cfg, layouts, strs, v.page, v.lang, have[""], byLang[v.lang]); err != nil {
			return fmt.Errorf("главная/%s: %w", v.lang, err)
		}
	}
	return nil
}

// checkTranslations предупреждает о переводах, которые отстали от исходника или ещё не вычитаны.
func checkTranslations(cfg Config, pages []content.Page) {
	for _, p := range pages {
		if p.Lang == DefaultLang {
			continue
		}
		name := p.Slug
		if name == "" {
			name = "главная"
		}
		src, err := os.ReadFile(SourcePath(p.Dir, p.Slug, DefaultLang))
		if err != nil {
			cfg.warn("перевод %s/%s: нет исходника на %s", name, p.Lang, DefaultLang)
			continue
		}
		switch {
		case translate.Stale(src, p.Meta):
			cfg.warn("перевод %s/%s устарел: исходник поменялся после перевода, go run ./cmd/site translate %s", name, p.Lang, p.Slug)
		case p.Meta.Translation != "manual":
			cfg.warn("перевод %s/%s не вычитан: после вычитки поставь translation = \"manual\"", name, p.Lang)
		}
	}
}

// SourcePath: файл страницы на языке lang. Пустой slug: главная.
func SourcePath(dir, slug, lang string) string {
	if slug == "" {
		return filepath.Join(dir, "_index."+lang+".md")
	}
	return filepath.Join(dir, "index."+lang+".md")
}

type view struct {
	page content.Page
	lang string
}

// views: каждая страница на каждом языке. Нет перевода: на этом языке показывается оригинал
// на языке по умолчанию, чтобы ссылка из переключателя не вела в 404.
func views(pages []content.Page) []view {
	have := map[string]content.Page{}
	var out []view
	for _, p := range pages {
		have[p.Slug+"/"+p.Lang] = p
		out = append(out, view{p, p.Lang})
	}
	for _, p := range pages {
		if p.Lang != DefaultLang {
			continue
		}
		for _, l := range Langs {
			if _, ok := have[p.Slug+"/"+l]; !ok {
				out = append(out, view{p, l})
			}
		}
	}
	return out
}

// parseLayouts: base.html, article.html, home.html и шорткоды из shortcodes/*.html (имя шаблона: имя файла).
func parseLayouts(dir string) (*template.Template, error) {
	files := []string{"base.html", "article.html", "home.html"}
	for i, f := range files {
		files[i] = filepath.Join(dir, f)
	}
	sc, err := filepath.Glob(filepath.Join(dir, "shortcodes", "*.html"))
	if err != nil {
		return nil, err
	}
	return template.New("base").Funcs(funcs(&pageCtx{})).ParseFiles(append(files, sc...)...)
}

func buildPage(cfg Config, layouts *template.Template, strs *i18n.Strings, p content.Page, viewLang string, langs map[string]bool, list []*Page) (*Page, error) {
	pc := &pageCtx{lang: p.Lang, strings: strs, fmt: i18n.NewFormat(p.Lang)}
	if p.Slug != "" {
		dataPath := filepath.Join(p.Dir, "data.json")
		if _, err := os.Stat(dataPath); err == nil {
			if pc.data, err = data.Load(dataPath); err != nil {
				return nil, err
			}
		}
	}
	tmpl, err := layouts.Clone()
	if err != nil {
		return nil, err
	}
	tmpl.Funcs(funcs(pc))
	tmpl.Funcs(template.FuncMap{"TV": func(key string) (string, error) { return strs.T(viewLang, key) }})

	pg := &Page{
		Page:     p,
		Home:     p.Slug == "",
		View:     viewLang,
		Fallback: viewLang != p.Lang,
		URL:      URL(viewLang, p.Slug),
		HomeURL:  URL(viewLang, ""),
		Bundle:   URL(DefaultLang, p.Slug),
		Articles: list,
	}
	for _, l := range Langs {
		link := LangLink{Lang: l, URL: URL(l, p.Slug), Current: l == viewLang, Real: langs[l]}
		pg.Langs = append(pg.Langs, link)
	}
	if pc.data != nil {
		pg.Chart = pc.data.Chart()
		pg.Labels = strs.Prefix(p.Lang, "")
	}
	body, err := renderBody(tmpl, pg)
	if err != nil {
		return nil, err
	}
	pg.Content = template.HTML(body)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", pg); err != nil {
		return nil, err
	}
	dir := filepath.Join(cfg.Out, filepath.FromSlash(pg.URL))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	// Картинки лежат один раз, у версии на языке по умолчанию. Перевод ссылается на них оттуда.
	if viewLang == DefaultLang && p.Slug != "" {
		for _, name := range bundleFiles {
			if err := copyPath(filepath.Join(p.Dir, name), filepath.Join(dir, name)); err != nil {
				return nil, err
			}
		}
	}
	return pg, nil
}

// renderBody: шорткоды заменяются метками, Markdown рендерится, метки заменяются выводом шаблонов шорткодов.
func renderBody(tmpl *template.Template, pg *Page) ([]byte, error) {
	src, calls, err := shortcode.Extract(pg.Body)
	if err != nil {
		return nil, err
	}
	html, err := render.Markdown(src)
	if err != nil {
		return nil, err
	}
	results := make([]string, len(calls))
	for i, c := range calls {
		name := c.Name + ".html"
		if tmpl.Lookup(name) == nil {
			return nil, fmt.Errorf("нет шорткода %s (web/layouts/shortcodes/%s)", c.Name, name)
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, Shortcode{Args: c.Args, Page: pg}); err != nil {
			return nil, fmt.Errorf("шорткод %s %s: %w", c.Name, strings.Join(c.Args, " "), err)
		}
		results[i] = strings.TrimSpace(buf.String())
	}
	return shortcode.Restore(html, results)
}

// URL страницы от корня сайта. Пустой slug: главная.
func URL(lang, slug string) string {
	u := "/"
	if lang != DefaultLang {
		u += lang + "/"
	}
	if slug != "" {
		u += slug + "/"
	}
	return u
}
