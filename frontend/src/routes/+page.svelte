<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createQuery } from '@tanstack/svelte-query';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import LeadFilters from '#lib/components/leads/LeadFilters.svelte';
	import LeadTable from '#lib/components/leads/LeadTable.svelte';
	import LeadForm from '#lib/components/leads/LeadForm.svelte';
	import { leadsQuery } from '#lib/api/queries.js';
	import {
		LEAD_SORTS,
		LEAD_STATUSES,
		type LeadFilters as Filters,
		type LeadSort,
		type LeadStatus
	} from '#lib/api/types.js';

	const PAGE_SIZE = 50;

	const oneOf = <T extends string>(list: readonly T[], v: string | null): T | null =>
		v !== null && (list as readonly string[]).includes(v) ? (v as T) : null;

	// Filters live in the URL so views are shareable and the back button works.
	const filters = $derived.by<Filters>(() => {
		const p = page.url.searchParams;
		const pageNum = Math.max(1, Number.parseInt(p.get('page') ?? '1', 10) || 1);
		return {
			status: oneOf<LeadStatus>(LEAD_STATUSES, p.get('status')),
			q: (p.get('q') ?? '').trim(),
			sort: oneOf<LeadSort>(LEAD_SORTS, p.get('sort')) ?? 'created_desc',
			limit: PAGE_SIZE,
			offset: (pageNum - 1) * PAGE_SIZE
		};
	});
	const pageNum = $derived(filters.offset / PAGE_SIZE + 1);

	const leads = createQuery(() => leadsQuery(filters));

	let formOpen = $state(false);

	function setParams(change: {
		status?: LeadStatus | null;
		q?: string;
		sort?: LeadSort;
		page?: number;
	}) {
		const url = new URL(page.url.href);
		const set = (key: string, value: string | null | undefined, defaultValue?: string) => {
			if (value === undefined) return;
			if (value === null || value === '' || value === defaultValue) url.searchParams.delete(key);
			else url.searchParams.set(key, value);
		};
		set('status', change.status);
		set('q', change.q);
		set('sort', change.sort, 'created_desc');
		// Any filter change resets pagination.
		set('page', change.page !== undefined ? String(change.page) : '1', '1');
		goto(url, { replace: true, reset: false });
	}

	const total = $derived(leads.data?.total ?? 0);
	const lastPage = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-wrap items-end justify-between gap-4 pt-2">
		<div>
			<h1 class="text-3xl font-semibold tracking-tight">Leads</h1>
			<p class="mt-1 text-sm text-muted-foreground">
				Track and manage your pipeline
				{#if leads.data}
					<span class="mx-1 opacity-50">·</span>
					<span class="text-foreground tabular-nums">{total}</span>
					{total === 1 ? 'lead' : 'leads'}
				{/if}
			</p>
		</div>
		<Button class="h-10 rounded-xl px-5" onclick={() => (formOpen = true)}>
			<PlusIcon /> Add lead
		</Button>
	</div>

	<LeadFilters status={filters.status} q={filters.q} sort={filters.sort} onchange={setParams} />

	<Card.Root class="glow-edge gap-0 py-0">
		{#if leads.isError}
			<div class="flex flex-col items-center gap-3 py-16 text-center">
				<p class="text-muted-foreground">Could not load leads: {leads.error.message}</p>
				<Button variant="outline" onclick={() => leads.refetch()}>Try again</Button>
			</div>
		{:else}
			<LeadTable
				leads={leads.data?.items ?? []}
				loading={leads.isPending}
				filtered={filters.status !== null || filters.q !== ''}
				oncreate={() => (formOpen = true)}
			/>
		{/if}

		{#if total > 0}
			<div
				class="flex flex-wrap items-center justify-between gap-3 border-t border-white/[0.06] px-5 py-3.5 text-sm"
			>
				<span class="text-muted-foreground">
					Showing {filters.offset + 1}–{Math.min(filters.offset + PAGE_SIZE, total)} of {total}
				</span>
				{#if lastPage > 1}
					<div class="flex items-center gap-2">
						<Button
							variant="outline"
							size="sm"
							class="rounded-lg"
							disabled={pageNum <= 1}
							onclick={() => setParams({ page: pageNum - 1 })}>Previous</Button
						>
						<span class="btn-glow grid size-8 place-items-center rounded-lg text-xs tabular-nums"
							>{pageNum}</span
						>
						<Button
							variant="outline"
							size="sm"
							class="rounded-lg"
							disabled={pageNum >= lastPage}
							onclick={() => setParams({ page: pageNum + 1 })}>Next</Button
						>
					</div>
				{/if}
			</div>
		{/if}
	</Card.Root>
</div>

<LeadForm bind:open={formOpen} />
