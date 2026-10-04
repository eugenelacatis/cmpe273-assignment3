package grpcsvc

import (
	"context"
	"encoding/json"
	"log"
	"math"
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ItemID == "" || req.Quantity <= 0 || req.Quantity > math.MaxInt32 {
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
		log.Printf("order %q x%d: inventory call failed: %v", req.ItemID, req.Quantity, err)
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
