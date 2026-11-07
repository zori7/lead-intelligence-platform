// Package aiclient is the thin gRPC client of the Python AI service. It maps
// between protobuf messages and the domain types of the analysis and
// llmsettings packages, so neither of them depends on generated proto code.
package aiclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/oapi-codegen/nullable"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	analysisv1 "leadintel/backend/gen/analysis/v1"
	"leadintel/backend/internal/analysis"
	"leadintel/backend/internal/api"
	"leadintel/backend/internal/llmsettings"
)

// serviceName is the name the AI service reports health under (proto/…/analysis.proto).
const serviceName = "leadintel.analysis.v1.AnalysisService"

// ErrNotServing means the AI service answered the health check but is not serving.
var ErrNotServing = errors.New("ai service not serving")

// Client talks to the AI service over one multiplexed connection.
type Client struct {
	conn   *grpc.ClientConn
	svc    analysisv1.AnalysisServiceClient
	health healthpb.HealthClient
}

// New creates a client for addr. The connection is established lazily, so the
// API starts even when the AI service is not up yet. Plaintext is used inside
// the compose network (SPEC.md §12).
func New(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("create ai client: %w", err)
	}
	return &Client{conn: conn, svc: analysisv1.NewAnalysisServiceClient(conn), health: healthpb.NewHealthClient(conn)}, nil
}

// Health runs the standard gRPC health check for the analysis service. The
// caller bounds it with a deadline.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.health.Check(ctx, &healthpb.HealthCheckRequest{Service: serviceName})
	if err != nil {
		return fmt.Errorf("ai health check: %w", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("%w: %s", ErrNotServing, resp.GetStatus())
	}
	return nil
}

func (c *Client) Close() error { return c.conn.Close() }

var (
	_ analysis.Analyzer       = (*Client)(nil)
	_ llmsettings.ModelLister = (*Client)(nil)
)

// Analyze runs AnalyzeLead and forwards every event to handle.
func (c *Client) Analyze(ctx context.Context, req analysis.Request, handle func(analysis.Event) error) error {
	pbReq, err := toRequest(req)
	if err != nil {
		return fmt.Errorf("%w: %v", analysis.ErrAIProtocol, err)
	}
	md := []string{"x-job-id", req.JobID.String()}
	if rid := middleware.GetReqID(ctx); rid != "" {
		md = append(md, "x-request-id", rid)
	}
	stream, err := c.svc.AnalyzeLead(metadata.AppendToOutgoingContext(ctx, md...), pbReq)
	if err != nil {
		return mapStatus(ctx, err)
	}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return mapStatus(ctx, err)
		}
		ev, err := fromEvent(msg)
		if err != nil {
			return fmt.Errorf("%w: %v", analysis.ErrAIProtocol, err)
		}
		if err := handle(ev); err != nil {
			return fmt.Errorf("handle analysis event: %w", err)
		}
	}
}

// ListModels returns the model IDs the LLM server exposes.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	resp, err := c.svc.ListModels(ctx, &analysisv1.ListModelsRequest{})
	if err != nil {
		return nil, mapStatus(ctx, err)
	}
	ids := make([]string, 0, len(resp.GetModels()))
	for _, m := range resp.GetModels() {
		ids = append(ids, m.GetId())
	}
	return ids, nil
}

// mapStatus turns a gRPC status into the error kinds the worker understands.
func mapStatus(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted:
		return fmt.Errorf("%w: %v", analysis.ErrAIUnavailable, err)
	case codes.DeadlineExceeded:
		return context.DeadlineExceeded
	case codes.Canceled:
		return context.Canceled
	default:
		return fmt.Errorf("%w: %v", analysis.ErrAIProtocol, err)
	}
}

// ---- Mapping: domain → proto ----------------------------------------------------

var nodeToStep = map[llmsettings.Node]analysisv1.Step{
	api.NodeExtractCompany:          analysisv1.Step_STEP_EXTRACT_COMPANY,
	api.NodeAnalyzeFit:              analysisv1.Step_STEP_ANALYZE_FIT,
	api.NodeAnalyzeIntent:           analysisv1.Step_STEP_ANALYZE_INTENT,
	api.NodeGenerateRecommendations: analysisv1.Step_STEP_GENERATE_RECOMMENDATIONS,
}

