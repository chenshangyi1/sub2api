package rateconvert

import "testing"

func TestConvert(t *testing.T) {
	cases := []struct {
		v, custom float64
		mode      string
		want      float64
	}{
		{0.5, 0, "raw", 0.5},
		{0.5, 0, "", 0.5},
		{0.5, 0, "multiply_100", 50},
		{50, 0, "divide_100", 0.5},
		{0.5, 4, "multiply", 2},
		{0.5, 0, "multiply", 0.5},
		{50, 4, "divide", 12.5},
		{50, 0, "divide", 50},
		{0.5, 1.2, "custom", 1.2},
		{0.5, 1.2, "fixed", 1.2},
	}
	for _, tc := range cases {
		got := Convert(tc.v, tc.mode, tc.custom)
		if got != tc.want {
			t.Fatalf("Convert(%v,%q,%v)=%v want %v", tc.v, tc.mode, tc.custom, got, tc.want)
		}
	}
}
