// Command rental is a line-delimited JSON rental return settlement
// tool. It reads one JSON request per line from stdin and writes one
// JSON response per line to stdout. State persists under --state.
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
	Op       string              `json:"op"`
	RentalID string              `json:"rental_id"`
	Time     int64               `json:"time"`
	Devices  []rental.DeviceSpec `json:"devices"`
	Returns  []rental.ReturnItem `json:"returns"`
}

type response struct {
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	Code    string           `json:"code,omitempty"`
	Rental  *rental.Rental   `json:"rental,omitempty"`
	Summary *rental.Summary  `json:"summary,omitempty"`
}

func fail(err error) *response {
	resp := &response{OK: false, Error: err.Error()}
	if de, ok := err.(*rental.Error); ok {
		resp.Code = de.Code
	} else {
		resp.Code = rental.ErrCodeBadRequest
	}
	return resp
}

func handle(store *rental.Store, save func() error, req *request) *response {
	switch req.Op {
	case "create":
		r, err := store.CreateRental(req.RentalID, req.Time, req.Devices)
		if err != nil {
			return fail(err)
		}
		if err := save(); err != nil {
			return fail(err)
		}
		return &response{OK: true, Rental: r}
	case "return":
		r, err := store.ReturnDevices(req.RentalID, req.Returns)
		if err != nil {
			return fail(err)
		}
		if err := save(); err != nil {
			return fail(err)
		}
		return &response{OK: true, Rental: r}
	case "query":
		r, sum, err := store.Query(req.RentalID)
		if err != nil {
			return fail(err)
		}
		return &response{OK: true, Rental: r, Summary: sum}
	default:
		return fail(&rental.Error{Code: rental.ErrCodeBadRequest,
			Msg: fmt.Sprintf("unknown op %q (want create|return|query)", req.Op)})
	}
}

func main() {
	stateDir := flag.String("state", "", "directory holding rental state")
	flag.Parse()
	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "missing required --state directory")
		os.Exit(2)
	}
	store, path, err := rental.LoadStore(*stateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	save := func() error { return store.SaveTo(path) }

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		var resp *response
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			resp = fail(&rental.Error{Code: rental.ErrCodeBadRequest, Msg: "invalid JSON: " + err.Error()})
		} else {
			resp = handle(store, save, &req)
		}
		if err := enc.Encode(resp); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		out.Flush()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}