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