func toRequest(req analysis.Request) (*analysisv1.AnalyzeLeadRequest, error) {
	models := make([]*analysisv1.NodeModelConfig, 0, len(llmsettings.Nodes))
	for _, n := range llmsettings.Nodes {
		cfg, ok := req.Models[n]
		if !ok {
			return nil, fmt.Errorf("model config has no entry for %s", n)
		}
		models = append(models, &analysisv1.NodeModelConfig{
			Step: nodeToStep[n], Model: cfg.Model, Temperature: float32(cfg.Temperature),
		})
	}
	l := req.Lead
	return &analysisv1.AnalyzeLeadRequest{
		JobId: req.JobID.String(),
		Lead: &analysisv1.LeadInput{
			CompanyName:  l.CompanyName,
			Website:      l.Website,
			ContactName:  l.ContactName,
			ContactTitle: l.ContactTitle,
			Industry:     l.Industry,
			CompanySize:  l.CompanySize,
			Notes:        l.Notes,
		},
		Models: models,
	}, nil
}

// ---- Mapping: proto → domain ----------------------------------------------------

var stepNames = map[analysisv1.Step]api.StepName{
	analysisv1.Step_STEP_VALIDATE_INPUT:           api.StepValidateInput,
	analysisv1.Step_STEP_FETCH_WEBSITE:            api.StepFetchWebsite,
	analysisv1.Step_STEP_EXTRACT_COMPANY:          api.StepExtractCompany,
	analysisv1.Step_STEP_ANALYZE_FIT:              api.StepAnalyzeFit,
	analysisv1.Step_STEP_ANALYZE_INTENT:           api.StepAnalyzeIntent,
	analysisv1.Step_STEP_SCORE_LEAD:               api.StepScoreLead,
	analysisv1.Step_STEP_GENERATE_RECOMMENDATIONS: api.StepGenerateRecommendations,
	analysisv1.Step_STEP_AGGREGATE:                api.StepAggregate,
}

var stepStates = map[analysisv1.StepState]api.StepState{
	analysisv1.StepState_STEP_STATE_RUNNING: api.StepRunning,
	analysisv1.StepState_STEP_STATE_DONE:    api.StepDone,
	analysisv1.StepState_STEP_STATE_SKIPPED: api.StepSkipped,
	analysisv1.StepState_STEP_STATE_FAILED:  api.StepFailed,
}

var tiers = map[analysisv1.Tier]api.Tier{
	analysisv1.Tier_TIER_HOT:  api.TierHot,
	analysisv1.Tier_TIER_WARM: api.TierWarm,
	analysisv1.Tier_TIER_COLD: api.TierCold,
}

var priorities = map[analysisv1.Priority]api.Priority{
	analysisv1.Priority_PRIORITY_HIGH:   api.PriorityHigh,
	analysisv1.Priority_PRIORITY_MEDIUM: api.PriorityMedium,
	analysisv1.Priority_PRIORITY_LOW:    api.PriorityLow,
}

var websiteStatuses = map[analysisv1.WebsiteFetchStatus]api.WebsiteFetchStatus{
	analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_FETCHED:      api.WebsiteFetched,
	analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_NOT_PROVIDED: api.WebsiteNotProvided,
	analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_DISABLED:     api.WebsiteDisabled,
	analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_BLOCKED:      api.WebsiteBlocked,
	analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_FAILED:       api.WebsiteFailed,
}

func lookup[K comparable, V any](m map[K]V, k K, what string) (V, error) {
	v, ok := m[k]
	if !ok {
		var zero V
		return zero, fmt.Errorf("unknown %s %v", what, k)
	}
	return v, nil
}

func fromEvent(msg *analysisv1.AnalysisEvent) (analysis.Event, error) {
	switch p := msg.GetPayload().(type) {
	case *analysisv1.AnalysisEvent_Progress:
		step, err := lookup(stepNames, p.Progress.GetStep(), "step")
		if err != nil {
			return analysis.Event{}, err
		}
		state, err := lookup(stepStates, p.Progress.GetState(), "step state")
		if err != nil {
			return analysis.Event{}, err
		}
		return analysis.Event{Progress: &analysis.Progress{
			Step: step, State: state, Percent: min(100, int(p.Progress.GetPercent())),
		}}, nil
	case *analysisv1.AnalysisEvent_Completed:
		r, err := fromResult(p.Completed.GetResult())
		if err != nil {
			return analysis.Event{}, err
		}
		return analysis.Event{Completed: r}, nil
	case *analysisv1.AnalysisEvent_Failed:
		return analysis.Event{Failed: &analysis.Failure{
			Code:      errorCode(p.Failed.GetCode()),
			Message:   p.Failed.GetMessage(),
			Retryable: p.Failed.GetRetryable(),
		}}, nil
	default:
		return analysis.Event{}, errors.New("event without payload")
	}
}

