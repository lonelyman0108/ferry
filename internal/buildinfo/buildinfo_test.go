package buildinfo

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	Version, Commit = "v1.2.3", "abc1234"
	got := String()
	for _, want := range []string{"ferry v1.2.3", "commit abc1234"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q，缺少 %q", got, want)
		}
	}
}
