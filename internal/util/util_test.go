package util

import "testing"

func TestTruncateMiddle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		cap  int
		want string
	}{
		{"empty", "", 10, ""},
		{"short", "hi", 10, "hi"},
		{"exact", "1234567890", 10, "1234567890"},
		{"one-over", "12345678901", 10, "1234…78901"},
		{"odd-cap", "abcdefgh", 7, "abc…fgh"},
		{"small-cap", "123456", 3, "1…6"},
		{"cap-one", "abcdef", 1, "…"},
		{"cap-zero", "abcdef", 0, ""},
		{"unicode", "αβγδεζη", 5, "αβ…ζη"}, // keep=4, half=2 => αβ…ζη
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateMiddle(tc.in, tc.cap)
			if got != tc.want {
				t.Fatalf("TruncateMiddle(%q, %d) = %q, want %q", tc.in, tc.cap, got, tc.want)
			}
			if len([]rune(got)) > tc.cap {
				t.Fatalf("result %q exceeds cap %d", got, tc.cap)
			}
		})
	}
}
