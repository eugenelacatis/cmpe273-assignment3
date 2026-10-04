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
				log.Printf("order %q x%d: inventory timed out: %v", req.ItemID, req.Quantity, err)
				writeJSON(w, http.StatusGatewayTimeout, OrderResponse{"error", "inventory service timed out"})
				return
			}
			log.Printf("order %q x%d: inventory unreachable: %v", req.ItemID, req.Quantity, err)
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
