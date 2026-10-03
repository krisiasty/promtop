package format

import "testing"

func TestTextHelpers(t *testing.T) {
	checks := map[string]string{
		Truncate("http_requests_total", 10): "http_requ…",
		Truncate("abc", 3):                  "abc",
		Truncate("abc", 0):                  "",
		Truncate("日本語ラベル", 5):               "日本…",
		Cut("abcdef", 4):                    "abcd",
		PadRight("ab", 4):                   "ab  ",
		PadLeft("ab", 4):                    "  ab",
		PadLeft("abcdef", 4):                "abcdef",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if Width("日本") != 4 || Width("▸●") != 2 || Width("\x1b[31mred\x1b[0m") != 3 {
		t.Error("Width must count cells and ignore ANSI")
	}
}
