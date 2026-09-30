package selfupdate

import "testing"

func TestParseSemver(t *testing.T) {
	ok := []struct{ in, out string }{
		{"1.2.3", "1.2.3"}, {"v1.2.3", "1.2.3"}, {"2.0.0-rc.1", "2.0.0-rc.1"},
		{"1.0.0+abc", "1.0.0+abc"}, {"v0.0.1-beta+b.1", "0.0.1-beta+b.1"},
	}
	for _, c := range ok {
		t.Run("ok "+c.in, func(t *testing.T) {
			v, err := ParseSemver(c.in)
			if err != nil || v.String() != c.out {
				t.Fatalf("got %v, %v", v, err)
			}
		})
	}
	for _, in := range []string{"", "dev", "unknown", "1.2", "1.2.3.4", "1.2.x", "01.2.3", "1.2.3-", "1.2.3-dirty", "1.2.3-4-gabc-dirty", "1.2.3-a..b"} {
		t.Run("bad "+in, func(t *testing.T) {
			if _, err := ParseSemver(in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSemverCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0}, {"1.2.3", "1.2.4", -1}, {"1.10.0", "1.9.0", 1},
		{"2.0.0", "1.99.99", 1}, {"1.0.0-rc.1", "1.0.0", -1}, {"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1}, {"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1}, {"1.0.0-rc.1", "1.0.0-beta.9", 1},
		{"1.0.0+a", "1.0.0+b", 0}, {"v1.0.0", "1.0.0", 0},
	} {
		t.Run(c.a+" vs "+c.b, func(t *testing.T) {
			a, _ := ParseSemver(c.a)
			b, _ := ParseSemver(c.b)
			if got := a.Compare(b); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
			if got := b.Compare(a); got != -c.want {
				t.Fatalf("reverse got %d want %d", got, -c.want)
			}
		})
	}
}
