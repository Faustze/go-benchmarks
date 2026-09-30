// Команда site собирает go.faustze.tech из статей в папках замеров.
//
//	go run ./cmd/site build                  собрать в public/
//	go run ./cmd/site serve                  собрать, раздать на :1313 и пересобирать при изменениях
//	go run ./cmd/site translate [папка ...]  машинный черновик перевода на английский через claude -p;
//	                                         без аргументов: всё, где перевода нет или он устарел;
//	                                         _index: главная; вычитанные переводы (manual) не трогаются
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Faustze/go-benchmarks/internal/content"
	"github.com/Faustze/go-benchmarks/internal/site"
	"github.com/Faustze/go-benchmarks/internal/translate"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
	}
	fl := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	root := fl.String("root", ".", "корень репозитория")
	out := fl.String("out", "public", "папка результата")
	addr := fl.String("addr", "localhost:1313", "адрес для serve")
	fl.Parse(os.Args[2:])
	cfg := site.Config{Root: *root, Out: filepath.Join(*root, *out), Warn: func(f string, a ...any) { log.Printf("внимание: "+f, a...) }}

	switch os.Args[1] {
	case "build":
		start := time.Now()
		if err := site.Build(cfg); err != nil {
			log.Fatal(err)
		}
		log.Printf("собрано в %s за %v", cfg.Out, time.Since(start).Round(time.Millisecond))
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := serve(ctx, cfg, *addr); err != nil {
			log.Fatal(err)
		}
	case "translate":
		if err := translateAll(context.Background(), cfg, fl.Args()); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: site build|serve|translate [-root .] [-out public] [-addr localhost:1313] [папка ...]")
	os.Exit(2)
}

// translateAll пишет черновики перевода на английский. Без аргументов берёт всё, где перевода нет
// или он устарел, кроме вычитанных (translation = "manual").
func translateAll(ctx context.Context, cfg site.Config, names []string) error {
	arts := filepath.Join(cfg.Root, "articles")
	const to = "en"
	explicit := len(names) > 0 // папки названы явно: переводим их, даже если перевод свежий
	if !explicit {
		entries, err := os.ReadDir(arts)
		if err != nil {
			return err
		}
		names = []string{"_index"}
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
	}
	for _, name := range names {
		dir, slug := filepath.Join(arts, name), name
		if name == "_index" {
			dir, slug = arts, ""
		}
		src, dst := site.SourcePath(dir, slug, site.DefaultLang), site.SourcePath(dir, slug, to)
		orig, err := os.ReadFile(src)
		if errors.Is(err, os.ErrNotExist) {
			continue // папка без статьи
		}
		if err != nil {
			return err
		}
		if old, err := os.ReadFile(dst); err == nil && !explicit {
			if meta, _, err := content.Parse(old); err == nil && !translate.Stale(orig, meta) {
				continue // перевод свежий
			}
		}
		log.Printf("перевожу %s → %s", src, dst)
		start := time.Now()
		switch err := translate.File(ctx, translate.Claude, src, dst); {
		case errors.Is(err, translate.ErrManual):
			log.Printf("  пропуск: %v", err)
		case err != nil:
			return fmt.Errorf("%s: %w", name, err)
		default:
			log.Printf("  готово за %v, вычитай и поставь translation = \"manual\"", time.Since(start).Round(time.Second))
		}
	}
	return nil
}

func serve(ctx context.Context, cfg site.Config, addr string) error {
	if err := site.Build(cfg); err != nil {
		return err
	}
	go watch(ctx, cfg)
	srv := &http.Server{Addr: addr, Handler: noCache(http.FileServer(http.Dir(cfg.Out)))}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	log.Printf("http://%s/", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// noCache: после пересборки браузер должен взять свежие файлы, а не свою копию.
// Шрифты не меняются, их кэшируем: иначе каждая перезагрузка качает их заново и текст рисуется запасным шрифтом.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/fonts/") {
			w.Header().Set("Cache-Control", "max-age=3600")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

// watch раз в полсекунды сравнивает отпечаток исходников (самое позднее изменение и число файлов,
// чтобы заметить и удаление) и пересобирает сайт.
// Опрос вместо fsnotify: одна зависимость меньше, а на сотне файлов обход занимает миллисекунды.
func watch(ctx context.Context, cfg site.Config) {
	last := fingerprint(cfg)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		fp := fingerprint(cfg)
		if fp == last {
			continue
		}
		last = fp
		start := time.Now()
		if err := site.Build(cfg); err != nil {
			log.Printf("ошибка сборки: %v", err)
			continue
		}
		log.Printf("пересобрано за %v", time.Since(start).Round(time.Millisecond))
	}
}

// Папки, изменения в которых на сайт не влияют.
var skipDirs = []string{".git", "bin", "logs", "report", "cmd", "internal", "vendor"}

// Расширения исходников сайта.
var watchExt = []string{".md", ".html", ".css", ".js", ".json", ".toml", ".png", ".svg"}

func fingerprint(cfg site.Config) string {
	out, _ := filepath.Abs(cfg.Out)
	var t time.Time
	n := 0
	filepath.WalkDir(cfg.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			abs, _ := filepath.Abs(path)
			if abs == out || slices.Contains(skipDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !slices.Contains(watchExt, strings.ToLower(filepath.Ext(path))) {
			return nil
		}
		n++
		if info, err := d.Info(); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
		return nil
	})
	return fmt.Sprintf("%d/%d", t.UnixNano(), n)
}
