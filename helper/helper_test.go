package helper

import "testing"

// helper_test.go 驗證民國年 → 西元年的日期轉換。

func TestROCToAD(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		err  bool
	}{
		{"normal", "113/01/02", "2024-01-02", false},
		{"single-digit padded", "099/3/5", "2010-03-05", false},
		{"bad year", "abc/01/02", "", true},
		{"bad month", "113/xx/02", "", true},
		{"bad day", "113/01/zz", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			got, err := ROCToAD(c.in)
			// Assert
			if c.err {
				if err == nil {
					t.Fatalf("ROCToAD(%q) expected error", c.in)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("ROCToAD(%q) = (%q,%v), want %q", c.in, got, err, c.want)
			}
		})
	}
}
