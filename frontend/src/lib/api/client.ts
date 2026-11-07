import type {
	AnalysisJob,
	AnalysisJobList,
	AppConfig,
	Lead,
	LeadDetail,
	LeadFilters,
	LeadCreate,
	LeadList,
	LeadPatch
} from './types.js';

/** A non-2xx API response (or a network failure, with status 0). */
export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly fields: Record<string, string>;

	constructor(status: number, code: string, message: string, fields: Record<string, string> = {}) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
		this.fields = fields;
	}
}

interface ErrorEnvelope {
	error?: { code?: string; message?: string; fields?: Record<string, string> };
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	let res: Response;
	try {
		res = await fetch(path, {
			method,
			headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body)
		});
	} catch {
		throw new ApiError(0, 'network_error', 'Cannot reach the server. Check your connection.');
	}

	if (res.status === 204) return undefined as T;

	const data: unknown = await res.json().catch(() => null);
	if (!res.ok) {
		const e = (data as ErrorEnvelope | null)?.error;
		throw new ApiError(
			res.status,
			e?.code ?? 'unknown',
			e?.message ?? `Request failed (${res.status}).`,
			e?.fields ?? {}
		);
	}
	return data as T;
}

function listQuery(f: LeadFilters): string {
	const p = new URLSearchParams();
	if (f.status) p.set('status', f.status);
	if (f.q) p.set('q', f.q);
	p.set('sort', f.sort);
	p.set('limit', String(f.limit));
	p.set('offset', String(f.offset));
	return p.toString();
}

export const api = {
	listLeads: (filters: LeadFilters) => request<LeadList>('GET', `/api/leads?${listQuery(filters)}`),
	getLead: (id: string) => request<LeadDetail>('GET', `/api/leads/${encodeURIComponent(id)}`),
	createLead: (input: LeadCreate) => request<Lead>('POST', '/api/leads', input),
	updateLead: (id: string, patch: LeadPatch) =>
		request<Lead>('PATCH', `/api/leads/${encodeURIComponent(id)}`, patch),
	deleteLead: (id: string) => request<void>('DELETE', `/api/leads/${encodeURIComponent(id)}`),
	triggerAnalysis: (leadId: string) =>
		request<AnalysisJob>('POST', `/api/leads/${encodeURIComponent(leadId)}/analyses`),
	listAnalyses: (leadId: string, limit: number) =>
		request<AnalysisJobList>(
			'GET',
			`/api/leads/${encodeURIComponent(leadId)}/analyses?limit=${limit}`
		),
	getAnalysis: (id: string) =>
		request<AnalysisJob>('GET', `/api/analyses/${encodeURIComponent(id)}`),
	cancelAnalysis: (id: string) =>
		request<AnalysisJob>('POST', `/api/analyses/${encodeURIComponent(id)}/cancel`),
	getConfig: () => request<AppConfig>('GET', '/api/config')
};

/** URL of a job's Server-Sent Events stream (consumed by live-analysis.ts). */
export const analysisEventsUrl = (id: string) => `/api/analyses/${encodeURIComponent(id)}/events`;
