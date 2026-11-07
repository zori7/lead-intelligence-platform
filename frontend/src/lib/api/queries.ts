import { keepPreviousData, queryOptions, type QueryClient } from '@tanstack/svelte-query';
import { adminSession } from '#lib/admin-token.svelte.js';
import { ApiError, adminApi, api } from './client.js';
import type {
	AnalysisJob,
	Lead,
	LeadDetail,
	LeadFilters,
	LeadCreate,
	LeadPatch,
	LeadStatus,
	LlmSettings,
	LlmSettingsUpdate
} from './types.js';

export const keys = {
	leads: ['leads'] as const,
	leadList: (filters: LeadFilters) => ['leads', filters] as const,
	lead: (id: string) => ['lead', id] as const,
	analyses: (leadId: string) => ['analyses', leadId] as const,
	analysis: (jobId: string) => ['analysis', jobId] as const,
	config: ['config'] as const,
	admin: ['admin'] as const,
	llmSettings: ['admin', 'llm-settings'] as const,
	llmModels: ['admin', 'llm-models'] as const
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

/**
 * Cancels a queued or running job. The event stream also delivers the
 * `cancelled` event; writing the response here updates the UI even when the
 * stream is not connected. A 409 means the job finished first: the stream
 * (or a refetch) brings its final state.
 */
export const cancelAnalysisMutation = (qc: QueryClient, leadId: string) => ({
	mutationFn: (jobId: string) => api.cancelAnalysis(jobId),
	onSuccess: (job: AnalysisJob) => {
		writeAnalysis(qc, job);
		return qc.invalidateQueries({ queryKey: keys.analyses(leadId) });
	},
	onError: (_err: Error, jobId: string) => qc.invalidateQueries({ queryKey: keys.analysis(jobId) })
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

// ---- Superadmin ---------------------------------------------------------------

const hasStatus = (err: Error, ...statuses: number[]) =>
	err instanceof ApiError && statuses.includes(err.status);

/**
 * Runs an admin call with the session token. A 401 means the token is wrong or
 * was rotated on the server, so the session locks and the page shows the gate.
 */
async function asAdmin<T>(call: (token: string) => Promise<T>): Promise<T> {
	const token = adminSession.token;
	if (token === null) throw new ApiError(401, 'unauthorized', 'Admin token required.');
	try {
		return await call(token);
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) adminSession.lock();
		throw err;
	}
}

export const llmSettingsQuery = () =>
	queryOptions({
		queryKey: keys.llmSettings,
		queryFn: () => asAdmin(adminApi.getLlmSettings),
		retry: (count, err) => !hasStatus(err, 401) && count < 3
	});

/**
 * Models on the LLM server. A 503 (server unreachable) is shown at once with a
 * Refresh button instead of being retried in the background.
 */
export const llmModelsQuery = () =>
	queryOptions({
		queryKey: keys.llmModels,
		queryFn: () => asAdmin(adminApi.listLlmModels),
		retry: (count, err) => !hasStatus(err, 401, 503) && count < 2
	});

/**
 * Checks a token by loading the settings with it. On success the token is
 * kept for the session and the settings seed the cache; anything cached under
 * a previous token is dropped first.
 */
export const unlockAdminMutation = (qc: QueryClient) => ({
	mutationFn: (token: string) => adminApi.getLlmSettings(token),
	onSuccess: (settings: LlmSettings, token: string) => {
		qc.removeQueries({ queryKey: keys.admin });
		qc.setQueryData(keys.llmSettings, settings);
		adminSession.unlock(token);
	}
});

/** Full-replace save. The response is authoritative; `['config']` follows it. */
export const saveLlmSettingsMutation = (qc: QueryClient) => ({
	mutationFn: (body: LlmSettingsUpdate) => asAdmin((t) => adminApi.putLlmSettings(t, body)),
	onSuccess: (settings: LlmSettings) => {
		qc.setQueryData(keys.llmSettings, settings);
		return qc.invalidateQueries({ queryKey: keys.config });
	}
});
