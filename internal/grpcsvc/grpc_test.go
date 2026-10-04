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

func TestOrderQuantityOverflowIs400(t *testing.T) {
	h := NewOrderHandler(startInventory(t, 0), time.Second)
	if got := post(h, `{"item_id":"widget","quantity":4294967297}`); got != 400 {
		t.Fatalf("got %d want 400 (int32 truncation would turn this into quantity 1)", got)
	}
}
