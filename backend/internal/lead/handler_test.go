package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/httpx"
)

// fakeRepo holds at most one lead and records writes; it is enough to exercise
// routing, request decoding, error mapping and response shapes.
type fakeRepo struct {
	lead    *Lead
	created *Fields
	changes []Change
}

func (f *fakeRepo) List(context.Context, ListParams) ([]Lead, int, error) {
	if f.lead == nil {
		return nil, 0, nil
	}
	return []Lead{*f.lead}, 1, nil
}

func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (Lead, error) {
	if f.lead == nil || f.lead.ID != id {
		return Lead{}, ErrNotFound
	}
	return *f.lead, nil
}

func (f *fakeRepo) Create(_ context.Context, fl Fields) (Lead, error) {
	f.created = &fl
	now := time.Now().UTC()
	return Lead{ID: uuid.New(), CompanyName: fl.CompanyName, Status: fl.Status, CreatedAt: now, UpdatedAt: now}, nil
}

func (f *fakeRepo) Update(ctx context.Context, id uuid.UUID, c []Change) (Lead, error) {
	f.changes = c
	return f.Get(ctx, id)
}

func (f *fakeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := f.Get(ctx, id)
	return err
}

// newTestRouter mounts the handler exactly like cmd/api does.
func newTestRouter(repo Repository) http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		api.HandlerWithOptions(NewHandler(repo), api.ChiServerOptions{
			BaseRouter:       r,
			ErrorHandlerFunc: httpx.ParamError,
		})
	})
	return r
}

type errorBody struct {
	Error struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
	} `json:"error"`
}

func do(t *testing.T, repo Repository, method, path, body string) (*httptest.ResponseRecorder, errorBody) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	newTestRouter(repo).ServeHTTP(rec, req)
	var eb errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	return rec, eb
}

func sampleLead() *Lead {
	now := time.Now().UTC()
	return &Lead{ID: uuid.New(), CompanyName: "Acme", Status: api.StatusNew, CreatedAt: now, UpdatedAt: now}
}

func TestHandlerErrors(t *testing.T) {
	l := sampleLead()
	id := l.ID.String()
	unknown := uuid.NewString()
	tests := []struct {
		name, method, path, body string
		wantStatus               int
		wantCode                 string
		wantField                string
	}{
		{"invalid json", "POST", "/api/leads", "{", 400, "invalid_json", ""},
		{"empty body", "POST", "/api/leads", "", 400, "invalid_json", ""},
		{"unknown field", "POST", "/api/leads", `{"company_name":"A","foo":1}`, 400, "invalid_json", ""},
		{"trailing data", "POST", "/api/leads", `{"company_name":"A"}{}`, 400, "invalid_json", ""},
		{"wrong type", "POST", "/api/leads", `{"company_name":42}`, 400, "validation_failed", "company_name"},
		{"validation", "POST", "/api/leads", `{"company_name":""}`, 400, "validation_failed", "company_name"},
		{"too large", "POST", "/api/leads", `{"company_name":"A","notes":"` + strings.Repeat("x", 70<<10) + `"}`, 413, "payload_too_large", ""},
		{"bad status param", "GET", "/api/leads?status=nope", "", 400, "validation_failed", "status"},
		{"non-integer limit", "GET", "/api/leads?limit=abc", "", 400, "validation_failed", "limit"},
		{"malformed id", "GET", "/api/leads/not-a-uuid", "", 404, "not_found", ""},
		{"unknown id", "GET", "/api/leads/" + unknown, "", 404, "not_found", ""},
		{"delete unknown", "DELETE", "/api/leads/" + unknown, "", 404, "not_found", ""},
		{"patch null status", "PATCH", "/api/leads/" + id, `{"status":null}`, 400, "validation_failed", "status"},
		{"patch wrong type", "PATCH", "/api/leads/" + id, `{"status":5}`, 400, "validation_failed", "status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, eb := do(t, &fakeRepo{lead: l}, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus || eb.Error.Code != tt.wantCode {
				t.Fatalf("got %d %q, want %d %q (body: %s)", rec.Code, eb.Error.Code, tt.wantStatus, tt.wantCode, rec.Body)
			}
			if tt.wantField != "" {
				if _, ok := eb.Error.Fields[tt.wantField]; !ok {
					t.Fatalf("expected field error on %q, got %v", tt.wantField, eb.Error.Fields)
				}
			}
		})
	}
}

func TestHandlerCreateAndPatch(t *testing.T) {
	repo := &fakeRepo{lead: sampleLead()}
	rec, _ := do(t, repo, "POST", "/api/leads", `{"company_name":" Acme ","website":null}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body)
	}
	if repo.created == nil || repo.created.CompanyName != "Acme" {
		t.Fatalf("create: repo got %+v", repo.created)
	}

	rec, _ = do(t, repo, "PATCH", "/api/leads/"+repo.lead.ID.String(), `{"status":"won","website":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: got %d: %s", rec.Code, rec.Body)
	}
	if len(repo.changes) != 2 || repo.changes[0].Column != "website" || repo.changes[0].Value != (*string)(nil) || repo.changes[1].Column != "status" {
		t.Fatalf("patch: changes = %+v", repo.changes)
	}
}
