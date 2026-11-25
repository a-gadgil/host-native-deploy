package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/aditya/polyglot-stack/go-api/internal/store"
	"github.com/google/uuid"
)

type Handler struct {
	store      *store.Store
	pythonURL  string
	env        string
	startedAt  time.Time
	httpClient *http.Client
}

func New(s *store.Store, pythonURL, env string) *Handler {
	return &Handler{
		store:     s,
		pythonURL: pythonURL,
		env:       env,
		startedAt: time.Now(),
		httpClient: &http.Client{
			Timeout: 500 * time.Millisecond, // Strict RPC budget for internal Python call
		},
	}
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

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"service":        "go-api",
		"env":            h.env,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
	})
}

func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ready",
		"service": "go-api",
	})
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	if req.CustomerID == "" || req.ItemSKU == "" || req.AmountCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "customer_id, item_sku, and positive amount_cents are required"})
		return
	}

	riskScore, riskTier := h.fetchRiskScore(r.Context(), req.CustomerID, req.AmountCents)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	created, err := h.store.CreateOrder(ctx, store.Order{
		ID:          uuid.NewString(),
		CustomerID:  req.CustomerID,
		ItemSKU:     req.ItemSKU,
		AmountCents: req.AmountCents,
		RiskScore:   riskScore,
		RiskTier:    riskTier,
	})
	if err != nil {
		slog.Error("failed to create order", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist order"})
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order UUID"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	order, cacheHit, err := h.store.GetOrder(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrOrderNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "order not found"})
			return
		}
		slog.Error("failed to fetch order", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve order"})
		return
	}

	if cacheHit {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	writeJSON(w, http.StatusOK, order)
}

func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	orders, err := h.store.ListOrders(ctx)
	if err != nil {
		slog.Error("failed to list orders", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list orders"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

func (h *Handler) fetchRiskScore(ctx context.Context, customerID string, amountCents int64) (float64, string) {
	body, err := json.Marshal(scoreRequest{
		CustomerID:  customerID,
		AmountCents: amountCents,
	})
	if err != nil {
		return -1.0, "UNAVAILABLE"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.pythonURL+"/internal/v1/score", bytes.NewReader(body))
	if err != nil {
		return -1.0, "UNAVAILABLE"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		slog.Warn("python-api scoring degraded; falling back to UNAVAILABLE", "error", err)
		return -1.0, "UNAVAILABLE"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("python-api returned non-200 status", "status", resp.StatusCode)
		return -1.0, "UNAVAILABLE"
	}

	var scored scoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&scored); err != nil {
		slog.Warn("failed to decode python-api response", "error", err)
		return -1.0, "UNAVAILABLE"
	}
	return scored.RiskScore, scored.RiskTier
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
