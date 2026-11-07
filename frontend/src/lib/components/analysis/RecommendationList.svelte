<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import type { Recommendation } from '#lib/api/types.js';
	import { priorityTone } from './labels.js';

	let { recommendations }: { recommendations: Recommendation[] } = $props();
</script>

<Card.Root>
	<Card.Header><Card.Title class="text-base">Recommended next steps</Card.Title></Card.Header>
	<Card.Content>
		{#if recommendations.length === 0}
			<p class="text-muted-foreground">No recommendations.</p>
		{:else}
			<ol class="flex flex-col gap-4">
				{#each recommendations as rec, i (i)}
					<li class="flex gap-3">
						<span
							class="grid size-6 shrink-0 place-items-center rounded-full bg-white/[0.06] text-xs font-medium tabular-nums"
							>{i + 1}</span
						>
						<div class="flex flex-col gap-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="font-medium">{rec.action}</span>
								<span
									class="rounded-full px-2 py-0.5 text-xs font-medium capitalize ring-1 ring-inset {priorityTone[
										rec.priority
									]}">{rec.priority}</span
								>
							</div>
							<p class="text-muted-foreground">{rec.rationale}</p>
						</div>
					</li>
				{/each}
			</ol>
		{/if}
	</Card.Content>
</Card.Root>
