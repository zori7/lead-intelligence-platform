// API types. The wire types are generated from api/openapi.yaml into ./gen
// (run `make openapi`); this module re-exports them and adds frontend-only types.
import { zCompanySize, zLeadSort, zLeadStatus, zLlmNode } from './gen/zod.gen.js';
import type { JobStatus, LeadStatus, LeadSort } from './gen/types.gen.js';

export type {
	AnalysisJob,
	AnalysisJobList,
	AnalysisProgressEvent,
	AnalysisResult,
	AnalysisStep,
	AppConfig,
	CompanyInsights,
	CompanySize,
	FitAnalysis,
	IntentAnalysis,
	JobStatus,
	LeadScore,
	Priority,
	Recommendation,
	ResultMeta,
	StepName,
	StepState,
	WebsiteFetch,
	Lead,
	LeadCreate,
	LeadDetail,
	LeadList,
	LeadPatch,
	LeadStats,
	LeadSort,
	LeadStatus,
	LlmDefaultSetting,
	LlmNode,
	LlmNodeSetting,
	LlmNodeUpdate,
	LlmSettings,
	LlmSettingsUpdate,
	ModelList,
	Tier
} from './gen/types.gen.js';

// Enum values, derived from the generated zod enums (and so from the spec).
export const LEAD_STATUSES = zLeadStatus.options;
export const COMPANY_SIZES = zCompanySize.options;
export const LEAD_SORTS = zLeadSort.unwrap().options;
/** The LLM workflow steps, in workflow order. */
export const LLM_NODES = zLlmNode.options;

/** Lead list filters as held in the URL (frontend-only). */
export interface LeadFilters {
	status: LeadStatus | null;
	q: string;
	sort: LeadSort;
	limit: number;
	offset: number;
}

/** Queued and running jobs are active: they can still change. */
export const isActiveJob = (status: JobStatus) => status === 'queued' || status === 'running';
