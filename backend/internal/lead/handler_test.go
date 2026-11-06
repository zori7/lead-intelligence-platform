package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeRepo records calls; it is enough to exercise request decoding and error mapping.
type fakeRepo struct {
	created *Fields
	changes []Change
}

func (f *fakeRepo) List(context.Context, ListParams) ([]Lead, int, error) { return nil, 0, nil }
func (f *fakeRepo) Get(context.Context, uuid.UUID) (Lead, error)          { return Lead{}, ErrNotFound }
func (f *fakeRepo) Create(_ context.Context, fl Fields) (Lead, error) {
	f.created = &fl
	return Lead{ID: uuid.New(), CompanyName: fl.CompanyName, Status: fl.Status}, nil
}
func (f *fakeRepo) Update(_ context.Context, _ uuid.UUID, c []Change) (Lead, error) {
	f.changes = c
	return Lead{}, nil
}
func (f *fakeRepo) Delete(context.Context, uuid.UUID) error { return ErrNotFound }

type errorBody struct {
	Error struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
	} `json:"error"`
}

func do(t *testing.T, repo Repository, method, path, body string) (*httptest.ResponseRecorder, errorBody) {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/leads", NewHandler(repo).Routes)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var eb errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	return rec, eb
}

func TestHandlerErrors(t *testing.T) {
	id := uuid.NewString()
	tests := []struct {
		name, method, path, body string
		wantStatus               int
		wantCode                 string
	}{
		{"invalid json", "POST", "/api/leads", "{", 400, "invalid_json"},
		{"empty body", "POST", "/api/leads", "", 400, "invalid_json"},
		{"unknown field", "POST", "/api/leads", `{"company_name":"A","foo":1}`, 400, "invalid_json"},
		{"trailing data", "POST", "/api/leads", `{"company_name":"A"}{}`, 400, "invalid_json"},
		{"wrong type", "POST", "/api/leads", `{"company_name":42}`, 400, "validation_failed"},
		{"validation", "POST", "/api/leads", `{"company_name":""}`, 400, "validation_failed"},
		{"too large", "POST", "/api/leads", `{"company_name":"A","notes":"` + strings.Repeat("x", 70<<10) + `"}`, 413, "payload_too_large"},
		{"bad list param", "GET", "/api/leads?status=nope", "", 400, "validation_failed"},
		{"malformed id", "GET", "/api/leads/not-a-uuid", "", 404, "not_found"},
		{"unknown id", "GET", "/api/leads/" + id, "", 404, "not_found"},
		{"delete unknown", "DELETE", "/api/leads/" + id, "", 404, "not_found"},
		{"patch null status", "PATCH", "/api/leads/" + id, `{"status":null}`, 400, "validation_failed"},
		{"patch wrong type", "PATCH", "/api/leads/" + id, `{"status":5}`, 400, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, eb := do(t, &fakeRepo{}, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus || eb.Error.Code != tt.wantCode {
				t.Fatalf("got %d %q, want %d %q (body: %s)", rec.Code, eb.Error.Code, tt.wantStatus, tt.wantCode, rec.Body)
			}
		})
	}
}

func TestHandlerCreateAndPatch(t *testing.T) {
	repo := &fakeRepo{}
	rec, _ := do(t, repo, "POST", "/api/leads", `{"company_name":" Acme ","website":null}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body)
	}
	if repo.created == nil || repo.created.CompanyName != "Acme" {
		t.Fatalf("create: repo got %+v", repo.created)
	}

	rec, _ = do(t, repo, "PATCH", "/api/leads/"+uuid.NewString(), `{"status":"won","website":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: got %d: %s", rec.Code, rec.Body)
	}
	if len(repo.changes) != 2 || repo.changes[0].Column != "website" || repo.changes[0].Value != (*string)(nil) || repo.changes[1].Column != "status" {
		t.Fatalf("patch: changes = %+v", repo.changes)
	}
}
