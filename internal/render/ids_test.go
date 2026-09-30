package render

import "testing"

func TestIDs(t *testing.T) {
	s := newIDs()
	for _, tc := range []struct{ in, want string }{
		{"Как прикинуть цену у себя", "как-прикинуть-цену-у-себя"},
		{"Green Tea ускорил, разрыв остался", "green-tea-ускорил-разрыв-остался"},
		{"Кейс discord: сборка раз в две минуты", "кейс-discord-сборка-раз-в-две-минуты"},
		{"`runtime.GC()` и 10 млн", "runtimegc-и-10-млн"},
		{"Как прикинуть цену у себя", "как-прикинуть-цену-у-себя-1"},
		{"!!!", "section"},
	} {
		if got := string(s.Generate([]byte(tc.in), 0)); got != tc.want {
			t.Errorf("%q → %q, want %q", tc.in, got, tc.want)
		}
	}
}
