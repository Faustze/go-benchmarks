// Package i18n: строки интерфейса из i18n/<lang>.toml и форматирование чисел по правилам языка.
package i18n

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Strings: строки всех языков. Вложенные таблицы TOML разворачиваются в ключи через точку.
type Strings struct {
	fallback string
	byLang   map[string]map[string]string
}

// Load читает dir/<lang>.toml для каждого языка. fallback: язык, из которого берётся строка, если в другом её нет.
func Load(dir string, langs []string, fallback string) (*Strings, error) {
	s := &Strings{fallback: fallback, byLang: map[string]map[string]string{}}
	for _, lang := range langs {
		var raw map[string]any
		path := filepath.Join(dir, lang+".toml")
		if _, err := toml.DecodeFile(path, &raw); err != nil {
			return nil, err
		}
		flat := map[string]string{}
		if err := flatten("", raw, flat); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		s.byLang[lang] = flat
	}
	if _, ok := s.byLang[fallback]; !ok {
		return nil, fmt.Errorf("нет строк языка по умолчанию %q", fallback)
	}
	return s, nil
}

func flatten(prefix string, m map[string]any, out map[string]string) error {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch v := v.(type) {
		case string:
			out[key] = v
		case map[string]any:
			if err := flatten(key, v, out); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: ждали строку, а там %T", key, v)
		}
	}
	return nil
}

// T возвращает строку по ключу. Нет в языке: берётся из языка по умолчанию. Нет и там: ошибка,
// чтобы опечатка в ключе ломала сборку, а не тихо показывала ключ на странице.
func (s *Strings) T(lang, key string) (string, error) {
	if v, ok := s.byLang[lang][key]; ok {
		return v, nil
	}
	if v, ok := s.byLang[s.fallback][key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("нет строки %q", key)
}

// Prefix: все строки языка с ключами, которые начинаются с prefix (с откатом на язык по умолчанию).
// Нужен островам: скрипт графика получает подписи из страницы, а не держит свои.
func (s *Strings) Prefix(lang, prefix string) map[string]string {
	out := map[string]string{}
	for _, l := range []string{s.fallback, lang} {
		for k, v := range s.byLang[l] {
			if strings.HasPrefix(k, prefix) {
				out[k] = v
			}
		}
	}
	return out
}

// Format: числа по правилам языка. В русском группы разделяет неразрывный пробел, дробь отделяет запятая.
type Format struct {
	lang string
	p    *message.Printer
}

// NewFormat создаёт форматтер для языка.
func NewFormat(lang string) Format {
	return Format{lang: lang, p: message.NewPrinter(language.Make(lang))}
}

// Num: число с не более чем digits знаками после запятой, лишние нули не пишутся.
func (f Format) Num(v float64, digits int) string {
	return f.p.Sprint(number.Decimal(v, number.MaxFractionDigits(digits)))
}

// Fixed: ровно digits знаков после запятой.
func (f Format) Fixed(v float64, digits int) string {
	return f.p.Sprint(number.Decimal(v, number.MinFractionDigits(digits), number.MaxFractionDigits(digits)))
}

// Ms: длительность из миллисекунд, точность зависит от порядка, как на графиках.
func (f Format) Ms(v float64) string {
	switch {
	case v >= 1000:
		return f.Fixed(v/1000, 1) + " s"
	case v >= 10:
		return f.Num(v, 0) + " ms"
	case v >= 1:
		return f.Num(v, 1) + " ms"
	default:
		return f.Num(v, 2) + " ms"
	}
}

// Count: «25 млн», «33 тыс.» по-русски и «25M», «33K» по-английски.
func (f Format) Count(v float64) string {
	ru := f.lang == "ru"
	switch {
	case v >= 1e6:
		if ru {
			return f.Num(v/1e6, 1) + " млн"
		}
		return f.Num(v/1e6, 1) + "M"
	case v >= 1e3:
		if ru {
			return f.Num(v/1e3, 0) + " тыс."
		}
		return f.Num(v/1e3, 0) + "K"
	default:
		return f.Num(v, 0)
	}
}

// Round: округление до шага, 202 → 200 при step 10.
func Round(v, step float64) float64 { return math.Round(v/step) * step }
