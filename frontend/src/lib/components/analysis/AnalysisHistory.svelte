<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { analysesQuery } from '#lib/api/queries.js';
	import type { AnalysisJob } from '#lib/api/types.js';
	import { formatDateTime } from '#lib/components/leads/labels.js';
	import { jobStatusLabel, tierLabel, tierTone } from './labels.js';

	let {
		leadId,
		selectedId,
		onselect
	}: { leadId: string; selectedId: string | null; onselect: (job: AnalysisJob) => void } = $props();

	const history = createQuery(() => analysesQuery(leadId));
</script>

<Card.Root size="sm">
	<Card.Header><Card.Title class="text-base">History</Card.Title></Card.Header>
	<Card.Content>
		{#if history.isPending}
			<Skeleton class="h-24 w-full bg-white/[0.05]" />
		{:else if history.isError}
			<p class="text-muted-foreground">Could not load history.</p>
		{:else if history.data.items.length === 0}
			<p class="text-muted-foreground">No analyses yet.</p>
		{:else}
			<ul class="flex flex-col gap-1">
				{#each history.data.items as job (job.id)}
					<li>
						<button
							type="button"
							class="flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-white/[0.05]
							{job.id === selectedId ? 'bg-white/[0.07] ring-1 ring-white/[0.1] ring-inset' : ''}"
							aria-current={job.id === selectedId ? 'true' : undefined}
							onclick={() => onselect(job)}
						>
							<span class="flex-1 text-sm">{formatDateTime(job.created_at)}</span>
							{#if job.result}
								<span
									class="text-sm font-medium tabular-nums {tierTone[job.result.score.tier].text}"
								>
									{job.result.score.value} · {tierLabel[job.result.score.tier]}
								</span>
							{:else}
								<span class="text-xs text-muted-foreground">{jobStatusLabel[job.status]}</span>
							{/if}
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	</Card.Content>
</Card.Root>
