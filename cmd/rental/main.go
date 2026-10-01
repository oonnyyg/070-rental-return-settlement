package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/rental-return-settlement/internal/rental"
)

type request struct {
	Op       string               `json:"op"`
	OrderID  string               `json:"order_id"`
	RentTime int64                `json:"rent_time"`
	Devices  []rental.DeviceInput `json:"devices"`
	Returns  []rental.ReturnItem  `json:"returns"`
}

type response struct {
	OK    bool          `json:"ok"`
	Op    string        `json:"op,omitempty"`
	Data  any           `json:"data,omitempty"`
	Error *rental.Error `json:"error,omitempty"`
}

func emit(w *bufio.Writer, v any) {
	data, _ := json.Marshal(v)
	_, _ = w.Write(data)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}

func run() error {
	stateDir := flag.String("state", "", "directory holding rental state")
	flag.Parse()
	if *stateDir == "" {
		return fmt.Errorf("--state is required")
	}
	store, err := rental.Load(*stateDir)
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			handle(store, out, line)
		}
		if err != nil {
			break // EOF or read error: normal exit, state already persisted
		}
	}
	return nil
}

func handle(store *rental.Store, out *bufio.Writer, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		emit(out, response{Error: &rental.Error{Code: "invalid_json", Message: err.Error()}})
		return
	}
	switch req.Op {
	case "create":
		o, err := store.Create(req.OrderID, req.RentTime, req.Devices)
		if err != nil {
			emit(out, response{Error: asRentalError(err)})
			return
		}
		emit(out, response{OK: true, Op: "create", Data: o})
	case "return":
		o, _, err := store.Return(req.OrderID, req.Returns)
		if err != nil {
			emit(out, response{Error: asRentalError(err)})
			return
		}
		v, _ := store.Query(o.ID)
		emit(out, response{OK: true, Op: "return", Data: v.Orders[0]})
	case "query":
		v, err := store.Query(req.OrderID)
		if err != nil {
			emit(out, response{Error: asRentalError(err)})
			return
		}
		emit(out, response{OK: true, Op: "query", Data: v})
	default:
		emit(out, response{Error: &rental.Error{Code: "unknown_op", Message: "op must be create, return or query"}})
	}
}

func asRentalError(err error) *rental.Error {
	if e, ok := err.(*rental.Error); ok {
		return e
	}
	return &rental.Error{Code: "internal", Message: err.Error()}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rental:", err)
		os.Exit(1)
	}
}
