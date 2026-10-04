package rest

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestOrderLogEscapesItemID(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	h := NewOrderHandler(url, &http.Client{Timeout: time.Second})
	post(t, h, "/orders", `{"item_id":"a\nFAKE LOG LINE","quantity":1}`)
	if strings.Contains(buf.String(), "\nFAKE LOG LINE") {
		t.Fatalf("log injection: %q", buf.String())
	}
}
