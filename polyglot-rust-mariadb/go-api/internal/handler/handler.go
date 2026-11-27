package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aditya/polyglot-rust-mariadb/go-api/internal/store"
)

type Handler struct {
	store      *store.Store
	rustAPIURL string
	appEnv     string
	startedAt  time.Time
	rpcClient  *http.Client
}

type createOrderRequest struct {
	CustomerID  string `json:"customer_id"`
	ItemSKU     string `json:"item_sku"`
	AmountCents int64  `json:"amount_cents"`
}

type scoreRequest struct {
	CustomerID  string `json:"customer_id"`
	AmountCents int64  `json:"amount_cents"`
}

type scoreResponse struct {
	RiskScore float64 `json:"risk_score"`
	RiskTier  string  `json:"risk_tier"`
}

func New(st *store.Store, rustAPIURL, appEnv string) *Handler {
	return &Handler{
		store:      st,
		rustAPIURL: strings.TrimRight(rustAPIURL, "/"),
		appEnv:     appEnv,
		startedAt:  time.Now(),
		rpcClient:  &http.Client{Timeout: 500 * time.Millisecond},
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("POST /api/v1/orders", h.createOrder)
	mux.HandleFunc("GET /api/v1/orders/{id}", h.getOrder)
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"service":        "go-api",
		"env":            h.appEnv,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
	})
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ready",
		"service": "go-api",
	})
}

func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON payload"})
		return
	}
	req.CustomerID = strings.TrimSpace(req.CustomerID)
	req.ItemSKU = strings.TrimSpace(req.ItemSKU)
	if req.CustomerID == "" || req.ItemSKU == "" || req.AmountCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "customer_id, item_sku, and positive amount_cents are required",
		})
		return
	}

	riskScore, riskTier := h.fetchRiskScore(r.Context(), req.CustomerID, req.AmountCents)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	order, err := h.store.CreateOrder(ctx, req.CustomerID, req.ItemSKU, req.AmountCents, riskScore, riskTier)
	if err != nil {
		log.Printf("create order failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to persist order"})
		return
	}

	writeJSON(w, http.StatusCreated, order)
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "order id required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	order, cacheState, err := h.store.GetOrder(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "order not found"})
		return
	}
	if err != nil {
		log.Printf("get order failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query order"})
		return
	}

	w.Header().Set("X-Cache", cacheState)
	writeJSON(w, http.StatusOK, order)
}

func (h *Handler) fetchRiskScore(parent context.Context, customerID string, amountCents int64) (float64, string) {
	ctx, cancel := context.WithTimeout(parent, 500*time.Millisecond)
	defer cancel()

	body, err := json.Marshal(scoreRequest{
		CustomerID:  customerID,
		AmountCents: amountCents,
	})
	if err != nil {
		return -1, "UNAVAILABLE"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.rustAPIURL+"/internal/v1/score", bytes.NewReader(body))
	if err != nil {
		return -1, "UNAVAILABLE"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.rpcClient.Do(req)
	if err != nil {
		log.Printf("rust risk-score RPC fallback triggered: %v", err)
		return -1, "UNAVAILABLE"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("rust risk-score RPC non-200 status=%d", resp.StatusCode)
		return -1, "UNAVAILABLE"
	}

	var scored scoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&scored); err != nil || scored.RiskTier == "" {
		return -1, "UNAVAILABLE"
	}
	return scored.RiskScore, scored.RiskTier
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
