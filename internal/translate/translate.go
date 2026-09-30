// Package translate делает машинный черновик перевода статьи и следит, не устарел ли перевод.
//
// Черновик пишет локальный claude в режиме -p (подписка, без API-ключа), как скрипт перевода в notes.
// В файл перевода записываются translation = "machine" и хэш исходника. Автор вычитывает текст
// и меняет пометку на "manual": такой файл команда больше не перезаписывает.
package translate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Faustze/go-benchmarks/internal/content"
	"github.com/Faustze/go-benchmarks/internal/shortcode"
)

// Hash исходника: sha256 файла целиком, с нормализацией переводов строк.
func Hash(src []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// Prompt: инструкция модели. Глоссарий держит термины статьи одинаковыми от перевода к переводу.
const Prompt = `Translate this Russian technical article about Go performance into English.
The file is Markdown with a TOML frontmatter between +++ lines.

Rules:
- Translate only the values of title and description in the frontmatter. Keep every other key and value exactly as is.
- Keep every shortcode {{< ... >}} exactly as is, character for character, in the same place of the sentence.
- Keep code blocks unchanged, except translate comments inside them.
- Keep inline code, URLs, numbers, units (ms, ns, MB, s) and Markdown structure (headings, tables, lists, HTML tags) unchanged.
- Translate text inside HTML tags such as <summary>.
- Plain, precise engineering English. No marketing words, no added explanations, no summary at the end.

Glossary:
разметка / mark phase → marking; mark/scan по часам → mark/scan wall-clock time; сборка по таймеру → timer-triggered GC;
живой хип → live heap; раскладка → layout; плоская map → flat map; замер → benchmark; прогон → run;
фоновый воркер → background mark worker; логический CPU → logical CPU; кэш → cache; сборщик → the collector.

Output ONLY the translated file, no commentary and no code fence around it. The file follows the line ---FILE---.
---FILE---
`

// Runner вызывает модель: stdin → stdout. В тестах подменяется.
type Runner func(ctx context.Context, input string) (string, error)

// Claude: локальный claude -p без инструментов. Без --tools "" он работает как агент с доступом
// к файлам и может сам поправить файл статьи вместо того, чтобы вернуть текст.
func Claude(ctx context.Context, input string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", "--output-format", "text", "--no-session-persistence", "--tools", "")
	cmd.Stdin = strings.NewReader(input) // не аргументом: одна строка argv в Linux ограничена 128 KiB
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude -p: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// ErrManual: перевод вычитан автором, перезаписывать его нельзя.
var ErrManual = errors.New(`перевод вычитан (translation = "manual"), черновик не пишется`)

// File переводит src в dst. Готовый файл dst с translation = "manual" не трогает.
func File(ctx context.Context, run Runner, src, dst string) error {
	if old, err := os.ReadFile(dst); err == nil {
		if meta, _, err := content.Parse(old); err == nil && meta.Translation == "manual" {
			return ErrManual
		}
	}
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	out, err := run(ctx, Prompt+string(in))
	if err != nil {
		return err
	}
	res, err := Finish(in, []byte(out))
	if err != nil {
		return err
	}
	return os.WriteFile(dst, res, 0o644)
}

var fenceRe = regexp.MustCompile("(?m)^```")
var headingRe = regexp.MustCompile(`(?m)^#{1,6} `)

// Finish проверяет перевод против оригинала и записывает во frontmatter пометку и хэш.
// Модель могла потерять шорткод, блок кода или заголовок: тогда ошибка, файл не пишется.
func Finish(orig, tr []byte) ([]byte, error) {
	tr = bytes.TrimSpace(tr)
	// Модель всё же обернула файл в блок кода: снимаем обёртку. Только если ответ с неё начинается,
	// иначе «```» в конце это закрытие последнего настоящего блока кода статьи.
	if bytes.HasPrefix(tr, []byte("```")) {
		if _, rest, ok := bytes.Cut(tr, []byte("\n")); ok {
			tr = bytes.TrimSpace(bytes.TrimSuffix(rest, []byte("```")))
		}
	}
	tr = append(tr, '\n')

	if _, _, err := content.Parse(tr); err != nil {
		return nil, fmt.Errorf("перевод: %w", err)
	}
	_, oc, err := shortcode.Extract(orig)
	if err != nil {
		return nil, err
	}
	_, tc, err := shortcode.Extract(tr)
	if err != nil {
		return nil, fmt.Errorf("перевод: %w", err)
	}
	if !reflect.DeepEqual(oc, tc) {
		return nil, fmt.Errorf("перевод: шорткоды не совпадают с оригиналом (%d против %d)", len(tc), len(oc))
	}
	for name, re := range map[string]*regexp.Regexp{"блоков кода": fenceRe, "заголовков": headingRe} {
		if a, b := len(re.FindAll(orig, -1)), len(re.FindAll(tr, -1)); a != b {
			return nil, fmt.Errorf("перевод: %s %d, в оригинале %d", name, b, a)
		}
	}

	// Пометку и хэш пишет программа, не модель: убираем её версии и ставим свои перед закрывающим +++.
	lines := strings.Split(string(tr), "\n")
	var out []string
	closed := false
	for i, l := range lines {
		k := strings.TrimSpace(strings.SplitN(l, "=", 2)[0])
		if i > 0 && !closed && (k == "translation" || k == "source_hash") {
			continue
		}
		if i > 0 && !closed && l == "+++" {
			out = append(out, `translation = "machine"`, fmt.Sprintf("source_hash = %q", Hash(orig)))
			closed = true
		}
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n")), nil
}

// Stale: перевод сделан с другой версии исходника. Пустой хэш тоже считается устаревшим.
func Stale(orig []byte, meta content.Meta) bool {
	return meta.SourceHash != Hash(orig)
}
