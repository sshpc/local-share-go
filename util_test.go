package main

import "testing"

func TestHumanSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{512, "512 B"},
		{2048, "2.0 KB"},
		{100 << 20, "100.0 MB"},
		{1 << 30, "1.0 GB"},
	}
	for _, tc := range tests {
		if got := humanSize(tc.in); got != tc.want {
			t.Fatalf("humanSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
