package llmsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apitest"
	"leadintel/backend/internal/httpx"
)

func ptr[T any](v T) *T { return &v }

// resolveCasesFile is shared with the frontend preview test
// (frontend/src/lib/schemas/llm-settings.test.ts), so the client-side
// effective values follow the same rule as Resolve.
const resolveCasesFile = "../../../api/testdata/llm-resolve-cases.json"

func TestResolve(t *testing.T) {
	raw, err := os.ReadFile(resolveCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	type setting struct {
		Model       *string  `json:"model"`
		Temperature *float64 `json:"temperature"`
	}
	var file struct {
		Cases []struct {
			Name     string `json:"name"`
			Settings struct {
				Default setting          `json:"default"`
				Nodes   map[Node]setting `json:"nodes"`
			} `json:"settings"`
			Want Resolved `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, tt := range file.Cases {
		t.Run(tt.Name, func(t *testing.T) {
			s := Settings{
				Default: Setting(tt.Settings.Default),
				Nodes:   map[Node]Setting{},
			}
			for n, v := range tt.Settings.Nodes {
				s.Nodes[n] = Setting(v)
			}
			got, ok := Resolve(s)
			wantConfigured := tt.Want != nil
			if ok != wantConfigured || !reflect.DeepEqual(got, tt.Want) {
				t.Fatalf("Resolve = %v, %v; want %v, %v", got, ok, tt.Want, wantConfigured)
			}
		})
	}
}

// ---- Handler --------------------------------------------------------------------

type fakeRepo struct {
	s     Settings
	saved *Settings
}

func (f *fakeRepo) Get(context.Context) (Settings, error) { return f.s, nil }
func (f *fakeRepo) Replace(_ context.Context, next Settings) (Settings, error) {
	next.UpdatedAt = time.Now().UTC()
	f.saved, f.s = &next, next
	return next, nil
}

type fakeLister struct {
	ids []string
	err error
}

func (f fakeLister) ListModels(context.Context) ([]string, error) { return f.ids, f.err }

func newRouter(repo Repository, lister ModelLister) http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(httpx.RequireAdmin("test-admin-token-0123456789", "/api/admin/"))
		api.HandlerWithOptions(NewHandler(repo, lister), api.ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: httpx.ParamError})
	})
	return r
}

func do(h http.Handler, method, path, body string, auth bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth {
		req.Header.Set("Authorization", "Bearer test-admin-token-0123456789")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const validBody = `{
  "default": {"model": "base", "temperature": 0.2},
  "nodes": {
    "extract_company": {"model": "big", "temperature": null},
    "analyze_fit": {"model": null, "temperature": null},
    "analyze_intent": {"model": null, "temperature": 0.4},
    "generate_recommendations": {"model": null, "temperature": 0.6}
  }
}`

func TestPutSettings(t *testing.T) {
	models := fakeLister{ids: []string{"base", "big"}}
	tests := []struct {
		name       string
		body       string
		lister     ModelLister
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"valid", validBody, models, 200, "", ""},
		{"unknown default model", strings.Replace(validBody, `"base"`, `"typo"`, 1), models, 422, "unknown_model", "default.model"},
		{"unknown node model", strings.Replace(validBody, `"big"`, `"nope"`, 1), models, 422, "unknown_model", "nodes.extract_company.model"},
		{"llm unreachable", validBody, fakeLister{err: errors.New("down")}, 503, "llm_unreachable", ""},
		{"missing default model", strings.Replace(validBody, `"model": "base", `, ``, 1), models, 400, "validation_failed", "default.model"},
		{"empty default model", strings.Replace(validBody, `"model": "base"`, `"model": ""`, 1), models, 400, "validation_failed", "default.model"},
		{"temperature out of range", strings.Replace(validBody, `0.6`, `2.5`, 1), models, 400, "validation_failed", "nodes.generate_recommendations.temperature"},
		{"missing node", strings.Replace(validBody, `"analyze_fit": {"model": null, "temperature": null},`, ``, 1), models, 400, "validation_failed", "nodes.analyze_fit"},
		{"node without temperature key", strings.Replace(validBody, `"analyze_fit": {"model": null, "temperature": null}`, `"analyze_fit": {"model": null}`, 1), models, 400, "validation_failed", "nodes.analyze_fit"},
		{"unknown node", strings.Replace(validBody, `"nodes": {`, `"nodes": {"summarize": {"model": null, "temperature": null},`, 1), models, 400, "invalid_json", ""},
		{"too long model", strings.Replace(validBody, `"big"`, `"`+strings.Repeat("m", 201)+`"`, 1), models, 400, "validation_failed", "nodes.extract_company.model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			rec := do(newRouter(repo, tt.lister), "PUT", "/api/admin/llm-settings", tt.body, true)
			apitest.CheckResponse(t, "PUT", "/admin/llm-settings", rec)

			var eb struct {
				Error struct {
					Code   string            `json:"code"`
					Fields map[string]string `json:"fields"`
				} `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &eb)
			if rec.Code != tt.wantStatus || eb.Error.Code != tt.wantCode {
				t.Fatalf("got %d %q, want %d %q (body %s)", rec.Code, eb.Error.Code, tt.wantStatus, tt.wantCode, rec.Body)
			}
			if tt.wantField != "" {
				if _, ok := eb.Error.Fields[tt.wantField]; !ok {
					t.Fatalf("missing field error %q in %v", tt.wantField, eb.Error.Fields)
				}
			}
			if (repo.saved != nil) != (tt.wantStatus == 200) {
				t.Fatalf("saved = %v, want saved only on success", repo.saved != nil)
			}
		})
	}
}

func TestPutSettingsStoresWhatItValidated(t *testing.T) {
	repo := &fakeRepo{}
	rec := do(newRouter(repo, fakeLister{ids: []string{"base", "big"}}), "PUT", "/api/admin/llm-settings", validBody, true)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	got, _ := Resolve(*repo.saved)
	want := Resolved{
		api.NodeExtractCompany:          {"big", 0.2},
		api.NodeAnalyzeFit:              {"base", 0.2},
		api.NodeAnalyzeIntent:           {"base", 0.4},
		api.NodeGenerateRecommendations: {"base", 0.6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved %v, want %v", got, want)
	}
}

func TestAdminRoutesRequireToken(t *testing.T) {
	h := newRouter(&fakeRepo{}, fakeLister{})
	for _, tc := range []struct{ method, path, specPath string }{
		{"GET", "/api/admin/llm-settings", "/admin/llm-settings"},
		{"PUT", "/api/admin/llm-settings", "/admin/llm-settings"},
		{"GET", "/api/admin/llm-models", "/admin/llm-models"},
	} {
		rec := do(h, tc.method, tc.path, validBody, false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: %d", tc.method, tc.path, rec.Code)
		}
		apitest.CheckResponse(t, tc.method, tc.specPath, rec)
	}
	// The public config endpoint needs no token.
	if rec := do(h, "GET", "/api/config", "", false); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rec.Code)
	}
}

