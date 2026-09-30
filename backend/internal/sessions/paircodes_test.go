package sessions

import "testing"

func TestNewPairCodeIsSixDigits(t *testing.T) {
	for i := 0; i < 1000; i++ {
		c, err := newPairCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != 6 || NormalizePairCode(c) != c {
			t.Fatalf("bad code %q", c)
		}
	}
}

func TestNormalizePairCode(t *testing.T) {
	for in, want := range map[string]string{
		"482915":    "482915",
		"482 915":   "482915",
		" 482-915 ": "482915",
		"48a2915":   "482915",
		"":          "",
	} {
		if got := NormalizePairCode(in); got != want {
			t.Errorf("NormalizePairCode(%q) = %q, want %q", in, got, want)
		}
	}
}
