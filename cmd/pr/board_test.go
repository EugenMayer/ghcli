package pr

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPollBackoffDoublesEveryThreeFailuresUpToMax(t *testing.T) {
	b := newPollBackoff(5*time.Second, 60*time.Second)
	var got []time.Duration
	for range 18 {
		b.failure()
		got = append(got, b.current())
	}
	want := []time.Duration{5, 5, 10, 10, 10, 20, 20, 20, 40, 40, 40, 60, 60, 60, 60, 60, 60, 60}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("after %d failures interval = %s, want %s", i+1, got[i], want[i]*time.Second)
		}
	}
}

func TestBoxLinesFramesAndStripsColors(t *testing.T) {
	lines := boxLines([]string{"\x1b[34mbranch\x1b[0m updated", "ok"}, 80)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4: %q", len(lines), lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "\x1b") {
			t.Errorf("color codes left in %q", l)
		}
		if n := len([]rune(l)); n != len([]rune(lines[0])) {
			t.Errorf("line %q has width %d, want %d", l, n, len([]rune(lines[0])))
		}
	}
	if boxLines(nil, 80) != nil {
		t.Error("expected no box without lines")
	}
}

func TestBoardPlainOutputWithoutTerminal(t *testing.T) {
	var out bytes.Buffer
	b := newBoard(&out, []string{"#1 first", "#2 second"})
	b.row(0).Status("waiting")
	b.row(0).Note("a note")
	b.done(0, "merged")
	b.fail(1, "boom")

	want := "⟳ #1 first   waiting\n    a note\n✔ #1 first   merged\n✖ #2 second  boom\n"
	if out.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", out.String(), want)
	}
}
