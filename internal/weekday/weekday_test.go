package weekday

import "testing"

func TestHas(t *testing.T) {
	m := Monday | Wednesday | Thursday
	for _, tc := range []struct {
		day  Mask
		want bool
	}{
		{Monday, true},
		{Wednesday, true},
		{Thursday, true},
		{Tuesday, false},
		{Friday, false},
		{Saturday, false},
		{Sunday, false},
	} {
		if got := m.Has(tc.day); got != tc.want {
			t.Errorf("Has(%v) = %v, want %v", tc.day, got, tc.want)
		}
	}
}

func TestFromForm(t *testing.T) {
	checked := map[string]bool{"Mon": true, "Wed": true, "Thu": true}
	m := FromForm(func(name string) bool { return checked[name] })
	want := Monday | Wednesday | Thursday
	if m != want {
		t.Errorf("FromForm() = %v, want %v", m, want)
	}
}
