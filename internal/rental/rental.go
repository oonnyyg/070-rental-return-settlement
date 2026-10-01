package rental

import (
	"errors"
	"fmt"
	"math"
)

// MinutesPerDay is the billing segment length in minutes.
const MinutesPerDay = 1440

// Error codes returned in JSON responses so clients can distinguish
// failure kinds.
const (
	ErrCodeUnknownRental   = "unknown_rental"
	ErrCodeRentalExists    = "rental_exists"
	ErrCodeDuplicateDevice = "duplicate_device"
	ErrCodeUnknownDevice   = "unknown_device"
	ErrCodeAlreadyReturned = "already_returned"
	ErrCodeInvalidTime     = "invalid_time"
	ErrCodeInvalidAmount   = "invalid_amount"
	ErrCodeOverflow        = "overflow"
	ErrCodeBadRequest      = "bad_request"
)

// Error is a domain error with a machine-readable code.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func newError(code, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// DeviceSpec describes a device rented at the same moment in one rental.
type DeviceSpec struct {
	DeviceID   string `json:"device_id"`
	HourlyRate int64  `json:"hourly_rate"`
	DailyCap   int64  `json:"daily_cap"`
	Deposit    int64  `json:"deposit"`
}

// Segment is one 1440-minute billing segment of a returned device.
type Segment struct {
	Minutes int64 `json:"minutes"`
	Hours   int64 `json:"hours"`
	Rent    int64 `json:"rent"`
}

// Device is a rented device and its settlement state.
type Device struct {
	DeviceID   string    `json:"device_id"`
	HourlyRate int64     `json:"hourly_rate"`
	DailyCap   int64     `json:"daily_cap"`
	Deposit    int64     `json:"deposit"`
	Returned   bool      `json:"returned"`
	ReturnTime int64     `json:"return_time,omitempty"`
	DamageFee  int64     `json:"damage_fee,omitempty"`
	Segments   []Segment `json:"segments,omitempty"`
	Rent       int64     `json:"rent,omitempty"`
	Refund     int64     `json:"refund,omitempty"`
	Collect    int64     `json:"collect,omitempty"`
}

// Rental is a rental order with its devices.
type Rental struct {
	RentalID  string             `json:"rental_id"`
	StartTime int64              `json:"start_time"`
	Devices   map[string]*Device `json:"devices"`
	Settled   bool               `json:"settled"`
}

func checkNonNegative(code string, name string, v int64) error {
	if v < 0 {
		return newError(code, "%s must be non-negative, got %d", name, v)
	}
	return nil
}

func addChecked(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, newError(ErrCodeOverflow, "int64 overflow adding %d and %d", a, b)
	}
	return a + b, nil
}

func mulChecked(a, b int64) (int64, error) {
	if a != 0 && b != 0 && a > math.MaxInt64/b {
		return 0, newError(ErrCodeOverflow, "int64 overflow multiplying %d and %d", a, b)
	}
	return a * b, nil
}

// BillSegments computes per-segment rent for a duration in minutes.
// Each consecutive 1440-minute block is billed independently; partial
// hours round up; each segment is capped at dailyCap. Zero duration is
// free.
func BillSegments(durationMinutes, hourlyRate, dailyCap int64) ([]Segment, int64, error) {
	segments := []Segment{}
	var total int64
	remaining := durationMinutes
	for remaining > 0 {
		seg := remaining
		if seg > MinutesPerDay {
			seg = MinutesPerDay
		}
		hours := (seg + 59) / 60
		rent, err := mulChecked(hours, hourlyRate)
		if err != nil {
			return nil, 0, err
		}
		if rent > dailyCap {
			rent = dailyCap
		}
		segments = append(segments, Segment{Minutes: seg, Hours: hours, Rent: rent})
		total, err = addChecked(total, rent)
		if err != nil {
			return nil, 0, err
		}
		remaining -= seg
	}
	return segments, total, nil
}

// Store holds all rentals.
type Store struct {
	Rentals map[string]*Rental `json:"rentals"`
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{Rentals: map[string]*Rental{}}
}

// CreateRental registers a new rental. All devices share startTime.
func (s *Store) CreateRental(rentalID string, startTime int64, specs []DeviceSpec) (*Rental, error) {
	if rentalID == "" {
		return nil, newError(ErrCodeBadRequest, "rental_id is required")
	}
	if _, ok := s.Rentals[rentalID]; ok {
		return nil, newError(ErrCodeRentalExists, "rental %q already exists", rentalID)
	}
	if err := checkNonNegative(ErrCodeInvalidTime, "time", startTime); err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, newError(ErrCodeBadRequest, "at least one device is required")
	}
	r := &Rental{RentalID: rentalID, StartTime: startTime, Devices: map[string]*Device{}}
	for _, sp := range specs {
		if sp.DeviceID == "" {
			return nil, newError(ErrCodeBadRequest, "device_id is required")
		}
		if _, dup := r.Devices[sp.DeviceID]; dup {
			return nil, newError(ErrCodeDuplicateDevice, "device %q duplicated in rental", sp.DeviceID)
		}
		if err := checkNonNegative(ErrCodeInvalidAmount, "hourly_rate", sp.HourlyRate); err != nil {
			return nil, err
		}
		if err := checkNonNegative(ErrCodeInvalidAmount, "daily_cap", sp.DailyCap); err != nil {
			return nil, err
		}
		if err := checkNonNegative(ErrCodeInvalidAmount, "deposit", sp.Deposit); err != nil {
			return nil, err
		}
		r.Devices[sp.DeviceID] = &Device{
			DeviceID:   sp.DeviceID,
			HourlyRate: sp.HourlyRate,
			DailyCap:   sp.DailyCap,
			Deposit:    sp.Deposit,
		}
	}
	s.Rentals[rentalID] = r
	return r, nil
}

