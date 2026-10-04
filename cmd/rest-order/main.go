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
