// Package llmsettings owns the runtime LLM configuration: a default model and
// optional per-step overrides, edited on the admin page and resolved for every
// analysis job when it is claimed (SPEC.md §6.3, §7.5).
package llmsettings

import (
	"time"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

// Node names an LLM workflow step. The enum comes from the OpenAPI spec.
type Node = api.LlmNode

// Nodes lists the LLM steps in workflow order.
var Nodes = []Node{
	api.NodeExtractCompany,
	api.NodeAnalyzeFit,
	api.NodeAnalyzeIntent,
	api.NodeGenerateRecommendations,
}

// DefaultTemperature applies when the default row has no temperature.
const DefaultTemperature = 0.2

// Setting is one row of llm_node_settings. Nil means "inherit" for a node and
// "not set" for the default.
type Setting struct {
	Model       *string
	Temperature *float64
}

// Settings is the whole configuration.
type Settings struct {
	Default   Setting
	Nodes     map[Node]Setting
	UpdatedAt time.Time
}

// NodeConfig is the effective model and temperature of one step.
type NodeConfig struct {
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
}

// Resolved maps every LLM step to its effective config. Its JSON form is
// stored on each job as model_config.
type Resolved map[Node]NodeConfig

// Configured reports whether a default model is set; analyses are blocked until it is.
func (s Settings) Configured() bool { return s.Default.Model != nil }

// Resolve computes `effective = node override ?? default` for model and
// temperature of every LLM step. It returns false when no default model is set.
func Resolve(s Settings) (Resolved, bool) {
	if !s.Configured() {
		return nil, false
	}
	defTemp := DefaultTemperature
	if s.Default.Temperature != nil {
		defTemp = *s.Default.Temperature
	}
	out := make(Resolved, len(Nodes))
	for _, n := range Nodes {
		cfg := NodeConfig{Model: *s.Default.Model, Temperature: defTemp}
		if o := s.Nodes[n]; o.Model != nil {
			cfg.Model = *o.Model
		}
		if o := s.Nodes[n]; o.Temperature != nil {
			cfg.Temperature = *o.Temperature
		}
		out[n] = cfg
	}
	return out, true
}

var (
	// ErrNotConfigured blocks analyses until an admin picks a default model.
	ErrNotConfigured = &apperr.CodedError{
		Kind:    apperr.ErrConflict,
		Code:    "ai_not_configured",
		Message: "AI is not configured. An admin must choose a default model on the admin page.",
	}
	// ErrLLMUnreachable means the model list could not be fetched; nothing was saved.
	ErrLLMUnreachable = &apperr.CodedError{
		Kind:    apperr.ErrUnavailable,
		Code:    "llm_unreachable",
		Message: "The LLM server could not be reached.",
	}
)

func errUnknownModel(fields map[string]string) error {
	return &apperr.CodedError{
		Kind:    apperr.ErrUnprocessable,
		Code:    "unknown_model",
		Message: "A model is not available on the LLM server.",
		Fields:  fields,
	}
}