// ReturnItem is one device return within a batch.
type ReturnItem struct {
	DeviceID  string `json:"device_id"`
	Time      int64  `json:"time"`
	DamageFee int64  `json:"damage_fee"`
}

// ReturnDevices settles a batch of device returns atomically: any
// unknown, duplicated or already-returned device rejects the whole
// batch without changing the rental.
func (s *Store) ReturnDevices(rentalID string, items []ReturnItem) (*Rental, error) {
	r, ok := s.Rentals[rentalID]
	if !ok {
		return nil, newError(ErrCodeUnknownRental, "rental %q not found", rentalID)
	}
	if len(items) == 0 {
		return nil, newError(ErrCodeBadRequest, "at least one return item is required")
	}
	seen := map[string]bool{}
	type computed struct {
		dev      *Device
		item     ReturnItem
		segments []Segment
		rent     int64
	}
	plan := make([]computed, 0, len(items))
	for _, it := range items {
		if seen[it.DeviceID] {
			return nil, newError(ErrCodeDuplicateDevice, "device %q duplicated in return batch", it.DeviceID)
		}
		seen[it.DeviceID] = true
		dev, ok := r.Devices[it.DeviceID]
		if !ok {
			return nil, newError(ErrCodeUnknownDevice, "device %q not in rental %q", it.DeviceID, rentalID)
		}
		if dev.Returned {
			return nil, newError(ErrCodeAlreadyReturned, "device %q already returned", it.DeviceID)
		}
		if err := checkNonNegative(ErrCodeInvalidTime, "time", it.Time); err != nil {
			return nil, err
		}
		if it.Time < r.StartTime {
			return nil, newError(ErrCodeInvalidTime, "return time %d before rental start %d", it.Time, r.StartTime)
		}
		if err := checkNonNegative(ErrCodeInvalidAmount, "damage_fee", it.DamageFee); err != nil {
			return nil, err
		}
		segments, rent, err := BillSegments(it.Time-r.StartTime, dev.HourlyRate, dev.DailyCap)
		if err != nil {
			return nil, err
		}
		if _, err := addChecked(rent, it.DamageFee); err != nil {
			return nil, err
		}
		plan = append(plan, computed{dev: dev, item: it, segments: segments, rent: rent})
	}
	for _, c := range plan {
		dev := c.dev
		dev.Returned = true
		dev.ReturnTime = c.item.Time
		dev.DamageFee = c.item.DamageFee
		dev.Segments = c.segments
		dev.Rent = c.rent
		total := c.rent + c.item.DamageFee
		if total < dev.Deposit {
			dev.Refund = dev.Deposit - total
		} else {
			dev.Collect = total - dev.Deposit
		}
	}
	settled := true
	for _, dev := range r.Devices {
		if !dev.Returned {
			settled = false
			break
		}
	}
	r.Settled = settled
	return r, nil
}

// Summary aggregates settled fees and outstanding deposits.
type Summary struct {
	SettledRent        int64 `json:"settled_rent"`
	SettledDamage      int64 `json:"settled_damage"`
	SettledTotal       int64 `json:"settled_total"`
	Refunded           int64 `json:"refunded"`
	Collected          int64 `json:"collected"`
	OutstandingDeposit int64 `json:"outstanding_deposit"`
}

// Query returns the rental and its summary. Returned devices show
// fixed settlement figures; unreturned devices only show status and
// contribute their deposit to the outstanding total.
func (s *Store) Query(rentalID string) (*Rental, *Summary, error) {
	r, ok := s.Rentals[rentalID]
	if !ok {
		return nil, nil, newError(ErrCodeUnknownRental, "rental %q not found", rentalID)
	}
	sum := &Summary{}
	var err error
	for _, dev := range r.Devices {
		if !dev.Returned {
			sum.OutstandingDeposit, err = addChecked(sum.OutstandingDeposit, dev.Deposit)
			if err != nil {
				return nil, nil, err
			}
			continue
		}
		if sum.SettledRent, err = addChecked(sum.SettledRent, dev.Rent); err != nil {
			return nil, nil, err
		}
		if sum.SettledDamage, err = addChecked(sum.SettledDamage, dev.DamageFee); err != nil {
			return nil, nil, err
		}
		if sum.Refunded, err = addChecked(sum.Refunded, dev.Refund); err != nil {
			return nil, nil, err
		}
		if sum.Collected, err = addChecked(sum.Collected, dev.Collect); err != nil {
			return nil, nil, err
		}
	}
	if sum.SettledTotal, err = addChecked(sum.SettledRent, sum.SettledDamage); err != nil {
		return nil, nil, err
	}
	return r, sum, nil
}

// IsDomainError reports whether err is a coded domain error.
func IsDomainError(err error) bool {
	var de *Error
	return errors.As(err, &de)
}