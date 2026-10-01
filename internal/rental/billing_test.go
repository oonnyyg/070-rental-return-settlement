package rental

import "testing"

func TestRent(t *testing.T) {
	cases := []struct {
		name           string
		duration       int64
		hourly, cap    int64
		wantRent       int64
		wantSegMinutes []int64
		wantSegRent    []int64
	}{
		{"zero duration", 0, 100, 800, 0, nil, nil},
		{"partial hour rounds up", 1, 100, 800, 100, []int64{1}, []int64{100}},
		{"exact hour", 60, 100, 800, 100, []int64{60}, []int64{100}},
		{"61 minutes two hours", 61, 100, 800, 200, []int64{61}, []int64{200}},
		{"daily cap applies", 1440, 100, 800, 800, []int64{1440}, []int64{800}},
		{"cross day independent", 1500, 100, 800, 900,
			[]int64{1440, 60}, []int64{800, 100}},
		{"cross day zero tail still segment", 2880, 100, 800, 1600,
			[]int64{1440, 1440}, []int64{800, 800}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rent, segs, err := Rent(tc.duration, tc.hourly, tc.cap)
			if err != nil {
				t.Fatal(err)
			}
			if rent != tc.wantRent {
				t.Fatalf("rent = %d, want %d", rent, tc.wantRent)
			}
			if len(segs) != len(tc.wantSegMinutes) {
				t.Fatalf("segments = %v, want minutes %v", segs, tc.wantSegMinutes)
			}
			for i, m := range tc.wantSegMinutes {
				if segs[i].Minutes != m || segs[i].Rent != tc.wantSegRent[i] {
					t.Fatalf("seg %d = %+v, want minutes %d rent %d", i, segs[i], m, tc.wantSegRent[i])
				}
			}
		})
	}
}

func TestRentOverflow(t *testing.T) {
	if _, _, err := Rent(1440, 1<<62, 1<<62); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestSettle(t *testing.T) {
	if s, _ := Settle(300, 100, 500); s.Refund != 100 || s.Supplement != 0 {
		t.Fatalf("deposit surplus: %+v", s)
	}
	if s, _ := Settle(300, 300, 500); s.Refund != 0 || s.Supplement != 100 {
		t.Fatalf("deposit short: %+v", s)
	}
	if s, _ := Settle(0, 0, 0); s.Refund != 0 || s.Supplement != 0 {
		t.Fatalf("zero: %+v", s)
	}
	if _, err := Settle(1<<62, 1<<62, 0); err == nil {
		t.Fatal("expected overflow")
	}
}
