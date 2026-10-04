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
