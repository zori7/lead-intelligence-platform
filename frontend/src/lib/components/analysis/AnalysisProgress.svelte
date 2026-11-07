<script lang="ts">
	import CircleIcon from '@lucide/svelte/icons/circle';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleMinusIcon from '@lucide/svelte/icons/circle-minus';
	import CircleXIcon from '@lucide/svelte/icons/circle-x';
	import LoaderCircleIcon from '@lucide/svelte/icons/loader-circle';
	import type { AnalysisStep, StepName } from '#lib/api/types.js';
	import { stepLabel } from './labels.js';

	let { steps, progress }: { steps: AnalysisStep[]; progress: number } = $props();

	// fit and intent run in parallel, so they share a row.
	const parallel: StepName[] = ['analyze_fit', 'analyze_intent'];
	const rows = $derived.by(() => {
		const out: AnalysisStep[][] = [];
		for (const step of steps) {
			const last = out.at(-1);
			if (last && parallel.includes(step.name) && parallel.includes(last[0].name)) {
				last.push(step);
			} else {
				out.push([step]);
			}
		}
		return out;
	});
</script>

<div class="flex flex-col gap-4">
	<div
		class="h-1.5 overflow-hidden rounded-full bg-white/[0.07]"
		role="progressbar"
		aria-label="Analysis progress"
		aria-valuemin={0}
		aria-valuemax={100}
		aria-valuenow={progress}
	>
		<div
			class="h-full rounded-full bg-gradient-to-r from-indigo-500 via-blue-500 to-sky-300 transition-[width] duration-300"
			style:width="{Math.max(0, Math.min(100, progress))}%"
		></div>
	</div>

	<ol class="flex flex-col gap-2">
		{#each rows as row (row[0].name)}
			<li class="grid gap-2 {row.length > 1 ? 'sm:grid-cols-2' : ''}">
				{#each row as step (step.name)}
					<div
						class="flex items-center gap-2.5 rounded-xl px-3 py-2 ring-1 ring-white/[0.06] ring-inset
						{step.state === 'running' ? 'bg-sky-400/[0.08]' : 'bg-white/[0.02]'}"
						data-state={step.state}
					>
						{#if step.state === 'done'}
							<CircleCheckIcon class="size-4 text-emerald-300" />
						{:else if step.state === 'running'}
							<LoaderCircleIcon class="size-4 animate-spin text-sky-300" />
						{:else if step.state === 'failed'}
							<CircleXIcon class="size-4 text-rose-300" />
						{:else if step.state === 'skipped'}
							<CircleMinusIcon class="size-4 text-muted-foreground" />
						{:else}
							<CircleIcon class="size-4 text-muted-foreground/60" />
						{/if}
						<span
							class={step.state === 'pending' || step.state === 'skipped'
								? 'text-muted-foreground'
								: ''}
						>
							{stepLabel[step.name]}
						</span>
						{#if step.state === 'skipped'}
							<span class="ml-auto text-xs text-muted-foreground">skipped</span>
						{/if}
					</div>
				{/each}
			</li>
		{/each}
	</ol>
</div>
