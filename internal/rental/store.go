package rental

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Device is one piece of equipment in a rental order.
type Device struct {
	ID         string      `json:"id"`
	HourlyRate int64       `json:"hourly_rate"`
	DailyCap   int64       `json:"daily_cap"`
	Deposit    int64       `json:"deposit"`
	Returned   bool        `json:"returned"`
	ReturnTime int64       `json:"return_time,omitempty"`
	Damage     int64       `json:"damage,omitempty"`
	Segments   []Segment   `json:"segments,omitempty"`
	Settlement *Settlement `json:"settlement,omitempty"`
}

// Order groups devices rented out at the same instant.
type Order struct {
	ID       string    `json:"id"`
	RentTime int64     `json:"rent_time"`
	Devices  []*Device `json:"devices"`
	Settled  bool      `json:"settled"`
}

// state is the persisted snapshot of the store.
type state struct {
	Orders []*Order `json:"orders"`
}

// Store keeps rental orders and tracks which devices are currently rented.
type Store struct {
	dir     string
	orders  map[string]*Order
	renting map[string]string // device ID -> order ID while not returned
}

// Load reads the store from dir, starting empty when no snapshot exists.
func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, orders: map[string]*Order{}, renting: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var snap state
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("corrupt state: %w", err)
	}
	for _, o := range snap.Orders {
		s.orders[o.ID] = o
		for _, d := range o.Devices {
			if !d.Returned {
				s.renting[d.ID] = o.ID
			}
		}
	}
	return s, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, "state.json") }

// save atomically rewrites the snapshot so an interrupted write cannot
// corrupt state needed to resume after a normal restart.
func (s *Store) save() error {
	snap := state{Orders: s.sortedOrders()}
	data, err := json.MarshalIndent(&snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

func (s *Store) sortedOrders() []*Order {
	out := make([]*Order, 0, len(s.orders))
	for _, o := range s.orders {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Order returns a copy-safe pointer to the named order.
func (s *Store) Order(id string) (*Order, bool) {
	o, ok := s.orders[id]
	return o, ok
}
