package lead

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

type Handler struct {
	repo Repository
}

func NewHandler(repo Repository) *Handler { return &Handler{repo: repo} }

// Routes mounts the lead endpoints under the given router (expected at /api/leads).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Get("/{id}", h.get)
	r.Patch("/{id}", h.update)
	r.Delete("/{id}", h.delete)
}

type listResponse struct {
	Items []Lead `json:"items"`
	Total int    `json:"total"`
}

// detailResponse is the shape of GET /api/leads/{id}. latest_analysis is
// populated once the analysis feature exists; until then it is always null.
type detailResponse struct {
	Lead           Lead `json:"lead"`
	LatestAnalysis any  `json:"latest_analysis"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, err := ParseListParams(r.URL.Query())
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

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
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

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	l, err := h.repo.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detailResponse{Lead: l})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req PatchRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	changes, err := ValidatePatch(req)
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

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseID treats a malformed ID like an unknown one: the resource does not exist.
func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}
