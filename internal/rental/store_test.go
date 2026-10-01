package rental

import (
	"path/filepath"
	"testing"
)

func mustCreate(t *testing.T, s *Store, id string, rentTime int64, in []DeviceInput) *Order {
	t.Helper()
	o, err := s.Create(id, rentTime, in)
	if err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return o
}

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func code(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return ""
}

func TestBatchReturnRejectionIsAtomic(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, "O1", 1000, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 500},
		{ID: "B", HourlyRate: 100, DailyCap: 800, Deposit: 500},
	})
	// B is unknown to the order -> whole batch rejected, A stays renting.
	_, _, err := s.Return("O1", []ReturnItem{
		{DeviceID: "A", Time: 1060},
		{DeviceID: "ZZZ", Time: 1060},
	})
	if code(err) != "unknown_device" {
		t.Fatalf("err = %v", err)
	}
	o, _ := s.Order("O1")
	for _, d := range o.Devices {
		if d.Returned {
			t.Fatalf("device %s unexpectedly returned", d.ID)
		}
	}
	// Duplicate device inside one batch is rejected too.
	_, _, err = s.Return("O1", []ReturnItem{
		{DeviceID: "A", Time: 1060},
		{DeviceID: "A", Time: 1120},
	})
	if code(err) != "duplicate_return" {
		t.Fatalf("err = %v", err)
	}
	// Returning the same device again after a successful return is rejected.
	if _, _, err = s.Return("O1", []ReturnItem{{DeviceID: "A", Time: 1060}}); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Return("O1", []ReturnItem{{DeviceID: "A", Time: 1120}})
	if code(err) != "already_returned" {
		t.Fatalf("err = %v", err)
	}
}

func TestRentedDeviceCannotBeRentedAgain(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, "O1", 0, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 10},
	})
	_, err := s.Create("O2", 0, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 10},
	})
	if code(err) != "device_rented" {
		t.Fatalf("err = %v", err)
	}
}

func TestPartialReturnsSettlementAndSummary(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, "O1", 0, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 500},
		{ID: "B", HourlyRate: 100, DailyCap: 800, Deposit: 200},
	})
	// A: 61 min -> 200 rent + 100 damage = 300, refund 200.
	if _, _, err := s.Return("O1", []ReturnItem{{DeviceID: "A", Time: 61, Damage: 100}}); err != nil {
		t.Fatal(err)
	}
	// Order still open; B renting, no clock-based estimate.
	res, err := s.Query("O1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Orders[0].Status != "open" {
		t.Fatalf("status = %s", res.Orders[0].Status)
	}
	if res.Summary.OutstandingDeposit != 200 {
		t.Fatalf("outstanding deposit = %d", res.Summary.OutstandingDeposit)
	}
	if res.Summary.SettledRefund != 200 {
		t.Fatalf("refund = %d", res.Summary.SettledRefund)
	}

	// B: 61 min -> 200 rent + 100 damage = 300 > 200 deposit, supplement 100.
	if _, _, err := s.Return("O1", []ReturnItem{{DeviceID: "B", Time: 61, Damage: 100}}); err != nil {
		t.Fatal(err)
	}
	res, _ = s.Query("O1")
	if res.Orders[0].Status != "settled" {
		t.Fatalf("status = %s", res.Orders[0].Status)
	}
	if res.Summary.SettledSupplement != 100 {
		t.Fatalf("supplement = %d", res.Summary.SettledSupplement)
	}
	if res.Summary.OutstandingDeposit != 0 {
		t.Fatalf("outstanding deposit = %d", res.Summary.OutstandingDeposit)
	}
}

func TestCrossDayReturnPersistsSegments(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, "O1", 0, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 10000},
	})
	if _, _, err := s.Return("O1", []ReturnItem{{DeviceID: "A", Time: 1500}}); err != nil {
		t.Fatal(err)
	}
	res, _ := s.Query("O1")
	d := res.Orders[0].Devices[0]
	if len(d.Segments) != 2 || d.Segments[0].Rent != 800 || d.Segments[1].Rent != 100 {
		t.Fatalf("segments = %+v", d.Segments)
	}
}

func TestReopenContinuesAndQueries(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "O1", 0, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 500},
		{ID: "B", HourlyRate: 100, DailyCap: 800, Deposit: 500},
	})
	if _, _, err := s.Return("O1", []ReturnItem{{DeviceID: "A", Time: 60}}); err != nil {
		t.Fatal(err)
	}

	// Normal restart from the same directory.
	s2, err := Load(filepath.Join(dir))
	if err != nil {
		t.Fatal(err)
	}
	res, err := s2.Query("O1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Orders[0].Status != "open" || len(res.Orders[0].Devices) != 2 {
		t.Fatalf("after reopen: %+v", res.Orders[0])
	}
	// Return the remaining device using the reloaded store.
	if _, _, err := s2.Return("O1", []ReturnItem{{DeviceID: "B", Time: 60}}); err != nil {
		t.Fatal(err)
	}
	res, _ = s2.Query("O1")
	if res.Orders[0].Status != "settled" {
		t.Fatalf("status = %s", res.Orders[0].Status)
	}
	if res.Summary.SettledRent != 200 || res.Summary.SettledRefund != 800 {
		t.Fatalf("summary = %+v", res.Summary)
	}
}

func TestReturnBeforeRentRejected(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, "O1", 1000, []DeviceInput{
		{ID: "A", HourlyRate: 100, DailyCap: 800, Deposit: 10},
	})
	_, _, err := s.Return("O1", []ReturnItem{{DeviceID: "A", Time: 999}})
	if code(err) != "time_before_rent" {
		t.Fatalf("err = %v", err)
	}
}
