package rental

// DeviceView is the query representation of one device.
type DeviceView struct {
	ID         string      `json:"id"`
	Status     string      `json:"status"`
	HourlyRate int64       `json:"hourly_rate"`
	DailyCap   int64       `json:"daily_cap"`
	Deposit    int64       `json:"deposit"`
	ReturnTime *int64      `json:"return_time,omitempty"`
	Damage     *int64      `json:"damage,omitempty"`
	Segments   []Segment   `json:"segments,omitempty"`
	Settlement *Settlement `json:"settlement,omitempty"`
}

// OrderView is the query representation of one order.
type OrderView struct {
	ID       string       `json:"id"`
	RentTime int64        `json:"rent_time"`
	Status   string       `json:"status"`
	Devices  []DeviceView `json:"devices"`
}

// Summary aggregates money across all orders.
type Summary struct {
	SettledRent        int64 `json:"settled_rent"`
	SettledDamage      int64 `json:"settled_damage"`
	SettledRefund      int64 `json:"settled_refund"`
	SettledSupplement  int64 `json:"settled_supplement"`
	OutstandingDeposit int64 `json:"outstanding_deposit"`
}

// QueryResult holds all orders plus the aggregate summary.
type QueryResult struct {
	Orders  []OrderView `json:"orders"`
	Summary Summary     `json:"summary"`
}

func viewOf(o *Order) OrderView {
	v := OrderView{ID: o.ID, RentTime: o.RentTime, Status: "open", Devices: []DeviceView{}}
	if o.Settled {
		v.Status = "settled"
	}
	for _, d := range o.Devices {
		dv := DeviceView{
			ID:         d.ID,
			HourlyRate: d.HourlyRate,
			DailyCap:   d.DailyCap,
			Deposit:    d.Deposit,
			Segments:   d.Segments,
			Settlement: d.Settlement,
		}
		if d.Returned {
			dv.Status = "returned"
			t := d.ReturnTime
			dm := d.Damage
			dv.ReturnTime = &t
			dv.Damage = &dm
		} else {
			dv.Status = "renting"
		}
		v.Devices = append(v.Devices, dv)
	}
	return v
}

// Query builds views for one order or all orders and computes summaries.
func (s *Store) Query(orderID string) (*QueryResult, error) {
	var orders []*Order
	if orderID != "" {
		o, ok := s.orders[orderID]
		if !ok {
			return nil, errf("unknown_order", "order %q not found", orderID)
		}
		orders = []*Order{o}
	} else {
		orders = s.sortedOrders()
	}
	res := &QueryResult{Orders: []OrderView{}}
	for _, o := range orders {
		res.Orders = append(res.Orders, viewOf(o))
		for _, d := range o.Devices {
			if d.Returned {
				st := d.Settlement
				sum, err := add64(res.Summary.SettledRent, st.Rent)
				if err != nil {
					return nil, errf("overflow", "summary overflow")
				}
				res.Summary.SettledRent = sum
				res.Summary.SettledDamage, _ = add64(res.Summary.SettledDamage, st.Damage)
				res.Summary.SettledRefund, _ = add64(res.Summary.SettledRefund, st.Refund)
				res.Summary.SettledSupplement, _ = add64(res.Summary.SettledSupplement, st.Supplement)
			} else {
				res.Summary.OutstandingDeposit, _ = add64(res.Summary.OutstandingDeposit, d.Deposit)
			}
		}
	}
	return res, nil
}
