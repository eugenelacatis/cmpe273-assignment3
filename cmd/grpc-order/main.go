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
