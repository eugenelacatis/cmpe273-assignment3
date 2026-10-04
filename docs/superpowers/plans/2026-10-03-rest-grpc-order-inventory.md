# Order/Inventory REST vs gRPC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an Order Service and Inventory Service in Go, wired together once over REST/JSON and once over gRPC, with timeout/deadline failure demos and a README containing all evidence.

**Architecture:** Four small binaries sharing one in-memory stock store. Order Service always exposes `POST /orders` over HTTP to the client (so the same `curl` drives both versions); only the Order -> Inventory hop differs (HTTP+JSON vs gRPC). Inventory delay and Order timeout/deadline come from env vars so the slow-inventory demo needs no code change.

**Tech Stack:** Go 1.22+, `net/http` (stdlib), `google.golang.org/grpc`, `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc`.

**Spec:** `/home/eugenekarr/fall26/cmpe273/project-requirements.txt`

## Global Constraints

- Language: Go (spec allows Python or Go).
- Order accepts `item_id` and `quantity`; Inventory checks item + quantity availability.
- REST: JSON request/response, appropriate HTTP status codes, timeout on Order -> Inventory request.
- gRPC: contract in a `.proto` file, deadline on Order -> Inventory call, appropriate gRPC status when item unavailable.
- Failure demo: Inventory delay longer than REST timeout and gRPC deadline; both handled cleanly; Order Service stays running.
- README: run steps (REST, gRPC), successful REST + gRPC examples, REST timeout evidence, gRPC deadline evidence, Order-still-running evidence, 4-6 sentence REST vs gRPC comparison.
- Submission is the GitHub repo link only.
- Repo rules (Eugene's CLAUDE.md): conventional commits, NO `Co-Authored-By` trailer, no `git push` unless told, run `go vet ./...` before each commit.

## Review Focus

- Unknown `item_id` (not in stock): REST 404, gRPC `NotFound`, Order returns 404.
- Known item but `quantity` > stock: REST 409, gRPC `FailedPrecondition`, Order returns 409.
- `quantity <= 0` or missing/empty `item_id` or malformed JSON: Order returns 400, never reaches Inventory.
- Inventory unreachable (not running): Order returns 502, stays up.
- Inventory delay shorter than timeout: request still succeeds (timeout isn't always-on).

## File Structure

```
go.mod
proto/inventory.proto
internal/pb/                     generated (committed)
internal/store/store.go          in-memory stock + Check()
internal/store/store_test.go
internal/rest/inventory.go       REST inventory handler
internal/rest/order.go           order handler, calls inventory over HTTP
internal/rest/rest_test.go
internal/grpcsvc/inventory.go    gRPC inventory server
internal/grpcsvc/order.go        order handler, calls inventory over gRPC
internal/grpcsvc/grpc_test.go
cmd/rest-inventory/main.go       :8081
cmd/rest-order/main.go           :8080
cmd/grpc-inventory/main.go       :50051
cmd/grpc-order/main.go           :8082
scripts/demo.sh                  runs all evidence, tee to evidence/
evidence/                        captured terminal output
README.md
```

Env vars: `INVENTORY_DELAY_MS` (inventory, default 0), `ORDER_TIMEOUT_MS` (order, default 1000), `INVENTORY_ADDR` (order, default `localhost:8081` / `localhost:50051`).

Seed stock: `widget=10`, `gadget=0`, `gizmo=3`.

---

### Task 1: Scaffold + stock store

**Files:**
- Create: `go.mod`, `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces:**
- Produces: `store.Status` (`Available`, `UnknownItem`, `Insufficient`), `store.New() *Inventory`, `(*Inventory).Check(itemID string, qty int) (Status, int)` returning status and current stock.

- [ ] **Step 1: Init module**

```bash
cd /home/eugenekarr/fall26/cmpe273
go mod init cmpe273-lab
```

- [ ] **Step 2: Write failing test** `internal/store/store_test.go`

```go
package store

import "testing"

func TestCheck(t *testing.T) {
	inv := New()
	cases := []struct {
		item string
		qty  int
		want Status
	}{
		{"widget", 5, Available},
		{"widget", 10, Available},
		{"widget", 11, Insufficient},
		{"gadget", 1, Insufficient},
		{"nope", 1, UnknownItem},
	}
	for _, c := range cases {
		got, _ := inv.Check(c.item, c.qty)
		if got != c.want {
			t.Errorf("Check(%q,%d)=%v want %v", c.item, c.qty, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run, expect FAIL** — `go test ./internal/store/` (undefined: New)

- [ ] **Step 4: Implement** `internal/store/store.go`

```go
package store

type Status int

const (
	Available Status = iota
	UnknownItem
	Insufficient
)

type Inventory struct {
	stock map[string]int
}

func New() *Inventory {
	return &Inventory{stock: map[string]int{"widget": 10, "gadget": 0, "gizmo": 3}}
}

func (i *Inventory) Check(itemID string, qty int) (Status, int) {
	have, ok := i.stock[itemID]
	if !ok {
		return UnknownItem, 0
	}
	if qty > have {
		return Insufficient, have
	}
	return Available, have
}
```

- [ ] **Step 5: Run, expect PASS** — `go test ./internal/store/`
- [ ] **Step 6: Commit** — `go vet ./... && git add -A && git commit -m "feat: add in-memory inventory store"`

---

### Task 2: REST inventory + order handlers

**Files:**
- Create: `internal/rest/inventory.go`, `internal/rest/order.go`, `internal/rest/rest_test.go`, `cmd/rest-inventory/main.go`, `cmd/rest-order/main.go`

**Interfaces:**
- Consumes: `store.New()`, `Check`, `Status` from Task 1.
- Produces: `rest.NewInventoryHandler(inv *store.Inventory, delay time.Duration) http.Handler` (`POST /inventory/check`); `rest.NewOrderHandler(inventoryURL string, client *http.Client) http.Handler` (`POST /orders`).
- Shared JSON: request `{"item_id":string,"quantity":int}`; inventory response `{"item_id","available":bool,"stock":int}`; order response `{"status":"confirmed"|"rejected"|"error","detail":string}`.
- Status mapping. Inventory: 200 available, 404 unknown, 409 insufficient, 400 bad body. Order: 201 confirmed, 400 bad input, 404/409 passthrough, 504 inventory timeout, 502 inventory unreachable.

- [ ] **Step 1: Write failing tests** `internal/rest/rest_test.go`

```go
package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cmpe273-lab/internal/store"
)

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestInventoryStatuses(t *testing.T) {
	h := NewInventoryHandler(store.New(), 0)
	cases := []struct {
		body string
		want int
	}{
		{`{"item_id":"widget","quantity":2}`, 200},
		{`{"item_id":"nope","quantity":1}`, 404},
		{`{"item_id":"widget","quantity":99}`, 409},
		{`not json`, 400},
	}
	for _, c := range cases {
		if got := post(t, h, "/inventory/check", c.body).Code; got != c.want {
			t.Errorf("%s -> %d want %d", c.body, got, c.want)
		}
	}
}

func orderAgainst(t *testing.T, delay, timeout time.Duration) http.Handler {
	t.Helper()
	srv := httptest.NewServer(NewInventoryHandler(store.New(), delay))
	t.Cleanup(srv.Close)
	return NewOrderHandler(srv.URL, &http.Client{Timeout: timeout})
}

func TestOrderStatuses(t *testing.T) {
	h := orderAgainst(t, 0, time.Second)
	cases := []struct {
		body string
		want int
	}{
		{`{"item_id":"widget","quantity":2}`, 201},
		{`{"item_id":"nope","quantity":1}`, 404},
		{`{"item_id":"widget","quantity":99}`, 409},
		{`{"item_id":"widget","quantity":0}`, 400},
		{`{"item_id":"","quantity":1}`, 400},
		{`garbage`, 400},
	}
	for _, c := range cases {
		if got := post(t, h, "/orders", c.body).Code; got != c.want {
			t.Errorf("%s -> %d want %d", c.body, got, c.want)
		}
	}
}

func TestOrderTimeoutIs504AndHandlerSurvives(t *testing.T) {
	h := orderAgainst(t, 300*time.Millisecond, 100*time.Millisecond)
	if got := post(t, h, "/orders", `{"item_id":"widget","quantity":1}`).Code; got != 504 {
		t.Fatalf("got %d want 504", got)
	}
	if got := post(t, h, "/orders", `{"item_id":"widget","quantity":0}`).Code; got != 400 {
		t.Fatalf("handler dead after timeout: got %d", got)
	}
}

func TestOrderFastEnoughSucceeds(t *testing.T) {
	h := orderAgainst(t, 20*time.Millisecond, time.Second)
	if got := post(t, h, "/orders", `{"item_id":"widget","quantity":1}`).Code; got != 201 {
		t.Fatalf("got %d want 201", got)
	}
}

func TestOrderInventoryDown502(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	h := NewOrderHandler(url, &http.Client{Timeout: time.Second})
	if got := post(t, h, "/orders", `{"item_id":"widget","quantity":1}`).Code; got != 502 {
		t.Fatalf("got %d want 502", got)
	}
}
```

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/rest/` (undefined handlers)

- [ ] **Step 3: Implement** `internal/rest/inventory.go`

```go
package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"cmpe273-lab/internal/store"
)

type CheckRequest struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type CheckResponse struct {
	ItemID    string `json:"item_id"`
	Available bool   `json:"available"`
	Stock     int    `json:"stock"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func NewInventoryHandler(inv *store.Inventory, delay time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /inventory/check", func(w http.ResponseWriter, r *http.Request) {
		var req CheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		status, stock := inv.Check(req.ItemID, req.Quantity)
		resp := CheckResponse{ItemID: req.ItemID, Available: status == store.Available, Stock: stock}
		switch status {
		case store.UnknownItem:
			writeJSON(w, http.StatusNotFound, resp)
		case store.Insufficient:
			writeJSON(w, http.StatusConflict, resp)
		default:
			writeJSON(w, http.StatusOK, resp)
		}
	})
	return mux
}
```

- [ ] **Step 4: Implement** `internal/rest/order.go`

```go
package rest

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
)

type OrderResponse struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func NewOrderHandler(inventoryURL string, client *http.Client) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var req CheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ItemID == "" || req.Quantity <= 0 {
			writeJSON(w, http.StatusBadRequest, OrderResponse{"error", "item_id and positive quantity required"})
			return
		}
		body, _ := json.Marshal(req)
		resp, err := client.Post(inventoryURL+"/inventory/check", "application/json", bytes.NewReader(body))
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				log.Printf("order %s x%d: inventory timed out: %v", req.ItemID, req.Quantity, err)
				writeJSON(w, http.StatusGatewayTimeout, OrderResponse{"error", "inventory service timed out"})
				return
			}
			log.Printf("order %s x%d: inventory unreachable: %v", req.ItemID, req.Quantity, err)
			writeJSON(w, http.StatusBadGateway, OrderResponse{"error", "inventory service unreachable"})
			return
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			writeJSON(w, http.StatusCreated, OrderResponse{"confirmed", "item available"})
		case http.StatusNotFound:
			writeJSON(w, http.StatusNotFound, OrderResponse{"rejected", "unknown item"})
		case http.StatusConflict:
			writeJSON(w, http.StatusConflict, OrderResponse{"rejected", "insufficient stock"})
		default:
			writeJSON(w, http.StatusBadGateway, OrderResponse{"error", "unexpected inventory response"})
		}
	})
	return mux
}
```

- [ ] **Step 5: Implement mains**

`cmd/rest-inventory/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"cmpe273-lab/internal/rest"
	"cmpe273-lab/internal/store"
)

func main() {
	ms, _ := strconv.Atoi(os.Getenv("INVENTORY_DELAY_MS"))
	delay := time.Duration(ms) * time.Millisecond
	log.Printf("rest inventory on :8081 (delay %v)", delay)
	log.Fatal(http.ListenAndServe(":8081", rest.NewInventoryHandler(store.New(), delay)))
}
```

`cmd/rest-order/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"cmpe273-lab/internal/rest"
)

func main() {
	ms, err := strconv.Atoi(os.Getenv("ORDER_TIMEOUT_MS"))
	if err != nil {
		ms = 1000
	}
	addr := os.Getenv("INVENTORY_ADDR")
	if addr == "" {
		addr = "localhost:8081"
	}
	timeout := time.Duration(ms) * time.Millisecond
	log.Printf("rest order on :8080 -> http://%s (timeout %v)", addr, timeout)
	h := rest.NewOrderHandler("http://"+addr, &http.Client{Timeout: timeout})
	log.Fatal(http.ListenAndServe(":8080", h))
}
```

- [ ] **Step 6: Run, expect PASS** — `go test ./internal/rest/ && go build ./...`
- [ ] **Step 7: Commit** — `go vet ./... && git add -A && git commit -m "feat: add REST order and inventory services"`

---

### Task 3: Proto contract + codegen

**Files:**
- Create: `proto/inventory.proto`, `internal/pb/*.pb.go` (generated)

**Interfaces:**
- Produces: package `pb` with `InventoryClient`/`InventoryServer`, `UnimplementedInventoryServer`, `CheckRequest{ItemId string; Quantity int32}`, `CheckResponse{ItemId string; Available bool; Stock int32}`, `NewInventoryClient`, `RegisterInventoryServer`.

- [ ] **Step 1: Install tooling**

```bash
sudo apt-get install -y protobuf-compiler
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
go get google.golang.org/grpc google.golang.org/protobuf
```

- [ ] **Step 2: Write** `proto/inventory.proto`

```proto
syntax = "proto3";

package inventory;

option go_package = "cmpe273-lab/internal/pb;pb";

service Inventory {
  // Returns OK when stock suffices.
  // NOT_FOUND for unknown item, FAILED_PRECONDITION for insufficient stock.
  rpc CheckInventory(CheckRequest) returns (CheckResponse);
}

message CheckRequest {
  string item_id = 1;
  int32 quantity = 2;
}

message CheckResponse {
  string item_id = 1;
  bool available = 2;
  int32 stock = 3;
}
```

- [ ] **Step 3: Generate**

```bash
protoc --go_out=. --go_opt=module=cmpe273-lab --go-grpc_out=. --go-grpc_opt=module=cmpe273-lab proto/inventory.proto
go mod tidy && go build ./...
```
Expected: `internal/pb/inventory.pb.go` and `inventory_grpc.pb.go` exist, build passes.

- [ ] **Step 4: Commit** — `git add -A && git commit -m "feat: add inventory proto contract"`

---

### Task 4: gRPC inventory + order

**Files:**
- Create: `internal/grpcsvc/inventory.go`, `internal/grpcsvc/order.go`, `internal/grpcsvc/grpc_test.go`, `cmd/grpc-inventory/main.go`, `cmd/grpc-order/main.go`

**Interfaces:**
- Consumes: `pb.*` from Task 3, `store.*` from Task 1, `rest.CheckRequest` is NOT reused (order body type redefined locally to avoid cross-package coupling).
- Produces: `grpcsvc.NewInventoryServer(inv *store.Inventory, delay time.Duration) pb.InventoryServer`; `grpcsvc.NewOrderHandler(c pb.InventoryClient, deadline time.Duration) http.Handler` (`POST /orders`, same JSON as REST order).
- Status mapping. Inventory: `NotFound` unknown item, `FailedPrecondition` insufficient. Order HTTP: OK->201, NotFound->404, FailedPrecondition->409, DeadlineExceeded->504, Unavailable->502, other->500.

- [ ] **Step 1: Write failing tests** `internal/grpcsvc/grpc_test.go`

```go
package grpcsvc

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"cmpe273-lab/internal/pb"
	"cmpe273-lab/internal/store"
)

func startInventory(t *testing.T, delay time.Duration) pb.InventoryClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	pb.RegisterInventoryServer(s, NewInventoryServer(store.New(), delay))
	go s.Serve(lis)
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewInventoryClient(conn)
}

func post(h http.Handler, body string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body)))
	return rec.Code
}

func TestOrderStatuses(t *testing.T) {
	h := NewOrderHandler(startInventory(t, 0), time.Second)
	cases := []struct {
		body string
		want int
	}{
		{`{"item_id":"widget","quantity":2}`, 201},
		{`{"item_id":"nope","quantity":1}`, 404},
		{`{"item_id":"widget","quantity":99}`, 409},
		{`{"item_id":"widget","quantity":0}`, 400},
		{`{"item_id":"","quantity":1}`, 400},
		{`garbage`, 400},
	}
	for _, c := range cases {
		if got := post(h, c.body); got != c.want {
			t.Errorf("%s -> %d want %d", c.body, got, c.want)
		}
	}
}

func TestOrderDeadlineIs504AndHandlerSurvives(t *testing.T) {
	h := NewOrderHandler(startInventory(t, 300*time.Millisecond), 100*time.Millisecond)
	if got := post(h, `{"item_id":"widget","quantity":1}`); got != 504 {
		t.Fatalf("got %d want 504", got)
	}
	if got := post(h, `{"item_id":"widget","quantity":0}`); got != 400 {
		t.Fatalf("handler dead after deadline: got %d", got)
	}
}

func TestOrderFastEnoughSucceeds(t *testing.T) {
	h := NewOrderHandler(startInventory(t, 20*time.Millisecond), time.Second)
	if got := post(h, `{"item_id":"widget","quantity":1}`); got != 201 {
		t.Fatalf("got %d want 201", got)
	}
}

func TestOrderInventoryDown502(t *testing.T) {
	conn, _ := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
	t.Cleanup(func() { conn.Close() })
	h := NewOrderHandler(pb.NewInventoryClient(conn), time.Second)
	if got := post(h, `{"item_id":"widget","quantity":1}`); got != 502 {
		t.Fatalf("got %d want 502", got)
	}
}
```

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/grpcsvc/`

- [ ] **Step 3: Implement** `internal/grpcsvc/inventory.go`

```go
package grpcsvc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cmpe273-lab/internal/pb"
	"cmpe273-lab/internal/store"
)

type inventoryServer struct {
	pb.UnimplementedInventoryServer
	inv   *store.Inventory
	delay time.Duration
}

func NewInventoryServer(inv *store.Inventory, delay time.Duration) pb.InventoryServer {
	return &inventoryServer{inv: inv, delay: delay}
}

func (s *inventoryServer) CheckInventory(ctx context.Context, req *pb.CheckRequest) (*pb.CheckResponse, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	st, stock := s.inv.Check(req.ItemId, int(req.Quantity))
	switch st {
	case store.UnknownItem:
		return nil, status.Errorf(codes.NotFound, "unknown item %q", req.ItemId)
	case store.Insufficient:
		return nil, status.Errorf(codes.FailedPrecondition, "insufficient stock for %q: have %d, want %d", req.ItemId, stock, req.Quantity)
	}
	return &pb.CheckResponse{ItemId: req.ItemId, Available: true, Stock: int32(stock)}, nil
}
```

- [ ] **Step 4: Implement** `internal/grpcsvc/order.go`

```go
package grpcsvc

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cmpe273-lab/internal/pb"
)

type orderRequest struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type orderResponse struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func NewOrderHandler(c pb.InventoryClient, deadline time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var req orderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ItemID == "" || req.Quantity <= 0 {
			writeJSON(w, http.StatusBadRequest, orderResponse{"error", "item_id and positive quantity required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), deadline)
		defer cancel()
		_, err := c.CheckInventory(ctx, &pb.CheckRequest{ItemId: req.ItemID, Quantity: int32(req.Quantity)})
		if err == nil {
			writeJSON(w, http.StatusCreated, orderResponse{"confirmed", "item available"})
			return
		}
		log.Printf("order %s x%d: inventory call failed: %v", req.ItemID, req.Quantity, err)
		switch status.Code(err) {
		case codes.NotFound:
			writeJSON(w, http.StatusNotFound, orderResponse{"rejected", "unknown item"})
		case codes.FailedPrecondition:
			writeJSON(w, http.StatusConflict, orderResponse{"rejected", "insufficient stock"})
		case codes.DeadlineExceeded:
			writeJSON(w, http.StatusGatewayTimeout, orderResponse{"error", "inventory deadline exceeded"})
		case codes.Unavailable:
			writeJSON(w, http.StatusBadGateway, orderResponse{"error", "inventory service unreachable"})
		default:
			writeJSON(w, http.StatusInternalServerError, orderResponse{"error", "inventory call failed"})
		}
	})
	return mux
}
```

- [ ] **Step 5: Implement mains**

`cmd/grpc-inventory/main.go`:
```go
package main

import (
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"google.golang.org/grpc"

	"cmpe273-lab/internal/grpcsvc"
	"cmpe273-lab/internal/pb"
	"cmpe273-lab/internal/store"
)

func main() {
	ms, _ := strconv.Atoi(os.Getenv("INVENTORY_DELAY_MS"))
	delay := time.Duration(ms) * time.Millisecond
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterInventoryServer(s, grpcsvc.NewInventoryServer(store.New(), delay))
	log.Printf("grpc inventory on :50051 (delay %v)", delay)
	log.Fatal(s.Serve(lis))
}
```

`cmd/grpc-order/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cmpe273-lab/internal/grpcsvc"
	"cmpe273-lab/internal/pb"
)

func main() {
	ms, err := strconv.Atoi(os.Getenv("ORDER_TIMEOUT_MS"))
	if err != nil {
		ms = 1000
	}
	addr := os.Getenv("INVENTORY_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Duration(ms) * time.Millisecond
	log.Printf("grpc order on :8082 -> %s (deadline %v)", addr, deadline)
	log.Fatal(http.ListenAndServe(":8082", grpcsvc.NewOrderHandler(pb.NewInventoryClient(conn), deadline)))
}
```

- [ ] **Step 6: Run, expect PASS** — `go test ./... && go build ./...`
- [ ] **Step 7: Commit** — `go vet ./... && git add -A && git commit -m "feat: add gRPC order and inventory services"`

---

### Task 5: Demo script + captured evidence

**Files:**
- Create: `scripts/demo.sh`, `evidence/*.txt`

Evidence needs the real binaries, not just unit tests. Order timeout 500ms, inventory delay 2000ms.

- [ ] **Step 1: Write** `scripts/demo.sh`

```bash
#!/usr/bin/env bash
# Usage: scripts/demo.sh rest|grpc   -> prints success, timeout, and still-alive evidence
set -u
mode=${1:?usage: demo.sh rest|grpc}
cd "$(dirname "$0")/.."
mkdir -p evidence
go build -o bin/ ./cmd/...
if [ "$mode" = rest ]; then ORDER_PORT=8080; else ORDER_PORT=8082; fi
order() { curl -s -w '\nHTTP %{http_code}\n' -X POST localhost:$ORDER_PORT/orders \
  -H 'Content-Type: application/json' -d "$1"; }
pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null; }
trap cleanup EXIT

echo "== 1. fast inventory (success) =="
bin/$mode-inventory 2>evidence/$mode-inventory.log & pids+=($!)
ORDER_TIMEOUT_MS=500 bin/$mode-order 2>evidence/$mode-order.log & pids+=($!)
sleep 1
order '{"item_id":"widget","quantity":2}'

echo "== 2. unavailable cases =="
order '{"item_id":"gadget","quantity":1}'
order '{"item_id":"nope","quantity":1}'

echo "== 3. slow inventory (2000ms delay > 500ms limit) =="
kill "${pids[0]}"; sleep 0.5
INVENTORY_DELAY_MS=2000 bin/$mode-inventory 2>>evidence/$mode-inventory.log & pids[0]=$!
sleep 1
order '{"item_id":"widget","quantity":2}'

echo "== 4. order service still running =="
kill -0 "${pids[1]}" && echo "order service PID ${pids[1]} alive"
order '{"item_id":"widget","quantity":0}'

echo "== order service log =="
cat evidence/$mode-order.log
```

- [ ] **Step 2: Run both and capture**

```bash
chmod +x scripts/demo.sh
scripts/demo.sh rest | tee evidence/rest.txt
scripts/demo.sh grpc | tee evidence/grpc.txt
```
Expected in `rest.txt`: step 1 `HTTP 201`; step 2 `HTTP 409` then `HTTP 404`; step 3 `HTTP 504`; step 4 `alive` and `HTTP 400`. Same codes in `grpc.txt`, order log contains `DeadlineExceeded`.

- [ ] **Step 3: Add `bin/` to `.gitignore`, commit** — `echo bin/ >> .gitignore && git add -A && git commit -m "feat: add demo script and captured evidence"`

---

### Task 6: README

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write README sections, pasting real output from `evidence/*.txt`:**
  1. Overview + architecture (ports table: 8080 rest-order, 8081 rest-inventory, 8082 grpc-order, 50051 grpc-inventory)
  2. Prerequisites (Go 1.22+, protoc only if regenerating)
  3. Run REST version (two terminals, exact commands, env vars)
  4. Run gRPC version (same)
  5. Successful REST request + response (curl + `HTTP 201`)
  6. Successful gRPC request + response (curl to :8082, plus optionally `grpcurl -plaintext -d '{"item_id":"widget","quantity":2}' localhost:50051 inventory.Inventory/CheckInventory`)
  7. REST timeout evidence (step 3 of `rest.txt`, 504 + order log line)
  8. gRPC deadline evidence (step 3 of `grpc.txt`, 504 + `DeadlineExceeded` log line)
  9. Order Service still running (step 4 of both files)
  10. Status code mapping table (REST code vs gRPC code vs Order HTTP code)
  11. REST vs gRPC comparison, 4-6 sentences, written in own words from what was observed (typed contract and codegen vs hand-written JSON structs; error model: HTTP codes vs `codes.*`; deadline propagates through ctx and cancels server work vs client timeout; protoc toolchain setup cost vs stdlib-only REST; binary HTTP/2 vs text JSON, debuggable with curl vs needing grpcurl)

- [ ] **Step 2: Verify the sentence count is 4-6 and every spec README bullet has a section**
- [ ] **Step 3: Commit** — `git add -A && git commit -m "docs: add README with run steps and evidence"`

---

## Self-Review

- Spec coverage: REST endpoint/JSON/status codes/timeout -> Task 2. Proto/gRPC op/deadline/status -> Tasks 3-4. Slow-inventory demo + survive -> Tasks 2, 4 (tests) and 5 (real binaries). README items -> Task 6. All covered.
- Placeholders: none; README task lists exact content because its body is captured output.
- Type consistency: `store.Check`, `rest.NewInventoryHandler/NewOrderHandler`, `grpcsvc.NewInventoryServer/NewOrderHandler`, `pb.CheckRequest{ItemId,Quantity}` used identically across tasks.
- Review Focus: unknown/insufficient/bad-input/down/fast-enough all have tests in Tasks 2 and 4.
