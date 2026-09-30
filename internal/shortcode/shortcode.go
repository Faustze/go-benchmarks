// Package shortcode: вызовы {{< имя арг "арг с пробелами" >}} в тексте статьи, как у Hugo.
// Обработка идёт после Markdown: перед рендером вызов заменяется меткой, после рендера метка
// заменяется результатом. Так вывод шорткода (HTML таблицы, острова графика) не проходит через Markdown.
package shortcode

import (
	"fmt"
	"regexp"
	"strings"
)

// Call: один вызов шорткода.
type Call struct {
	Name string
	Args []string
}

var (
	callRe = regexp.MustCompile(`\{\{<\s*([a-z][a-z0-9-]*)((?:\s+(?:"[^"]*"|[^\s">]+))*)\s*>\}\}`)
	argRe  = regexp.MustCompile(`"([^"]*)"|([^\s"]+)`)
)

// Метка из букв и цифр: goldmark не превратит её ни в разметку, ни в сущность.
func token(i int) string { return fmt.Sprintf("GBSHORTCODE%04dEND", i) }

// Extract заменяет вызовы метками и возвращает текст и список вызовов по порядку.
// Незакрытый {{< … без >}} считается ошибкой: иначе он тихо уйдёт на страницу как текст.
func Extract(src []byte) ([]byte, []Call, error) {
	var calls []Call
	out := callRe.ReplaceAllFunc(src, func(m []byte) []byte {
		sub := callRe.FindSubmatch(m)
		c := Call{Name: string(sub[1])}
		for _, a := range argRe.FindAllSubmatch(sub[2], -1) {
			if a[1] != nil {
				c.Args = append(c.Args, string(a[1]))
			} else {
				c.Args = append(c.Args, string(a[2]))
			}
		}
		calls = append(calls, c)
		return []byte(token(len(calls) - 1))
	})
	if i := strings.Index(string(out), "{{<"); i >= 0 {
		line := strings.Count(string(out[:i]), "\n") + 1
		return nil, nil, fmt.Errorf("строка %d: не разобран вызов шорткода", line)
	}
	return out, calls, nil
}

// Restore подставляет результаты вызовов на место меток. Метка одна в абзаце: абзац целиком
// заменяется результатом, чтобы блочный HTML не оказался внутри <p>.
func Restore(html []byte, results []string) ([]byte, error) {
	s := string(html)
	for i, r := range results {
		t := token(i)
		if !strings.Contains(s, t) {
			return nil, fmt.Errorf("метка шорткода %d пропала после Markdown (шорткод внутри блока кода?)", i)
		}
		s = strings.ReplaceAll(s, "<p>"+t+"</p>", r)
		s = strings.ReplaceAll(s, t, r)
	}
	return []byte(s), nil
}