// errorCode maps ERROR_CODE_PROVIDER_ERROR → "provider_error".
func errorCode(c analysisv1.ErrorCode) string {
	if c == analysisv1.ErrorCode_ERROR_CODE_UNSPECIFIED {
		return analysis.CodeInternal
	}
	return strings.ToLower(strings.TrimPrefix(c.String(), "ERROR_CODE_"))
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func fromResult(r *analysisv1.AnalysisResult) (*api.AnalysisResult, error) {
	if r == nil || r.GetCompany() == nil || r.GetScore() == nil || r.GetMeta() == nil {
		return nil, errors.New("incomplete analysis result")
	}
	tier, err := lookup(tiers, r.GetScore().GetTier(), "tier")
	if err != nil {
		return nil, err
	}
	meta := r.GetMeta()
	website, err := lookup(websiteStatuses, meta.GetWebsite().GetStatus(), "website status")
	if err != nil {
		return nil, err
	}

	c := r.GetCompany()
	out := &api.AnalysisResult{
		Summary: r.GetSummary(),
		Company: api.CompanyInsights{
			Name:          c.GetName(),
			Industry:      c.GetIndustry(),
			SizeEstimate:  c.GetSizeEstimate(),
			BusinessModel: c.GetBusinessModel(),
			Description:   c.GetDescription(),
			KeyNeeds:      orEmpty(c.GetKeyNeeds()),
			Confidence:    c.GetConfidence(),
		},
		Recommendations: make([]api.Recommendation, 0, len(r.GetRecommendations())),
		Meta: api.ResultMeta{
			NodeModels:   meta.GetNodeModels(),
			Partial:      meta.GetPartial(),
			Warnings:     orEmpty(meta.GetWarnings()),
			DurationMs:   int(meta.GetDurationMs()),
			InputTokens:  int(meta.GetInputTokens()),
			OutputTokens: int(meta.GetOutputTokens()),
			Website:      api.WebsiteFetch{Status: website, FinalUrl: nullable.NewNullNullable[string]()},
		},
	}
	if out.Meta.NodeModels == nil {
		out.Meta.NodeModels = map[string]string{}
	}
	if u := meta.GetWebsite().GetFinalUrl(); u != "" {
		out.Meta.Website.FinalUrl = nullable.NewNullableWithValue(u)
	}

	out.Fit = nullable.NewNullNullable[api.FitAnalysis]()
	if f := r.GetFit(); f != nil {
		out.Fit = nullable.NewNullableWithValue(api.FitAnalysis{
			Score: int(f.GetScore()), Rationale: f.GetRationale(),
			MatchedCriteria: orEmpty(f.GetMatchedCriteria()), Gaps: orEmpty(f.GetGaps()),
		})
	}
	out.Intent = nullable.NewNullNullable[api.IntentAnalysis]()
	if i := r.GetIntent(); i != nil {
		out.Intent = nullable.NewNullableWithValue(api.IntentAnalysis{
			Score: int(i.GetScore()), Rationale: i.GetRationale(), Signals: orEmpty(i.GetSignals()),
		})
	}

	s := r.GetScore()
	out.Score = api.LeadScore{Value: int(s.GetValue()), Tier: tier, Confidence: s.GetConfidence()}
	out.Score.Breakdown.Fit = nullable.NewNullNullable[int]()
	out.Score.Breakdown.Intent = nullable.NewNullNullable[int]()
	if b := s.GetBreakdown(); b != nil {
		if b.Fit != nil {
			out.Score.Breakdown.Fit = nullable.NewNullableWithValue(int(b.GetFit()))
		}
		if b.Intent != nil {
			out.Score.Breakdown.Intent = nullable.NewNullableWithValue(int(b.GetIntent()))
		}
	}

	for _, rec := range r.GetRecommendations() {
		p, err := lookup(priorities, rec.GetPriority(), "priority")
		if err != nil {
			return nil, err
		}
		out.Recommendations = append(out.Recommendations, api.Recommendation{
			Action: rec.GetAction(), Rationale: rec.GetRationale(), Priority: p,
		})
	}
	return out, nil
}
