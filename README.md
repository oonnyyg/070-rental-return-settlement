# rental-return-settlement

租赁归还结算工具：跟踪在租设备，逐行接收 JSON 请求并返回 JSON 响应，说明每台设备的租金、损坏费用与押金去向。仅使用 Go 标准库。

## 运行

    go run ./cmd/rental --state <状态目录>

从 stdin 逐行读取 JSON 请求，每行处理后在 stdout 输出一行 JSON 响应。状态持久化在 `--state` 目录下的 `state.json`，进程退出后重开可继续办理。

## 协议

### 创建租单 create

    {"op":"create","rental_id":"R1","time":0,"devices":[
      {"device_id":"A","hourly_rate":100,"daily_cap":1000,"deposit":5000},
      {"device_id":"B","hourly_rate":50,"daily_cap":500,"deposit":300}]}

- `time`：出租时刻，整数分钟，非负；同一租单内所有设备同刻出租。
- `hourly_rate` / `daily_cap` / `deposit`：非负整数，单位为分。
- `rental_id` 全局唯一；`device_id` 在租单内唯一，未归还的设备不能再次出租。

### 分批归还 return

    {"op":"return","rental_id":"R1","returns":[
      {"device_id":"A","time":1500,"damage_fee":700}]}

- 一次可含多台设备；任一设备未知、重复或已归还，整批拒绝且不改变租单。
- `time` 不得早于出租时刻；`damage_fee` 为非负整数分。

### 查询 query

    {"op":"query","rental_id":"R1"}

返回租单与汇总 `summary`。已归还设备包含归还时间、计租分段 `segments`、租金 `rent`、退款 `refund` 或补款 `collect`；在租设备只显示状态，不按当前时钟预估。汇总含已结费用（`settled_rent`、`settled_damage`、`settled_total`）、累计退款/补款与未结押金 `outstanding_deposit`。

## 计租规则

- 每连续 1440 分钟独立计租为一段。
- 每段不足一小时向上取整，段租金 = 小时数 × 小时租金，且不超过日封顶。
- 零时长租金为零，无最低租金。

## 结算规则

租金加损坏费用先抵该台押金：费用不足押金时退差额（`refund`），超过时补差额（`collect`），不跨设备抵扣。已归还设备费用固定；全部设备归还后租单结清（`settled=true`）。

## 错误

非法请求返回 `{"ok":false,"code":...,"error":...}` 并继续处理下一行。`code` 可区分：`unknown_rental`、`rental_exists`、`duplicate_device`、`unknown_device`、`already_returned`、`invalid_time`、`invalid_amount`、`overflow`、`bad_request`。金额按 int64 运算，溢出拒绝且不改变状态。

## 测试

    go test ./...

覆盖跨日计租、分批归还、押金不足补款、整批拒绝原子性及重开查询。