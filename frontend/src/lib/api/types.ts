// API DTO types — mirror of SPEC §7.2. Keep in sync with backend/internal/lead/model.go.

export const LEAD_STATUSES = ['new', 'contacted', 'qualified', 'proposal', 'won', 'lost'] as const;
export type LeadStatus = (typeof LEAD_STATUSES)[number];

export const COMPANY_SIZES = ['1-10', '11-50', '51-200', '201-1000', '1000+'] as const;
export type CompanySize = (typeof COMPANY_SIZES)[number];

export type Tier = 'hot' | 'warm' | 'cold';

export interface Lead {
	id: string;
	company_name: string;
	website: string | null;
	contact_name: string | null;
	contact_email: string | null;
	contact_title: string | null;
	industry: string | null;
	company_size: CompanySize | null;
	notes: string | null;
	status: LeadStatus;
	ai_score: number | null;
	ai_tier: Tier | null;
	last_analyzed_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface LeadDetail {
	lead: Lead;
	// Typed as AnalysisJob once analyses exist.
	latest_analysis: null;
}

export interface LeadList {
	items: Lead[];
	total: number;
}

/** Editable fields, as sent on create. */
export interface LeadInput {
	company_name: string;
	website: string | null;
	contact_name: string | null;
	contact_email: string | null;
	contact_title: string | null;
	industry: string | null;
	company_size: CompanySize | null;
	notes: string | null;
	status: LeadStatus;
}

/** Partial update: absent = unchanged, null = clear. */
export type LeadPatch = Partial<LeadInput>;

export const LEAD_SORTS = ['created_desc', 'score_desc'] as const;
export type LeadSort = (typeof LEAD_SORTS)[number];

export interface LeadFilters {
	status: LeadStatus | null;
	q: string;
	sort: LeadSort;
	limit: number;
	offset: number;
}
