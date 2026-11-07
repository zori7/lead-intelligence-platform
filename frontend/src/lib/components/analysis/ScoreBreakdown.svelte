<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import type { FitAnalysis, IntentAnalysis } from '#lib/api/types.js';

	let { fit, intent }: { fit: FitAnalysis | null; intent: IntentAnalysis | null } = $props();
</script>

{#snippet bar(score: number)}
	<div class="flex items-center gap-3">
		<div class="h-1.5 flex-1 overflow-hidden rounded-full bg-white/[0.07]">
			<div
				class="h-full rounded-full bg-gradient-to-r from-indigo-500 to-sky-300"
				style:width="{Math.max(0, Math.min(100, score))}%"
			></div>
		</div>
		<span class="w-8 text-right font-medium tabular-nums">{score}</span>
	</div>
{/snippet}

{#snippet list(title: string, items: string[], tone: string)}
	{#if items.length > 0}
		<div>
			<p class="mb-1.5 text-sm text-muted-foreground">{title}</p>
			<ul class="flex flex-col gap-1">
				{#each items as item, i (i)}
					<li class="flex gap-2"><span class={tone}>•</span>{item}</li>
				{/each}
			</ul>
		</div>
	{/if}
{/snippet}

{#snippet unavailable()}
	<p class="text-muted-foreground">Unavailable: this part of the analysis failed.</p>
{/snippet}

<div class="grid gap-4 md:grid-cols-2">
	<Card.Root>
		<Card.Header><Card.Title class="text-base">ICP fit</Card.Title></Card.Header>
		<Card.Content class="flex flex-col gap-4">
			{#if fit}
				{@render bar(fit.score)}
				<p class="leading-relaxed">{fit.rationale}</p>
				{@render list('Matched criteria', fit.matched_criteria, 'text-emerald-300')}
				{@render list('Gaps', fit.gaps, 'text-amber-300')}
			{:else}
				{@render unavailable()}
			{/if}
		</Card.Content>
	</Card.Root>
	<Card.Root>
		<Card.Header><Card.Title class="text-base">Buying intent</Card.Title></Card.Header>
		<Card.Content class="flex flex-col gap-4">
			{#if intent}
				{@render bar(intent.score)}
				<p class="leading-relaxed">{intent.rationale}</p>
				{@render list('Signals', intent.signals, 'text-sky-300')}
			{:else}
				{@render unavailable()}
			{/if}
		</Card.Content>
	</Card.Root>
</div>
