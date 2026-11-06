package lead

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/httpx"
)

// Repository is the persistence the handler depends on (implemented by *Store).
type Repository interface {
	List(ctx context.Context, p ListParams) ([]Lead, int, error)
	Get(ctx context.Context, id uuid.UUID) (Lead, error)
	Create(ctx context.Context, f Fields) (Lead, error)
	Update(ctx context.Context, id uuid.UUID, changes []Change) (Lead, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// Handler implements the lead operations of the generated api.ServerInterface.
// Routing, path-parameter parsing and query-parameter type conversion are
// generated from api/openapi.yaml; a missing or mistyped operation is a compile error.
type Handler struct {
	repo Repository
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(repo Repository) *Handler { return &Handler{repo: repo} }

type listResponse struct {
	Items []Lead `json:"items"`
	Total int    `json:"total"`
}

// detailResponse is the `LeadDetail` schema. latest_analysis is populated once
// the analysis feature exists; until then it is always null.
type detailResponse struct {
	Lead           Lead `json:"lead"`
	LatestAnalysis any  `json:"latest_analysis"`
}

func (h *Handler) ListLeads(w http.ResponseWriter, r *http.Request, params api.ListLeadsParams) {
	p, err := ParseListParams(params)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	leads, total, err := h.repo.List(r.Context(), p)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if leads == nil {
		leads = []Lead{}
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Items: leads, Total: total})
}

func (h *Handler) CreateLead(w http.ResponseWriter, r *http.Request) {
	var req api.LeadCreate
	if _, err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f, err := ValidateCreate(req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.repo.Create(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, l)
}

func (h *Handler) GetLead(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	l, err := h.repo.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detailResponse{Lead: l})
}

func (h *Handler) UpdateLead(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req api.LeadPatch
	raw, err := httpx.DecodeJSON(w, r, &req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	changes, err := ValidatePatch(req, raw)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.repo.Update(r.Context(), id, changes)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *Handler) DeleteLead(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := h.repo.Delete(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
