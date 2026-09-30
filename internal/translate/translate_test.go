package translate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Faustze/go-benchmarks/internal/content"
)

const ru = "+++\ntitle = \"Сколько стоит указатель\"\ngo = \"1.27.1\"\n+++\n\n## Что делать\n\nЭффект: {{< num \"timer.a.green.mark_clock_ms\" ms >}}.\n\n```go\nx := 1 // комментарий\n```\n"

const en = "+++\ntitle = \"How much a pointer costs\"\ngo = \"1.27.1\"\ntranslation = \"manual\"\n+++\n\n## What to do\n\nEffect: {{< num \"timer.a.green.mark_clock_ms\" ms >}}.\n\n```go\nx := 1 // comment\n```\n"

func TestFinish(t *testing.T) {
	got, err := Finish([]byte(ru), []byte(en))
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := content.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	// Пометку ставит программа: даже если модель написала manual, черновик остаётся machine.
	if meta.Translation != "machine" || meta.SourceHash != Hash([]byte(ru)) || meta.Title != "How much a pointer costs" {
		t.Errorf("meta = %+v", meta)
	}
	if Stale([]byte(ru), meta) {
		t.Error("свежий перевод считается устаревшим")
	}
	if !Stale([]byte(ru+"\nновый абзац\n"), meta) {
		t.Error("исходник поменялся, а перевод не устарел")
	}
}

func TestFinishRejects(t *testing.T) {
	for name, tr := range map[string]string{
		"потерян шорткод":   strings.Replace(en, `{{< num "timer.a.green.mark_clock_ms" ms >}}`, "974 ms", 1),
		"изменён шорткод":   strings.Replace(en, `ms >}}`, `n0 >}}`, 1),
		"потерян заголовок": strings.Replace(en, "## What to do\n", "What to do\n", 1),
		"нет frontmatter":   strings.SplitN(en, "+++\n", 3)[2],
		"потерян блок кода": strings.Replace(en, "```go\nx := 1 // comment\n```\n", "x := 1\n", 1),
	} {
		if _, err := Finish([]byte(ru), []byte(tr)); err == nil {
			t.Errorf("%s: ошибки нет", name)
		}
	}
}

func TestFinishUnwrapsFence(t *testing.T) {
	got, err := Finish([]byte(ru), []byte("```markdown\n"+en+"```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "+++\n") || !strings.HasSuffix(string(got), "// comment\n```\n") {
		t.Errorf("обёртка не снята или срезан блок кода:\n%s", got)
	}
}

func TestFileKeepsManual(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "index.ru.md"), filepath.Join(dir, "index.en.md")
	os.WriteFile(src, []byte(ru), 0o644)
	calls := 0
	run := func(context.Context, string) (string, error) { calls++; return en, nil }

	if err := File(context.Background(), run, src, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	manual := strings.Replace(string(b), `translation = "machine"`, `translation = "manual"`, 1)
	os.WriteFile(dst, []byte(manual), 0o644)
	if err := File(context.Background(), run, src, dst); !errors.Is(err, ErrManual) {
		t.Errorf("вычитанный перевод: err = %v", err)
	}
	if calls != 1 {
		t.Errorf("модель вызвана %d раз, ждали 1", calls)
	}
}
