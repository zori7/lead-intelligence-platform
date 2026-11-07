package aiclient

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	analysisv1 "leadintel/backend/gen/analysis/v1"
	"leadintel/backend/internal/analysis"
	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apitest"
	"leadintel/backend/internal/llmsettings"
)

func fullResult() *analysisv1.AnalysisResult {
	return &analysisv1.AnalysisResult{
		Summary: "Strong fit.",
		Company: &analysisv1.CompanyInsights{
			Name: "Acme", Industry: "SaaS", SizeEstimate: "51-200", BusinessModel: "Subscriptions",
			Description: "Software.", KeyNeeds: []string{"Growth"}, Confidence: 0.8,
		},
		Fit:    &analysisv1.FitAnalysis{Score: 80, Rationale: "Fits.", MatchedCriteria: []string{"B2B"}},
		Intent: &analysisv1.IntentAnalysis{Score: 55, Rationale: "Hiring.", Signals: []string{"Hiring"}},
		Score: &analysisv1.LeadScore{
			Value: 70, Tier: analysisv1.Tier_TIER_HOT, Confidence: 0.8,
			Breakdown: &analysisv1.ScoreBreakdown{Fit: proto.Uint32(80), Intent: proto.Uint32(55)},
		},
		Recommendations: []*analysisv1.Recommendation{{Action: "Call", Rationale: "Now", Priority: analysisv1.Priority_PRIORITY_HIGH}},
		Meta: &analysisv1.ResultMeta{
			NodeModels: map[string]string{"extract_company": "m"}, DurationMs: 1200, InputTokens: 10, OutputTokens: 5,
			Website: &analysisv1.WebsiteFetch{Status: analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_FETCHED, FinalUrl: "https://acme.example/"},
		},
	}
}

// partialResult has a failed intent branch: nullable fields must become null.
func partialResult() *analysisv1.AnalysisResult {
	r := fullResult()
	r.Intent = nil
	r.Score.Breakdown.Intent = nil
	r.Meta.Partial = true
	r.Meta.Warnings = []string{"intent unavailable"}
	r.Meta.Website = &analysisv1.WebsiteFetch{Status: analysisv1.WebsiteFetchStatus_WEBSITE_FETCH_STATUS_BLOCKED}
	r.Meta.NodeModels = nil
	return r
}

func TestResultMappingMatchesSpec(t *testing.T) {
	for name, r := range map[string]*analysisv1.AnalysisResult{"full": fullResult(), "partial": partialResult()} {
		t.Run(name, func(t *testing.T) {
			got, err := fromResult(r)
			if err != nil {
				t.Fatal(err)
			}
			apitest.CheckSchema(t, "AnalysisResult", got)
		})
	}
	got, _ := fromResult(partialResult())
	if !got.Intent.IsNull() || !got.Score.Breakdown.Intent.IsNull() || !got.Meta.Website.FinalUrl.IsNull() {
		t.Fatal("missing branch must map to null")
	}
	if got.Meta.Website.Status != api.WebsiteBlocked || got.Score.Tier != api.TierHot {
		t.Fatalf("enum mapping: %+v", got)
	}
}

func TestResultMappingRejectsUnknownEnums(t *testing.T) {
	r := fullResult()
	r.Score.Tier = analysisv1.Tier_TIER_UNSPECIFIED
	if _, err := fromResult(r); err == nil {
		t.Fatal("unspecified tier accepted")
	}
	if _, err := fromResult(&analysisv1.AnalysisResult{}); err == nil {
		t.Fatal("empty result accepted")
	}
}

func TestEventMapping(t *testing.T) {
	ev, err := fromEvent(&analysisv1.AnalysisEvent{Payload: &analysisv1.AnalysisEvent_Progress{Progress: &analysisv1.StepProgress{
		Step: analysisv1.Step_STEP_FETCH_WEBSITE, State: analysisv1.StepState_STEP_STATE_SKIPPED, Percent: 25,
	}}})
	if err != nil || ev.Progress.Step != api.StepFetchWebsite || ev.Progress.State != api.StepSkipped || ev.Progress.Percent != 25 {
		t.Fatalf("progress: %+v %v", ev.Progress, err)
	}
	ev, err = fromEvent(&analysisv1.AnalysisEvent{Payload: &analysisv1.AnalysisEvent_Failed{Failed: &analysisv1.AnalysisFailed{
		Code: analysisv1.ErrorCode_ERROR_CODE_PROVIDER_CONFIG, Message: "Fix settings.",
	}}})
	if err != nil || ev.Failed.Code != "provider_config" || ev.Failed.Retryable {
		t.Fatalf("failed: %+v %v", ev.Failed, err)
	}
	if _, err := fromEvent(&analysisv1.AnalysisEvent{}); err == nil {
		t.Fatal("event without payload accepted")
	}
}

func TestToRequest(t *testing.T) {
	models := llmsettings.Resolved{}
	for _, n := range llmsettings.Nodes {
		models[n] = llmsettings.NodeConfig{Model: "m-" + string(n), Temperature: 0.3}
	}
	req, err := toRequest(analysis.Request{JobID: uuid.New(), Lead: analysis.LeadInput{CompanyName: "Acme"}, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.GetModels()) != 4 || req.GetModels()[0].GetStep() != analysisv1.Step_STEP_EXTRACT_COMPANY ||
		req.GetModels()[0].GetModel() != "m-extract_company" {
		t.Fatalf("models: %v", req.GetModels())
	}
	delete(models, api.NodeAnalyzeFit)
	if _, err := toRequest(analysis.Request{Models: models}); err == nil {
		t.Fatal("incomplete model config accepted")
	}
}

func TestMapStatus(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		code codes.Code
		want error
	}{
		{codes.Unavailable, analysis.ErrAIUnavailable},
		{codes.ResourceExhausted, analysis.ErrAIUnavailable},
		{codes.DeadlineExceeded, context.DeadlineExceeded},
		{codes.Internal, analysis.ErrAIProtocol},
		{codes.AlreadyExists, analysis.ErrAIProtocol},
	}
	for _, tt := range tests {
		if got := mapStatus(ctx, status.Error(tt.code, "x")); !errors.Is(got, tt.want) {
			t.Fatalf("%s → %v, want %v", tt.code, got, tt.want)
		}
	}
}
