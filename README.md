# rental-return-settlement

Track rented equipment and explain where rent and deposits go. Standard library
only; all money is integer cents (`int64`) and all time is integer minutes.

## Run

```
go run ./cmd/rental --state ./state
```

The process reads one JSON request per line on stdin and writes one JSON
response per line on stdout. State is persisted to `state.json` in `--state`
after every successful create/return, so a normal exit and restart resumes with
the full history. The snapshot is written to a temp file and renamed
atomically.

## Requests

`create` opens an order: devices rented together at one instant.

```json
{"op":"create","order_id":"O1","rent_time":0,"devices":[
  {"id":"A","hourly_rate":100,"daily_cap":800,"deposit":500},
  {"id":"B","hourly_rate":100,"daily_cap":800,"deposit":200}
]}
```

- `order_id` must be unique; device `id` must be unique within the order.
- `hourly_rate`, `daily_cap`, `deposit` are non-negative integer cents.
- A device that is currently rented (belongs to an open order and not yet
  returned) cannot be added to any new order (`device_rented`).

`return` handles one batch return across multiple devices:

```json
{"op":"return","order_id":"O1","returns":[
  {"device_id":"A","time":1500,"damage":100},
  {"device_id":"B","time":1561}
]}
```

`damage` is optional and defaults to zero. Returns may be split into multiple
batches. The order becomes `settled` only after every device is returned.

`query` reports one order (`order_id` given) or all orders (omitted), including
each device's status, return time, billing segments, and settlement:

```json
{"op":"query","order_id":"O1"}
```

Devices still out show status `renting` only; costs are never estimated from
the current clock.

## Billing rules

For each device, rental time is divided into consecutive, independent
1440-minute (24-hour) segments starting at the order's rent time:

- Within a segment, minutes round up to whole hours (`ceil(minutes/60)`).
- Segment rent is `hours * hourly_rate`, capped at `daily_cap`.
- A zero-length rental costs zero; there is no minimum rent.
- Segments never share minutes, so each day's cap is independent
  (cross-day rentals pay each segment separately).

Settlement is per device and never crosses devices:

- `total = rent + damage`.
- If `total <= deposit`, the difference is `refund`-ed.
- If `total > deposit`, the difference is paid as `supplement`.

The query `summary` aggregates `settled_rent`, `settled_damage`,
`settled_refund`, `settled_supplement` across returned devices and
`outstanding_deposit` for devices still rented.

## Validation and errors

A batch return is atomic: if any device is unknown (`unknown_device`), repeated
within the batch (`duplicate_return`), already returned (`already_returned`),
or has a return time before rent time (`time_before_rent`), the whole batch is
rejected and the order is unchanged. Other codes include `unknown_order`,
`order_exists`, `order_settled`, `device_rented`, `negative_amount`,
`invalid_json`, `unknown_op`, and `overflow`.

Every response is `{"ok":true,...}` or
`{"ok":false,"error":{"code":"...","message":"..."}}`; after an error the
process keeps reading the next line. Arithmetic uses checked `int64`
operations and is rejected with `overflow` instead of wrapping.

## Tests

```
go test ./...
```

Coverage includes cross-day (1440-minute) segmentation, partial/batch
returns and atomic rejection, deposit surplus and shortfall, int64 overflow,
and continuing a partially returned order after restarting from the same
state directory.
