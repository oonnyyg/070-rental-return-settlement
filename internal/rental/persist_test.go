package rental

import (
	"path/filepath"
	"testing"
)

func TestPersistReloadContinue(t *testing.T) {
	dir := t.TempDir()
	s, path, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != stateFileName {
		t.Fatalf("unexpected state path %q", path)
	}
	mustCreate(t, s, "R1", 0, []DeviceSpec{
		{DeviceID: "A", HourlyRate: 100, DailyCap: 1000, Deposit: 5000},
		{DeviceID: "B", HourlyRate: 50, DailyCap: 500, Deposit: 300},
	})
	if _, err := s.ReturnDevices("R1", []ReturnItem{{DeviceID: "A", Time: 1500, DamageFee: 700}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	// Reopen: continue returning the remaining device.
	s2, path2, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path2 != path {
		t.Fatal("state path changed across loads")
	}
	r, sum, err := s2.Query("R1")
	if err != nil {
		t.Fatal(err)
	}
	a := r.Devices["A"]
	if !a.Returned || a.ReturnTime != 1500 || len(a.Segments) != 2 {
		t.Fatalf("A not restored: %+v", a)
	}
	// rent 1100 + damage 700 = 1800, deposit 5000 -> refund 3200.
	if a.Refund != 3200 {
		t.Fatalf("A refund = %d, want 3200", a.Refund)
	}
	if sum.OutstandingDeposit != 300 {
		t.Fatalf("outstanding deposit = %d, want 300", sum.OutstandingDeposit)
	}
	// B: 3000 minutes = 2 full days + 120 min -> 500 + 500 + 2*50 = 1100.
	if _, err := s2.ReturnDevices("R1", []ReturnItem{{DeviceID: "B", Time: 3000}}); err != nil {
		t.Fatal(err)
	}
	if err := s2.SaveTo(path2); err != nil {
		t.Fatal(err)
	}

	s3, _, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, sum, err = s3.Query("R1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Settled {
		t.Fatal("rental should be settled after reload")
	}
	b := r.Devices["B"]
	// rent 1100 > deposit 300 -> collect 800.
	if b.Collect != 800 || b.Refund != 0 {
		t.Fatalf("B settlement wrong: %+v", b)
	}
	if sum.SettledTotal != 2900 || sum.Refunded != 3200 || sum.Collected != 800 || sum.OutstandingDeposit != 0 {
		t.Fatalf("summary wrong after reload: %+v", sum)
	}
}