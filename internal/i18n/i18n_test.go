package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

const nbsp = " "

func TestFormat(t *testing.T) {
	ru, en := NewFormat("ru"), NewFormat("en")
	for _, tc := range []struct{ got, want string }{
		{ru.Num(1949, 0), "1" + nbsp + "949"},
		{en.Num(1949, 0), "1,949"},
		{ru.Num(0.08, 2), "0,08"},
		{en.Num(0.08, 2), "0.08"},
		{ru.Num(2.0, 1), "2"},
		{ru.Fixed(1.297, 1), "1,3"},
		{ru.Ms(974), "974 ms"},
		{ru.Ms(1297), "1,3 s"},
		{ru.Ms(1.2), "1,2 ms"},
		{ru.Ms(0.058), "0,06 ms"},
		{en.Ms(2668), "2.7 s"},
		{ru.Count(25015425), "25 млн"},
		{ru.Count(32975), "33 тыс."},
		{en.Count(10032987), "10M"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
	if r := Round(202.2, 10); r != 200 {
		t.Errorf("Round = %v", r)
	}
}

func TestStrings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ru.toml"), []byte("[variant]\na = \"A · LRU\"\nb = \"B · плоская map\"\n[ui]\nmore = \"Подробности\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "en.toml"), []byte("[variant]\na = \"A · LRU\"\n"), 0o644)
	s, err := Load(dir, []string{"ru", "en"}, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.T("en", "variant.a"); v != "A · LRU" {
		t.Errorf("en variant.a = %q", v)
	}
	if v, _ := s.T("en", "variant.b"); v != "B · плоская map" {
		t.Errorf("откат на ru: %q", v)
	}
	if _, err := s.T("ru", "variant.x"); err == nil {
		t.Error("нет ошибки на неизвестный ключ")
	}
	if p := s.Prefix("en", "variant."); len(p) != 2 {
		t.Errorf("Prefix = %v", p)
	}
}
