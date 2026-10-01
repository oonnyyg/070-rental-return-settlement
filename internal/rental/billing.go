package rental

import "errors"

// SegmentMinutes is the length of one independent billing segment: 24 hours.
const SegmentMinutes = 1440

// Segment describes one 1440-minute billing window for one device.
type Segment struct {
	Minutes int64 `json:"minutes"`
	Hours   int64 `json:"hours"`
	Rent    int64 `json:"rent"`
}

var ErrOverflow = errors.New("amount overflow")

// add64 returns a+b or ErrOverflow when the int64 range is exceeded.
func add64(a, b int64) (int64, error) {
	if b > 0 && a > (1<<63-1)-b {
		return 0, ErrOverflow
	}
	if b < 0 && a < (-1<<63)-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// mul64 returns a*b or ErrOverflow when the int64 range is exceeded.
func mul64(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	r := a * b
	if r/a != b || (a == -1<<63 && b == -1) || (b == -1<<63 && a == -1) {
		return 0, ErrOverflow
	}
	return r, nil
}

// Rent computes the rent for a rental lasting duration minutes.
//
// Time is split into consecutive, independent 1440-minute segments. Within
// each segment any partial hour rounds up, the segment rent is capped at the
// daily cap, and a zero-length segment costs nothing. There is no minimum.
func Rent(duration, hourlyRate, dailyCap int64) (int64, []Segment, error) {
	if duration < 0 {
		return 0, nil, errors.New("negative duration")
	}
	var total int64
	var segs []Segment
	for remaining := duration; remaining > 0; {
		mins := remaining
		if mins > SegmentMinutes {
			mins = SegmentMinutes
		}
		hours := (mins + 59) / 60
		rent, err := mul64(hours, hourlyRate)
		if err != nil {
			return 0, nil, err
		}
		if rent > dailyCap {
			rent = dailyCap
		}
		total, err = add64(total, rent)
		if err != nil {
			return 0, nil, err
		}
		segs = append(segs, Segment{Minutes: mins, Hours: hours, Rent: rent})
		remaining -= mins
	}
	return total, segs, nil
}

// Settlement records how one returned device's costs settle against deposit.
type Settlement struct {
	Rent       int64 `json:"rent"`
	Damage     int64 `json:"damage"`
	Total      int64 `json:"total"`
	Deposit    int64 `json:"deposit"`
	Refund     int64 `json:"refund"`
	Supplement int64 `json:"supplement"`
}

// Settle charges rent plus damage against the deposit for one device only.
func Settle(rent, damage, deposit int64) (Settlement, error) {
	total, err := add64(rent, damage)
	if err != nil {
		return Settlement{}, err
	}
	s := Settlement{Rent: rent, Damage: damage, Total: total, Deposit: deposit}
	if total <= deposit {
		s.Refund = deposit - total
	} else {
		s.Supplement = total - deposit
	}
	return s, nil
}
