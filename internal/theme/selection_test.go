package theme

import (
	"testing"
	"time"

	"github.com/xwvike/gong/internal/config"
)

func selectionThemes() []Theme {
	return []Theme{{ID: "charlie"}, {ID: "alpha"}, {ID: "beta"}}
}

func TestRandomSelectionIsStableForOneTrigger(t *testing.T) {
	s := config.Schedule{Theme: config.ThemeRandom, Weekdays: []int{1, 2, 3, 4, 5}}
	target := time.Date(2026, 7, 27, 12, 0, 0, 0, time.Local)
	first, err := SelectFrom(selectionThemes(), s, 2, target)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		got, err := SelectFrom(selectionThemes(), s, 2, target)
		if err != nil || got.ID != first.ID {
			t.Fatalf("同一次触发选择不稳定：first=%q got=%q err=%v", first.ID, got.ID, err)
		}
	}
}

func TestRandomSelectionVariesAcrossTriggers(t *testing.T) {
	s := config.Schedule{Theme: config.ThemeRandom, Weekdays: []int{0, 1, 2, 3, 4, 5, 6}}
	seen := map[string]bool{}
	for day := 1; day <= 30; day++ {
		th, err := SelectFrom(selectionThemes(), s, 0,
			time.Date(2026, 7, day, 12, 0, 0, 0, time.Local))
		if err != nil {
			t.Fatal(err)
		}
		seen[th.ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("30 次触发始终只选到一个主题：%v", seen)
	}
}

func TestSequenceAdvancesOnlyOnScheduledDays(t *testing.T) {
	s := config.Schedule{Theme: config.ThemeSequence, Weekdays: []int{1, 2, 3, 4, 5}}
	friday := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	monday := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	a, err := SelectFrom(selectionThemes(), s, 0, friday)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SelectFrom(selectionThemes(), s, 0, monday)
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]int{"alpha": 0, "beta": 1, "charlie": 2}
	if order[b.ID] != (order[a.ID]+1)%len(order) {
		t.Fatalf("周五 %q 后的周一应是下一个主题，拿到 %q", a.ID, b.ID)
	}
}

func TestFixedSelectionNormalizesLegacyDefault(t *testing.T) {
	s := config.Schedule{Theme: "default"}
	th, err := SelectFrom([]Theme{{ID: "led"}}, s, 0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if th.ID != "led" {
		t.Fatalf("default 选择到 %q，想要 led", th.ID)
	}
}
