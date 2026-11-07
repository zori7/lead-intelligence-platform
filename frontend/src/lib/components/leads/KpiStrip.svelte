<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { leadStatsQuery } from '#lib/api/queries.js';
	import type { Tier } from '#lib/api/types.js';
	import { tierLabel, tierTone } from '#lib/components/analysis/labels.js';
	import { staggerChildren } from '#lib/motion/attachments.js';

	// Pipeline totals over all leads, independent of the list's filters.
	const stats = createQuery(() => leadStatsQuery());

	const tiers: Tier[] = ['hot', 'warm', 'cold'];
	const analyzedPct = $derived(
		stats.data && stats.data.total > 0
			? Math.round((stats.data.analyzed / stats.data.total) * 100)
			: 0
	);
</script>

{#snippet tile(label: string, value: number, dot?: string)}
	<div class="glass flex flex-col gap-1 rounded-2xl px-5 py-4">
		<span class="flex items-center gap-2 text-sm text-muted-foreground">
			{#if dot}<span class="size-2 rounded-full {dot}" aria-hidden="true"></span>{/if}
			{label}
		</span>
		<span class="text-2xl font-semibold tracking-tight tabular-nums">{value}</span>
	</div>
{/snippet}

<section aria-label="Pipeline summary">
	{#if stats.data}
		{@const s = stats.data}
		<div class="grid grid-cols-2 gap-3 md:grid-cols-5" {@attach staggerChildren({ stagger: 0.04 })}>
			{@render tile('Total leads', s.total)}
			{#each tiers as tier (tier)}
				{@render tile(tierLabel[tier], s.by_tier[tier], tierTone[tier].dot)}
			{/each}
			<div class="glass col-span-2 flex flex-col gap-1 rounded-2xl px-5 py-4 md:col-span-1">
				<span class="text-sm text-muted-foreground">Analyzed</span>
				<span class="text-2xl font-semibold tracking-tight tabular-nums">{analyzedPct}%</span>
				<div
					class="mt-1 h-1 overflow-hidden rounded-full bg-foreground/[0.08]"
					role="meter"
					aria-label="Share of leads analyzed"
					aria-valuemin={0}
					aria-valuemax={100}
					aria-valuenow={analyzedPct}
					aria-valuetext="{s.analyzed} of {s.total} leads analyzed"
				>
					<div class="score-gradient h-full rounded-full" style:width="{analyzedPct}%"></div>
				</div>
			</div>
		</div>
	{:else if stats.isError}
		<p class="text-sm text-muted-foreground">Pipeline summary unavailable.</p>
	{:else}
		<div class="grid grid-cols-2 gap-3 md:grid-cols-5">
			{#each { length: 5 }, i (i)}
				<Skeleton class="h-[5.25rem] rounded-2xl {i === 4 ? 'col-span-2 md:col-span-1' : ''}" />
			{/each}
		</div>
	{/if}
</section>
