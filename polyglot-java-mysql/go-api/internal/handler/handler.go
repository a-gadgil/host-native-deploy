package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aditya/polyglot-java-mysql/go-api/internal/store"
)

type Handler struct {
	store      *store.Store
	javaAPIURL string
	appEnv     string
	startedAt  time.Time
	httpClient *http.Client
}

func New(st *store.Store, javaAPIURL, appEnv string) *Handler {
	return &Handler{
		store:      st,
		javaAPIURL: strings.TrimRight(javaAPIURL, "/"),
		appEnv:     appEnv,
		startedAt:  time.Now(),
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)
	mux.HandleFunc("POST /api/v1/orders", h.CreateOrder)
	mux.HandleFunc("GET /api/v1/orders/{id}", h.GetOrder)
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"service":        "go-api",
		"env":            h.appEnv,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
	})
}

func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.store.Ready(ctx); err != nil {
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

type createOrderReq struct {
	CustomerID  string `json:"customer_id"`
	ItemSKU     string `json:"item_sku"`
	AmountCents int64  `json:"amount_cents"`
}

type scoreReq struct {
	CustomerID  string `json:"customer_id"`
	AmountCents int64  `json:"amount_cents"`
}

type scoreResp struct {
	RiskScore float64 `json:"risk_score"`
	RiskTier  string  `json:"risk_tier"`
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(req.CustomerID) == "" || strings.TrimSpace(req.ItemSKU) == "" || req.AmountCents <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "customer_id, item_sku, and positive amount_cents are required"})
		return
	}

	riskScore, riskTier := h.fetchRiskScore(r.Context(), req.CustomerID, req.AmountCents)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	order, err := h.store.CreateOrder(ctx, req.CustomerID, req.ItemSKU, req.AmountCents, riskScore, riskTier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing order id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	order, hit, err := h.store.GetOrderByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "order not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	if hit {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	writeJSON(w, http.StatusOK, order)
}

func (h *Handler) fetchRiskScore(ctx context.Context, customerID string, amountCents int64) (float64, string) {
	body, err := json.Marshal(scoreReq{CustomerID: customerID, AmountCents: amountCents})
	if err != nil {
		return -1, "UNAVAILABLE"
	}

	reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.javaAPIURL+"/internal/v1/score", bytes.NewReader(body))
	if err != nil {
		return -1, "UNAVAILABLE"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return -1, "UNAVAILABLE"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return -1, "UNAVAILABLE"
	}

	var sr scoreResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return -1, "UNAVAILABLE"
	}
	return sr.RiskScore, sr.RiskTier
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
