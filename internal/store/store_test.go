package store

import "testing"

func TestCheck(t *testing.T) {
	inv := New()
	cases := []struct {
		item string
		qty  int
		want Status
	}{
		{"widget", 5, Available},
		{"widget", 10, Available},
		{"widget", 11, Insufficient},
		{"gadget", 1, Insufficient},
		{"nope", 1, UnknownItem},
	}
	for _, c := range cases {
		got, _ := inv.Check(c.item, c.qty)
		if got != c.want {
			t.Errorf("Check(%q,%d)=%v want %v", c.item, c.qty, got, c.want)
		}
	}
}
