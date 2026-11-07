package llmsettings

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
	"leadintel/backend/internal/httpx"
)

// Repository is the persistence the handler depends on (implemented by *Store).
type Repository interface {
	Get(ctx context.Context) (Settings, error)
	Replace(ctx context.Context, next Settings) (Settings, error)
}

// ModelLister lists the models the LLM server exposes (implemented by aiclient).
type ModelLister interface {
	ListModels(ctx context.Context) ([]string, error)
}

// listModelsTimeout bounds the model-list call made on save and on the admin page.
const listModelsTimeout = 3 * time.Second

// Handler serves the public config endpoint and the admin settings API.
// Admin routes are protected by httpx.RequireAdmin in cmd/api.
type Handler struct {
	api.Unimplemented
	repo   Repository
	models ModelLister
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(repo Repository, models ModelLister) *Handler {
	return &Handler{repo: repo, models: models}
}

// ---- Response shapes (LlmSettings in api/openapi.yaml) ------------------------

type settingResponse struct {
	Model       *string  `json:"model"`
	Temperature *float64 `json:"temperature"`
}

type nodeResponse struct {
	Node                 Node     `json:"node"`
	Model                *string  `json:"model"`
	Temperature          *float64 `json:"temperature"`
	EffectiveModel       *string  `json:"effective_model"`
	EffectiveTemperature float64  `json:"effective_temperature"`
}

type settingsResponse struct {
	Default    settingResponse `json:"default"`
	Nodes      []nodeResponse  `json:"nodes"`
	Configured bool            `json:"configured"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func toResponse(s Settings) settingsResponse {
	resolved, configured := Resolve(s)
	out := settingsResponse{
		Default:    settingResponse{Model: s.Default.Model, Temperature: s.Default.Temperature},
		Nodes:      make([]nodeResponse, 0, len(Nodes)),
		Configured: configured,
		UpdatedAt:  s.UpdatedAt,
	}
	defTemp := DefaultTemperature
	if s.Default.Temperature != nil {
		defTemp = *s.Default.Temperature
	}
	for _, n := range Nodes {
		row := s.Nodes[n]
		nr := nodeResponse{Node: n, Model: row.Model, Temperature: row.Temperature, EffectiveTemperature: defTemp}
		if row.Temperature != nil {
			nr.EffectiveTemperature = *row.Temperature
		}
		if configured {
			m := resolved[n].Model
			nr.EffectiveModel = &m
		}
		out.Nodes = append(out.Nodes, nr)
	}
	return out
}

// ---- Handlers ------------------------------------------------------------------

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	s, err := h.repo.Get(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, api.AppConfig{AiConfigured: s.Configured()})
}

func (h *Handler) GetLlmSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.repo.Get(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(s))
}

func (h *Handler) PutLlmSettings(w http.ResponseWriter, r *http.Request) {
	var req api.LlmSettingsUpdate
	if _, err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	next, err := validateUpdate(req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.checkModelsExist(r.Context(), next); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	prev, err := h.repo.Get(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	saved, err := h.repo.Replace(r.Context(), next)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	before, _ := Resolve(prev)
	after, _ := Resolve(saved)
	slog.InfoContext(r.Context(), "llm_settings_updated", "old", before, "new", after)
	httpx.WriteJSON(w, http.StatusOK, toResponse(saved))
}

func (h *Handler) ListLlmModels(w http.ResponseWriter, r *http.Request) {
	ids, err := h.listModels(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := api.ModelList{Items: make([]struct {
		Id string `json:"id"`
	}, 0, len(ids))}
	for _, id := range ids {
		resp.Items = append(resp.Items, struct {
			Id string `json:"id"`
		}{Id: id})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) listModels(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listModelsTimeout)
	defer cancel()
	ids, err := h.models.ListModels(ctx)
	if err != nil {
		slog.WarnContext(ctx, "list llm models failed", "err", err)
		return nil, ErrLLMUnreachable
	}
	return ids, nil
}

// checkModelsExist rejects models the LLM server does not list, so a typo
// cannot break every later analysis.
func (h *Handler) checkModelsExist(ctx context.Context, s Settings) error {
	available, err := h.listModels(ctx)
	if err != nil {
		return err
	}
	fields := apperr.Fields{}
	if !slices.Contains(available, *s.Default.Model) {
		fields.Add("default.model", "not available on LLM server")
	}
	for _, n := range Nodes {
		if m := s.Nodes[n].Model; m != nil && !slices.Contains(available, *m) {
			fields.Add("nodes."+string(n)+".model", "not available on LLM server")
		}
	}
	if len(fields) > 0 {
		return errUnknownModel(fields)
	}
	return nil
}

// ---- Validation (limits from the OpenAPI spec) ----------------------------------

var rules struct {
	modelMin, modelMax int
	tempMin, tempMax   float64
}

func init() {
	spec, err := api.GetSwagger()
	if err != nil {
		panic(fmt.Sprintf("llmsettings: load embedded OpenAPI spec: %v", err))
	}
	model := mustSchema(spec, "ModelName")
	temp := mustSchema(spec, "Temperature")
	if model.MaxLength == nil || temp.Min == nil || temp.Max == nil {
		panic("llmsettings: ModelName needs maxLength and Temperature needs minimum/maximum")
	}
	rules.modelMin, rules.modelMax = int(model.MinLength), int(*model.MaxLength)
	rules.tempMin, rules.tempMax = *temp.Min, *temp.Max
}

func mustSchema(spec *openapi3.T, name string) *openapi3.Schema {
	ref := spec.Components.Schemas[name]
	if ref == nil || ref.Value == nil {
		panic(fmt.Sprintf("llmsettings: OpenAPI schema %s not found", name))
	}
	return ref.Value
}

func checkModel(fe apperr.Fields, field, m string) {
	switch n := utf8.RuneCountInString(m); {
	case n < rules.modelMin:
		fe.Add(field, "must not be empty")
	case n > rules.modelMax:
		fe.Add(field, fmt.Sprintf("must be at most %d characters", rules.modelMax))
	}
}

func checkTemperature(fe apperr.Fields, field string, t float64) *float64 {
	if math.IsNaN(t) || t < rules.tempMin || t > rules.tempMax {
		fe.Add(field, fmt.Sprintf("must be between %g and %g", rules.tempMin, rules.tempMax))
	}
	// numeric(3,2) in the database: store what will be read back.
	v := math.Round(t*100) / 100
	return &v
}

func validateUpdate(req api.LlmSettingsUpdate) (Settings, error) {
	fe := apperr.Fields{}
	out := Settings{Nodes: make(map[Node]Setting, len(Nodes))}

	model := req.Default.Model
	if model == "" {
		fe.Add("default.model", "is required")
	} else {
		checkModel(fe, "default.model", model)
	}
	out.Default.Model = &model
	if !req.Default.Temperature.IsSpecified() {
		fe.Add("default.temperature", "is required (use null for the default of 0.2)")
	} else if t, err := req.Default.Temperature.Get(); err == nil {
		out.Default.Temperature = checkTemperature(fe, "default.temperature", float64(t))
	}

	updates := map[Node]api.LlmNodeUpdate{
		api.NodeExtractCompany:          req.Nodes.ExtractCompany,
		api.NodeAnalyzeFit:              req.Nodes.AnalyzeFit,
		api.NodeAnalyzeIntent:           req.Nodes.AnalyzeIntent,
		api.NodeGenerateRecommendations: req.Nodes.GenerateRecommendations,
	}
	for _, n := range Nodes {
		u, prefix := updates[n], "nodes."+string(n)
		// Full replace: every step must be present, with both fields (null inherits).
		if !u.Model.IsSpecified() || !u.Temperature.IsSpecified() {
			fe.Add(prefix, "is required with model and temperature (null inherits the default)")
			continue
		}
		var st Setting
		if m, err := u.Model.Get(); err == nil {
			checkModel(fe, prefix+".model", m)
			st.Model = &m
		}
		if t, err := u.Temperature.Get(); err == nil {
			st.Temperature = checkTemperature(fe, prefix+".temperature", float64(t))
		}
		out.Nodes[n] = st
	}
	if err := fe.Err(); err != nil {
		return Settings{}, err
	}
	return out, nil
}
