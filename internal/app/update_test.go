package app

import "testing"

func TestCompareVersionsDevRunNumbers(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "newer dev run", a: "dev-0012-20260523-abcdef0", b: "dev-0011-20260522-fedcba9", want: 1},
		{name: "older dev run", a: "dev-0011-20260522-fedcba9", b: "dev-0012-20260523-abcdef0", want: -1},
		{name: "same dev version", a: "dev-0012-20260523-abcdef0", b: "dev-0012-20260523-abcdef0", want: 0},
		{name: "dev release beats local dev", a: "dev-0012-20260523-abcdef0", b: "dev-local", want: 1},
		{name: "local dev is older than dev release", a: "dev-local", b: "dev-0012-20260523-abcdef0", want: -1},
		{name: "dev can upgrade stable users", a: "dev-0012-20260523-abcdef0", b: "0.1.0", want: 1},
		{name: "stable can upgrade dev users when selected", a: "0.1.1", b: "dev-0012-20260523-abcdef0", want: 1},
		{name: "stable v prefix is equivalent", a: "v0.1.1", b: "0.1.1", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSameVersionIgnoresStableVPrefix(t *testing.T) {
	if !sameVersion("v0.1.1", "0.1.1") {
		t.Fatal("expected v-prefixed stable versions to match")
	}
	if sameVersion("dev-0012-20260523-abcdef0", "dev-0013-20260523-abcdef0") {
		t.Fatal("expected different dev action versions not to match")
	}
}
