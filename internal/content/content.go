// Package content находит статьи в репозитории. Статья это leaf bundle, как у Hugo:
// папка замера с файлом index.<lang>.md, а данные и картинки лежат рядом с ним.
package content

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Meta: frontmatter статьи в формате TOML между строками +++
type Meta struct {
	Title       string    `toml:"title"`
	Description string    `toml:"description"`
	Date        time.Time `toml:"date"`
	Go          string    `toml:"go"`          // версия Go, на которой шёл замер
	Hardware    string    `toml:"hardware"`    // кратко: CPU и GOMAXPROCS
	Translation string    `toml:"translation"` // manual или machine, только у перевода
	SourceHash  string    `toml:"source_hash"` // хэш исходника, с которого сделан перевод
}

// Page: одна языковая версия статьи
type Page struct {
	Slug string // имя папки бандла, из него строится URL
	Lang string
	Dir  string // путь к папке бандла
	Meta Meta
	Body []byte // Markdown без frontmatter
}

// Number: номер замера из имени папки, «001» для 001-gc-pointers
func (p Page) Number() string {
	n, _, _ := strings.Cut(p.Slug, "-")
	return n
}

var delim = []byte("+++")

// Load читает все бандлы первого уровня в root (папке articles/). Порядок: по имени папки, внутри по порядку langs.
func Load(root string, langs []string) ([]Page, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var pages []Page
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		for _, lang := range langs {
			path := filepath.Join(dir, "index."+lang+".md")
			src, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			meta, body, err := Parse(src)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			pages = append(pages, Page{Slug: e.Name(), Lang: lang, Dir: dir, Meta: meta, Body: body})
		}
	}
	slices.SortStableFunc(pages, func(a, b Page) int { return strings.Compare(a.Slug, b.Slug) })
	return pages, nil
}

// LoadSection читает _index.<lang>.md из dir: текст раздела или главной, как branch bundle у Hugo.
// Файла на языке нет: страница этого языка просто не строится.
func LoadSection(dir string, langs []string) ([]Page, error) {
	var pages []Page
	for _, lang := range langs {
		path := filepath.Join(dir, "_index."+lang+".md")
		src, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		meta, body, err := Parse(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		pages = append(pages, Page{Lang: lang, Dir: dir, Meta: meta, Body: body})
	}
	return pages, nil
}

// Parse отделяет frontmatter от текста. Файл обязан начинаться со строки +++.
func Parse(src []byte) (Meta, []byte, error) {
	var meta Meta
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(src, append(delim, '\n'))
	if !ok {
		return meta, nil, errors.New("нет frontmatter: файл должен начинаться со строки +++")
	}
	front, body, ok := bytes.Cut(rest, append(append([]byte("\n"), delim...), '\n'))
	if !ok {
		return meta, nil, errors.New("frontmatter не закрыт строкой +++")
	}
	md, err := toml.Decode(string(front), &meta)
	if err != nil {
		return meta, nil, fmt.Errorf("frontmatter: %w", err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return meta, nil, fmt.Errorf("frontmatter: неизвестные поля %v", keys)
	}
	if meta.Title == "" {
		return meta, nil, errors.New("frontmatter: пустой title")
	}
	return meta, body, nil
}
