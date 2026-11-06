import { keepPreviousData, queryOptions, type QueryClient } from '@tanstack/svelte-query';
import { ApiError, api } from './client.js';
import type { Lead, LeadDetail, LeadFilters, LeadCreate, LeadPatch, LeadStatus } from './types.js';

export const keys = {
	leads: ['leads'] as const,
	leadList: (filters: LeadFilters) => ['leads', filters] as const,
	lead: (id: string) => ['lead', id] as const
};

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
		retry: (count, err) => !(err instanceof ApiError && err.status === 404) && count < 3
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
