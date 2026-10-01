package rental

import "fmt"

// Error carries a stable machine-readable code plus a human message.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// DeviceInput describes a device in a create request.
type DeviceInput struct {
	ID         string `json:"id"`
	HourlyRate int64  `json:"hourly_rate"`
	DailyCap   int64  `json:"daily_cap"`
	Deposit    int64  `json:"deposit"`
}

// Create validates and records a new order.
func (s *Store) Create(id string, rentTime int64, in []DeviceInput) (*Order, error) {
	if id == "" {
		return nil, errf("invalid_order_id", "order id must not be empty")
	}
	if _, exists := s.orders[id]; exists {
		return nil, errf("order_exists", "order %q already exists", id)
	}
	if len(in) == 0 {
		return nil, errf("no_devices", "an order needs at least one device")
	}
	seen := map[string]bool{}
	devs := make([]*Device, 0, len(in))
	for _, d := range in {
		if d.ID == "" {
			return nil, errf("invalid_device_id", "device id must not be empty")
		}
		if seen[d.ID] {
			return nil, errf("duplicate_device", "device %q listed more than once", d.ID)
		}
		seen[d.ID] = true
		if d.HourlyRate < 0 || d.DailyCap < 0 || d.Deposit < 0 {
			return nil, errf("negative_amount", "device %q amounts must be non-negative", d.ID)
		}
		if other, busy := s.renting[d.ID]; busy {
			return nil, errf("device_rented", "device %q is already rented in order %q", d.ID, other)
		}
		devs = append(devs, &Device{
			ID:         d.ID,
			HourlyRate: d.HourlyRate,
			DailyCap:   d.DailyCap,
			Deposit:    d.Deposit,
		})
	}
	o := &Order{ID: id, RentTime: rentTime, Devices: devs}
	for _, d := range devs {
		s.renting[d.ID] = id
	}
	s.orders[id] = o
	if err := s.save(); err != nil {
		delete(s.orders, id)
		for _, d := range devs {
			delete(s.renting, d.ID)
		}
		return nil, err
	}
	return o, nil
}

// ReturnItem is one device in a batch return.
type ReturnItem struct {
	DeviceID string `json:"device_id"`
	Time     int64  `json:"time"`
	Damage   int64  `json:"damage"`
}

type plannedReturn struct {
	device *Device
	item   ReturnItem
	rent   int64
	segs   []Segment
}

// Return applies a batch return. If any item is invalid the whole batch is
// rejected and the order is left unchanged.
func (s *Store) Return(orderID string, items []ReturnItem) (*Order, []plannedReturn, error) {
	o, ok := s.orders[orderID]
	if !ok {
		return nil, nil, errf("unknown_order", "order %q not found", orderID)
	}
	if o.Settled {
		return nil, nil, errf("order_settled", "order %q is already settled", orderID)
	}
	if len(items) == 0 {
		return nil, nil, errf("no_returns", "a return needs at least one device")
	}
	byID := map[string]*Device{}
	for _, d := range o.Devices {
		byID[d.ID] = d
	}
	seen := map[string]bool{}
	planned := make([]plannedReturn, 0, len(items))
	for _, it := range items {
		if seen[it.DeviceID] {
			return nil, nil, errf("duplicate_return", "device %q returned more than once in batch", it.DeviceID)
		}
		seen[it.DeviceID] = true
		d, known := byID[it.DeviceID]
		if !known {
			return nil, nil, errf("unknown_device", "device %q is not part of order %q", it.DeviceID, orderID)
		}
		if d.Returned {
			return nil, nil, errf("already_returned", "device %q has already been returned", it.DeviceID)
		}
		if it.Damage < 0 {
			return nil, nil, errf("negative_amount", "device %q damage must be non-negative", it.DeviceID)
		}
		if it.Time < o.RentTime {
			return nil, nil, errf("time_before_rent", "device %q return time precedes rent time", it.DeviceID)
		}
		rent, segs, err := Rent(it.Time-o.RentTime, d.HourlyRate, d.DailyCap)
		if err != nil {
			return nil, nil, errf("overflow", "device %q rent overflows int64", it.DeviceID)
		}
		if _, err := Settle(rent, it.Damage, d.Deposit); err != nil {
			return nil, nil, errf("overflow", "device %q settlement overflows int64", it.DeviceID)
		}
		planned = append(planned, plannedReturn{device: d, item: it, rent: rent, segs: segs})
	}
	// All items validated: mutate only now, then persist.
	for _, p := range planned {
		st, _ := Settle(p.rent, p.item.Damage, p.device.Deposit)
		p.device.Returned = true
		p.device.ReturnTime = p.item.Time
		p.device.Damage = p.item.Damage
		p.device.Segments = p.segs
		p.device.Settlement = &st
		delete(s.renting, p.device.ID)
	}
	o.Settled = true
	for _, d := range o.Devices {
		if !d.Returned {
			o.Settled = false
			break
		}
	}
	if err := s.save(); err != nil {
		return nil, nil, err
	}
	return o, planned, nil
}
