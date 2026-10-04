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
