package site

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// writeAssets раскладывает клиентскую часть: статику в корень, стили одним файлом, скрипты и шрифты в /assets/.
func writeAssets(cfg Config) error {
	web := cfg.web()
	if err := copyPath(filepath.Join(web, "static"), cfg.Out); err != nil {
		return err
	}
	for _, dir := range []string{"js", "fonts"} {
		if err := copyPath(filepath.Join(web, dir), filepath.Join(cfg.Out, "assets", dir)); err != nil {
			return err
		}
	}
	css, err := bundleCSS(filepath.Join(web, "css", "site.css"), map[string]bool{})
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(cfg.Out, "assets", "site.css"), css)
}

var importRe = regexp.MustCompile(`^@import\s+"([^"]+)";\s*$`)

// bundleCSS подставляет файлы из строк @import "путь"; на их место, рекурсивно. Пути от файла, где стоит @import.
// Исходник остаётся рабочим CSS: без сборки браузер сам загрузит @import по отдельности.
func bundleCSS(path string, seen map[string]bool) ([]byte, error) {
	if seen[path] {
		return nil, fmt.Errorf("%s: циклический @import", path)
	}
	seen[path] = true
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(src))
	for sc.Scan() {
		line := sc.Text()
		m := importRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			out.WriteString(line + "\n")
			continue
		}
		inner, err := bundleCSS(filepath.Join(filepath.Dir(path), m[1]), seen)
		if err != nil {
			return nil, err
		}
		out.Write(inner)
	}
	return out.Bytes(), sc.Err()
}

// checkOut не даёт удалить корень репозитория или папку вне его по ошибке в флагах.
func checkOut(cfg Config) error {
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(cfg.Out)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, out)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return fmt.Errorf("папка результата %s должна лежать внутри %s и не совпадать с ним", out, root)
	}
	return nil
}

// copyPath копирует файл или папку. Отсутствующий источник пропускается: не у каждого замера есть img/.
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
