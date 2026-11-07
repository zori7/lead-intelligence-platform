<script lang="ts">
	import { untrack } from 'svelte';
	import SearchIcon from '@lucide/svelte/icons/search';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { LEAD_SORTS, LEAD_STATUSES, type LeadSort, type LeadStatus } from '#lib/api/types.js';
	import { sortLabel, statusLabel } from './labels.js';

	type Change = { status?: LeadStatus | null; q?: string; sort?: LeadSort };

	let {
		status,
		q,
		sort,
		onchange
	}: {
		status: LeadStatus | null;
		q: string;
		sort: LeadSort;
		onchange: (change: Change) => void;
	} = $props();

	// Local draft so typing is instant; the URL (and the query) update after a pause.
	let search = $state(untrack(() => q));

	// Follow external changes (back/forward navigation) without clobbering typing.
	$effect(() => {
		const external = q;
		untrack(() => {
			if (search.trim() !== external) search = external;
		});
	});

	$effect(() => {
		const value = search.trim();
		if (value === untrack(() => q)) return;
		const timer = setTimeout(() => onchange({ q: value }), 300);
		return () => clearTimeout(timer);
	});
</script>

<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
	<div
		class="glass flex w-fit max-w-full flex-wrap gap-1 rounded-full p-1"
		role="group"
		aria-label="Filter by status"
	>
		{#each [null, ...LEAD_STATUSES] as s (s)}
			<button
				type="button"
				aria-pressed={status === s}
				class="rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none {status ===
				s
					? 'btn-glow'
					: 'text-muted-foreground hover:bg-foreground/[0.06] hover:text-foreground'}"
				onclick={() => onchange({ status: s })}>{s === null ? 'All' : statusLabel[s]}</button
			>
		{/each}
	</div>

	<div class="flex gap-2">
		<div class="relative w-full lg:w-72">
			<SearchIcon
				class="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground"
			/>
			<Input
				type="search"
				placeholder="Search company or contact…"
				aria-label="Search leads"
				class="h-10 rounded-full pl-10 backdrop-blur-md"
				bind:value={search}
			/>
		</div>
		<Select.Root
			type="single"
			value={sort}
			onValueChange={(v) => v && onchange({ sort: v as LeadSort })}
		>
			<Select.Trigger class="h-10! w-44 rounded-full px-4 backdrop-blur-md" aria-label="Sort">
				<span class="text-muted-foreground">Sort:</span>
				{sortLabel[sort]}
			</Select.Trigger>
			<Select.Content>
				{#each LEAD_SORTS as s (s)}
					<Select.Item value={s} label={sortLabel[s]} />
				{/each}
			</Select.Content>
		</Select.Root>
	</div>
</div>