func TestResponsesMatchSpec(t *testing.T) {
	configured := Settings{
		Default:   Setting{Model: ptr("base"), Temperature: ptr(0.3)},
		Nodes:     map[Node]Setting{api.NodeAnalyzeFit: {Model: ptr("big"), Temperature: ptr(1.0)}},
		UpdatedAt: time.Now().UTC(),
	}
	for name, s := range map[string]Settings{"unconfigured": {UpdatedAt: time.Now().UTC()}, "configured": configured} {
		t.Run(name, func(t *testing.T) {
			h := newRouter(&fakeRepo{s: s}, fakeLister{ids: []string{"base", "big"}})
			for _, tc := range []struct{ method, path, specPath string }{
				{"GET", "/api/config", "/config"},
				{"GET", "/api/admin/llm-settings", "/admin/llm-settings"},
				{"GET", "/api/admin/llm-models", "/admin/llm-models"},
			} {
				rec := do(h, tc.method, tc.path, "", true)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
				}
				apitest.CheckResponse(t, tc.method, tc.specPath, rec)
			}
		})
	}
	rec := do(newRouter(&fakeRepo{}, fakeLister{err: errors.New("down")}), "GET", "/api/admin/llm-models", "", true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("models with LLM down: %d", rec.Code)
	}
	apitest.CheckResponse(t, "GET", "/admin/llm-models", rec)
}
