import { keepPreviousData, queryOptions, type QueryClient } from '@tanstack/svelte-query';
import { ApiError, api } from './client.js';
import type {
	AnalysisJob,
	Lead,
	LeadDetail,
	LeadFilters,
	LeadCreate,
	LeadPatch,
	LeadStatus
} from './types.js';

export const keys = {
	leads: ['leads'] as const,
	leadList: (filters: LeadFilters) => ['leads', filters] as const,
	lead: (id: string) => ['lead', id] as const,
	analyses: (leadId: string) => ['analyses', leadId] as const,
	analysis: (jobId: string) => ['analysis', jobId] as const,
	config: ['config'] as const
};

/** How many past analyses the history list shows. */
export const HISTORY_LIMIT = 5;

const notFound = (err: Error) => err instanceof ApiError && err.status === 404;

export const leadsQuery = (filters: LeadFilters) =>
	queryOptions({
		queryKey: keys.leadList(filters),
		queryFn: () => api.listLeads(filters),
		// Keep the current rows on screen while a new filter/page loads.
		placeholderData: keepPreviousData
	});

export const leadQuery = (id: string) =>
	queryOptions({
		queryKey: keys.lead(id),
		queryFn: () => api.getLead(id),
		// A 404 will not fix itself; other failures get the default retries.
		retry: (count, err) => !notFound(err) && count < 3
	});

export const configQuery = () =>
	queryOptions({ queryKey: keys.config, queryFn: () => api.getConfig() });

/**
 * One analysis job. While it is active, live-analysis.ts keeps this cache
 * entry current from the SSE stream; `seed` (e.g. the lead's latest job)
 * renders immediately instead of waiting for the first fetch.
 */
export const analysisQuery = (jobId: string, seed?: AnalysisJob | null) =>
	queryOptions({
		queryKey: keys.analysis(jobId),
		queryFn: () => api.getAnalysis(jobId),
		initialData: seed?.id === jobId ? seed : undefined,
		retry: (count, err) => !notFound(err) && count < 3
	});

export const analysesQuery = (leadId: string) =>
	queryOptions({
		queryKey: keys.analyses(leadId),
		queryFn: () => api.listAnalyses(leadId, HISTORY_LIMIT)
	});

const isNewer = (a: AnalysisJob, b: AnalysisJob) => a.created_at > b.created_at;

/** Writes a job everywhere it is shown: its own entry and the lead's latest analysis. */
export function writeAnalysis(qc: QueryClient, job: AnalysisJob) {
	qc.setQueryData(keys.analysis(job.id), job);
	qc.setQueryData<LeadDetail>(keys.lead(job.lead_id), (old) =>
		old &&
		(old.latest_analysis === null ||
			old.latest_analysis.id === job.id ||
			isNewer(job, old.latest_analysis))
			? { ...old, latest_analysis: job }
			: old
	);
}

/**
 * Starts an analysis. The API returns the already-active job when there is
 * one, so double clicks and several tabs all converge on the same job.
 */
export const triggerAnalysisMutation = (qc: QueryClient, leadId: string) => ({
	mutationFn: () => api.triggerAnalysis(leadId),
	onSuccess: (job: AnalysisJob) => {
		writeAnalysis(qc, job);
		return qc.invalidateQueries({ queryKey: keys.analyses(leadId) });
	}
});

export const createLeadMutation = (qc: QueryClient) => ({
	mutationFn: (input: LeadCreate) => api.createLead(input),
	onSuccess: () => qc.invalidateQueries({ queryKey: keys.leads })
});

function writeLead(qc: QueryClient, lead: Lead) {
	qc.setQueryData<LeadDetail>(keys.lead(lead.id), (old) => (old ? { ...old, lead } : old));
}

export const updateLeadMutation = (qc: QueryClient, id: string) => ({
	mutationFn: (patch: LeadPatch) => api.updateLead(id, patch),
	onSuccess: (lead: Lead) => {
		writeLead(qc, lead);
		return qc.invalidateQueries({ queryKey: keys.leads });
	}
});

/** Status change is optimistic: the UI updates immediately and rolls back on error. */
export const updateStatusMutation = (qc: QueryClient, id: string) => ({
	mutationFn: (status: LeadStatus) => api.updateLead(id, { status }),
	onMutate: async (status: LeadStatus) => {
		await qc.cancelQueries({ queryKey: keys.lead(id) });
		const previous = qc.getQueryData<LeadDetail>(keys.lead(id));
		if (previous) {
			qc.setQueryData<LeadDetail>(keys.lead(id), {
				...previous,
				lead: { ...previous.lead, status }
			});
		}
		return { previous };
	},
	onError: (_err: Error, _status: LeadStatus, ctx: { previous?: LeadDetail } | undefined) => {
		if (ctx?.previous) qc.setQueryData(keys.lead(id), ctx.previous);
	},
	onSuccess: (lead: Lead) => writeLead(qc, lead),
	onSettled: () => qc.invalidateQueries({ queryKey: keys.leads })
});

export const deleteLeadMutation = (qc: QueryClient, id: string) => ({
	mutationFn: () => api.deleteLead(id),
	onSuccess: () => {
		qc.removeQueries({ queryKey: keys.lead(id) });
		return qc.invalidateQueries({ queryKey: keys.leads });
	}
});
