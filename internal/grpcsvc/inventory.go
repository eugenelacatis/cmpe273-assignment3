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
