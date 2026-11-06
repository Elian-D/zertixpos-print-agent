package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		remote, local string
		want          bool
	}{
		{"0.2.0", "0.1.9", true},
		{"v0.1.10", "0.1.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"1.0.0", "0.9.9", true},
		{"0.2.0", "dev", false},
		{"", "0.1.0", false},
		{"0.2.0-beta", "0.1.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.remote, c.local); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, quería %v", c.remote, c.local, got, c.want)
		}
	}
}
