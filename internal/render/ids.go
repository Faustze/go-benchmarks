package render

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
)

// ids строит id заголовков для якорей. Встроенный генератор goldmark оставляет только ASCII,
// и «Как прикинуть цену у себя» превращается в «----». Здесь буквы и цифры любого алфавита
// сохраняются, пробелы и дефисы схлопываются в один дефис, остальное выбрасывается.
type ids struct {
	used map[string]bool
}

func newIDs() *ids { return &ids{used: map[string]bool{}} }

func (s *ids) Generate(value []byte, _ ast.NodeKind) []byte {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(string(value)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-' || r == '_':
			dash = true
		}
	}
	id := b.String()
	if id == "" {
		id = "section"
	}
	base := id
	for i := 1; s.used[id]; i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	s.used[id] = true
	return []byte(id)
}

func (s *ids) Put(value []byte) { s.used[string(value)] = true }
