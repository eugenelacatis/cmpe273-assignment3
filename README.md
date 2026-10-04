# Order / Inventory: REST vs gRPC (Go)

Two services, implemented twice. The Order Service takes an order (`item_id`, `quantity`) and asks the Inventory Service whether it can be filled. The Order -> Inventory hop is done once over REST/JSON and once over gRPC.

The client always talks to Order the same way (`POST /orders` with JSON), so the same `curl` works against both versions.

| Binary | Port | Role |
|---|---|---|
| `cmd/rest-order` | 8080 | Order Service, calls Inventory over HTTP/JSON |
| `cmd/rest-inventory` | 8081 | Inventory Service, `POST /inventory/check` |
| `cmd/grpc-order` | 8082 | Order Service, calls Inventory over gRPC |
| `cmd/grpc-inventory` | 50051 | Inventory Service, `inventory.Inventory/CheckInventory` |

Seed stock: `widget=10`, `gadget=0`, `gizmo=3`. The contract is in [`proto/inventory.proto`](proto/inventory.proto).

Configuration (env vars):

| Var | Applies to | Default | Meaning |
|---|---|---|---|
| `ORDER_TIMEOUT_MS` | order services | 1000 | REST client timeout / gRPC deadline |
| `INVENTORY_ADDR` | order services | `localhost:8081` / `localhost:50051` | Inventory address |
| `INVENTORY_DELAY_MS` | inventory services | 0 | Artificial delay, used for the failure demo |

## Prerequisites

- Go 1.25+ (the gRPC dependency requires it; Go 1.21+ downloads the right toolchain automatically)
- `protoc` with `protoc-gen-go` and `protoc-gen-go-grpc`, only if you edit the `.proto`. Generated code is committed in `internal/pb/`.

Regenerate with:

```bash
protoc --go_out=. --go_opt=module=cmpe273-lab --go-grpc_out=. --go-grpc_opt=module=cmpe273-lab proto/inventory.proto
```

Run the tests: `go test ./...`

## Run the REST version

```bash
# terminal 1
go run ./cmd/rest-inventory
# terminal 2
go run ./cmd/rest-order
# terminal 3
curl -i -X POST localhost:8080/orders -H 'Content-Type: application/json' -d '{"item_id":"widget","quantity":2}'
```

## Run the gRPC version

```bash
# terminal 1
go run ./cmd/grpc-inventory
# terminal 2
go run ./cmd/grpc-order
# terminal 3
curl -i -X POST localhost:8082/orders -H 'Content-Type: application/json' -d '{"item_id":"widget","quantity":2}'
```

## Successful request and response

REST (Order on :8080, Inventory over HTTP/JSON):

```
$ curl -X POST localhost:8080/orders -H 'Content-Type: application/json' -d '{"item_id":"widget","quantity":2}'
{"status":"confirmed","detail":"item available"}
HTTP 201
```

gRPC (Order on :8082, Inventory over gRPC):

```
$ curl -X POST localhost:8082/orders -H 'Content-Type: application/json' -d '{"item_id":"widget","quantity":2}'
{"status":"confirmed","detail":"item available"}
HTTP 201
```

Under the hood the gRPC call is `CheckInventory({item_id:"widget", quantity:2})` returning `{item_id:"widget", available:true, stock:10}`.

## Status mapping

| Situation | Inventory (REST) | Inventory (gRPC) | Order returns |
|---|---|---|---|
| Enough stock | 200 | `OK` | 201 |
| Unknown item | 404 | `NotFound` | 404 |
| Not enough stock | 409 | `FailedPrecondition` | 409 |
| Bad input to Order | n/a | n/a | 400 |
| Inventory too slow | client timeout | `DeadlineExceeded` | 504 |
| Inventory not running | connection error | `Unavailable` | 502 |

Unavailable-item evidence (same on both versions):

```
$ curl ... -d '{"item_id":"gadget","quantity":1}'
{"status":"rejected","detail":"insufficient stock"}
HTTP 409
$ curl ... -d '{"item_id":"nope","quantity":1}'
{"status":"rejected","detail":"unknown item"}
HTTP 404
```

## Failure handling: slow Inventory

`scripts/demo.sh rest` and `scripts/demo.sh grpc` run the whole sequence below against the real binaries. Order is started with a 500 ms timeout/deadline. Inventory is then restarted with `INVENTORY_DELAY_MS=2000`, which is longer than the limit. Full captured output is in [`evidence/rest.txt`](evidence/rest.txt) and [`evidence/grpc.txt`](evidence/grpc.txt).

### REST timeout handling

```
== 3. slow inventory (2000ms delay > 500ms limit) ==
{"status":"error","detail":"inventory service timed out"}
HTTP 504
```

Order service log:

```
order "widget" x2: inventory timed out: Post "http://localhost:8081/inventory/check": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
```

### gRPC deadline handling

```
== 3. slow inventory (2000ms delay > 500ms limit) ==
{"status":"error","detail":"inventory deadline exceeded"}
HTTP 504
```

Order service log:

```
order "widget" x2: inventory call failed: rpc error: code = DeadlineExceeded desc = context deadline exceeded
```

### Order Service keeps running after the failure

Right after the timeout, the demo checks the process is alive and sends another request, which is answered normally (a 400 from Order's own validation, so it does not depend on the slow Inventory):

REST:

```
== 4. order service still running ==
order service PID 21879 alive
{"status":"error","detail":"item_id and positive quantity required"}
HTTP 400
```

gRPC:

```
== 4. order service still running ==
order service PID 21962 alive
{"status":"error","detail":"item_id and positive quantity required"}
HTTP 400
```

The same behavior is covered by unit tests (`TestOrderTimeoutIs504AndHandlerSurvives`, `TestOrderDeadlineIs504AndHandlerSurvives`), which also check that a delay shorter than the limit still succeeds.

## REST vs gRPC: what I observed

The `.proto` file gave me the request/response types and client/server stubs from one source, while the REST side needed hand-written JSON structs on both ends that could drift apart without any compiler warning. REST was quicker to get running because it only needs the standard library and `curl`, whereas gRPC needed `protoc`, two plugins, and a newer Go toolchain. Error handling differed in kind: REST used HTTP status codes that I had to agree on by convention (404, 409), while gRPC has a fixed set of status codes (`NotFound`, `FailedPrecondition`, `DeadlineExceeded`) built into the client library. Timeouts looked similar from the Order Service, but in gRPC the deadline is a `context` passed into the call and also reaches the Inventory handler, which stops waiting when the caller gives up. For debugging, REST wins on readability because responses show up in a terminal as plain JSON, while gRPC traffic needs a tool like `grpcurl` or the logs.
