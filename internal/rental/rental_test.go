package rental

import (
	"math"
	"testing"
)

func mustCreate(t *testing.T, s *Store, id string, start int64, specs []DeviceSpec) {
	t.Helper()
	if _, err := s.CreateRental(id, start, specs); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func TestBillSegmentsCrossDay(t *testing.T) {
	// 1500 minutes = one full day segment + 60 minutes.
	segs, total, err := BillSegments(1500, 100, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("want 2 segments, got %d", len(segs))
	}
	if segs[0].Minutes != 1440 || segs[0].Hours != 24 || segs[0].Rent != 1000 {
		t.Fatalf("day segment capped wrong: %+v", segs[0])
	}
	if segs[1].Minutes != 60 || segs[1].Hours != 1 || segs[1].Rent != 100 {
		t.Fatalf("partial segment wrong: %+v", segs[1])
	}
	if total != 1100 {
		t.Fatalf("total = %d, want 1100", total)
	}
}

func TestBillSegmentsRoundUpAndZero(t *testing.T) {
	segs, total, err := BillSegments(61, 100, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Hours != 2 || total != 200 {
		t.Fatalf("61 minutes should bill 2 hours: %+v total=%d", segs, total)
	}
	segs, total, err = BillSegments(0, 100, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 0 || total != 0 {
		t.Fatalf("zero duration must be free: %+v total=%d", segs, total)
	}
}

func TestDepositRefundAndCollect(t *testing.T) {
	s := NewStore()
	mustCreate(t, s, "R1", 0, []DeviceSpec{
		{DeviceID: "A", HourlyRate: 100, DailyCap: 1000, Deposit: 500},
		{DeviceID: "B", HourlyRate: 100, DailyCap: 1000, Deposit: 100},
	})
	// A: 60min rent=100, damage=50, total=150 < 500 -> refund 350.
	// B: 60min rent=100, damage=200, total=300 > 100 -> collect 200.
	if _, err := s.ReturnDevices("R1", []ReturnItem{
		{DeviceID: "A", Time: 60, DamageFee: 50},
		{DeviceID: "B", Time: 60, DamageFee: 200},
	}); err != nil {
		t.Fatal(err)
	}
	r, sum, err := s.Query("R1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Settled {
		t.Fatal("rental should be settled")
	}
	a := r.Devices["A"]
	if a.Refund != 350 || a.Collect != 0 {
		t.Fatalf("A settlement wrong: %+v", a)
	}
	b := r.Devices["B"]
	if b.Refund != 0 || b.Collect != 200 {
		t.Fatalf("B settlement wrong: %+v", b)
	}
	if sum.SettledTotal != 450 || sum.Refunded != 350 || sum.Collected != 200 || sum.OutstandingDeposit != 0 {
		t.Fatalf("summary wrong: %+v", sum)
	}
}

func TestPartialReturnAtomicAndSummary(t *testing.T) {
	s := NewStore()
	mustCreate(t, s, "R1", 100, []DeviceSpec{
		{DeviceID: "A", HourlyRate: 10, DailyCap: 100, Deposit: 1000},
		{DeviceID: "B", HourlyRate: 10, DailyCap: 100, Deposit: 2000},
	})
	if _, err := s.ReturnDevices("R1", []ReturnItem{{DeviceID: "A", Time: 220}}); err != nil {
		t.Fatal(err)
	}
	// Batch with unknown device must be rejected without changing B.
	_, err := s.ReturnDevices("R1", []ReturnItem{
		{DeviceID: "B", Time: 300},
		{DeviceID: "ZZZ", Time: 300},
	})
	if de, ok := err.(*Error); !ok || de.Code != ErrCodeUnknownDevice {
		t.Fatalf("want unknown_device, got %v", err)
	}
	r, sum, err := s.Query("R1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Settled {
		t.Fatal("rental must not be settled")
	}
	if r.Devices["B"].Returned {
		t.Fatal("rejected batch must not change B")
	}
	if sum.OutstandingDeposit != 2000 || sum.SettledTotal != 20 {
		t.Fatalf("summary wrong: %+v", sum)
	}
	// Duplicate in batch rejected.
	if _, err := s.ReturnDevices("R1", []ReturnItem{
		{DeviceID: "B", Time: 300},
		{DeviceID: "B", Time: 300},
	}); err == nil {
		t.Fatal("duplicate device in batch must fail")
	}
	// Already-returned device rejected.
	if _, err := s.ReturnDevices("R1", []ReturnItem{{DeviceID: "A", Time: 300}}); err == nil {
		t.Fatal("returning A again must fail")
	}
	// Time before rental start rejected.
	if _, err := s.ReturnDevices("R1", []ReturnItem{{DeviceID: "B", Time: 50}}); err == nil {
		t.Fatal("return before start must fail")
	}
	if _, err := s.ReturnDevices("R1", []ReturnItem{{DeviceID: "B", Time: 300}}); err != nil {
		t.Fatal(err)
	}
	r, _, err = s.Query("R1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Settled {
		t.Fatal("rental should be settled after all devices returned")
	}
}

func TestOverflowRejected(t *testing.T) {
	s := NewStore()
	mustCreate(t, s, "R1", 0, []DeviceSpec{
		{DeviceID: "A", HourlyRate: math.MaxInt64, DailyCap: math.MaxInt64, Deposit: 0},
	})
	_, err := s.ReturnDevices("R1", []ReturnItem{{DeviceID: "A", Time: 120}})
	if de, ok := err.(*Error); !ok || de.Code != ErrCodeOverflow {
		t.Fatalf("want overflow, got %v", err)
	}
	if s.Rentals["R1"].Devices["A"].Returned {
		t.Fatal("overflowing return must not change state")
	}
}

func TestDuplicateDeviceAndRental(t *testing.T) {
	s := NewStore()
	mustCreate(t, s, "R1", 0, []DeviceSpec{{DeviceID: "A", HourlyRate: 1, DailyCap: 1, Deposit: 1}})
	if _, err := s.CreateRental("R1", 0, []DeviceSpec{{DeviceID: "B"}}); err == nil {
		t.Fatal("duplicate rental id must fail")
	}
	if _, err := s.CreateRental("R2", 0, []DeviceSpec{{DeviceID: "A"}, {DeviceID: "A"}}); err == nil {
		t.Fatal("duplicate device in rental must fail")
	}
	if _, err := s.CreateRental("R3", 0, []DeviceSpec{{DeviceID: "A", HourlyRate: -1}}); err == nil {
		t.Fatal("negative rate must fail")
	}
}