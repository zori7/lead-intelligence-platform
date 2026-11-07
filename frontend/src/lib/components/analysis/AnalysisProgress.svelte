<script lang="ts">
	import CircleIcon from '@lucide/svelte/icons/circle';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleMinusIcon from '@lucide/svelte/icons/circle-minus';
	import CircleXIcon from '@lucide/svelte/icons/circle-x';
	import LoaderCircleIcon from '@lucide/svelte/icons/loader-circle';
	import type { AnalysisStep, StepName, StepState } from '#lib/api/types.js';
	import { popIn } from '#lib/motion/attachments.js';
	import { stepLabel, stepStateLabel } from './labels.js';

	let { steps, progress }: { steps: AnalysisStep[]; progress: number } = $props();

	const finished = new Set<StepState>(['done', 'skipped']);

	// Icons present on first render (e.g. a snapshot after a reload) appear as they
	// are; only icons that replace another later, as a step changes state, pop in.
	const mountedAt = performance.now();
	const iconIn = () => (performance.now() - mountedAt > 100 ? popIn() : undefined);

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
		class="h-1.5 overflow-hidden rounded-full bg-foreground/[0.07]"
		role="progressbar"
		aria-label="Analysis progress"
		aria-valuemin={0}
		aria-valuemax={100}
		aria-valuenow={progress}
	>
		<div
			class="score-gradient h-full rounded-full transition-[width] duration-300"
			style:width="{Math.max(0, Math.min(100, progress))}%"
		></div>
	</div>

	<ol class="flex flex-col">
		{#each rows as row, r (row[0].name)}
			<li>
				<div class="grid gap-2 {row.length > 1 ? 'sm:grid-cols-2' : ''}">
					{#each row as step (step.name)}
						<div
							class="flex items-center gap-2.5 rounded-xl px-3 py-2 ring-1 ring-foreground/[0.06] transition-colors duration-300 ring-inset
							{step.state === 'running' ? 'bg-info/[0.08]' : 'bg-foreground/[0.02]'}"
							data-state={step.state}
						>
							<!-- Each state renders its own icon, so a state change mounts a new one. -->
							{#if step.state === 'done'}
								<CircleCheckIcon class="size-4 shrink-0 text-success" {@attach iconIn()} />
							{:else if step.state === 'running'}
								<LoaderCircleIcon class="size-4 shrink-0 animate-spin text-info" />
							{:else if step.state === 'failed'}
								<CircleXIcon class="size-4 shrink-0 text-destructive" {@attach iconIn()} />
							{:else if step.state === 'skipped'}
								<CircleMinusIcon class="size-4 shrink-0 text-muted-foreground" {@attach iconIn()} />
							{:else}
								<CircleIcon class="size-4 shrink-0 text-muted-foreground/60" />
							{/if}
							<span
								class={step.state === 'pending' || step.state === 'skipped'
									? 'text-muted-foreground'
									: ''}
							>
								{stepLabel[step.name]}
								<span class="sr-only">: {stepStateLabel[step.state]}</span>
							</span>
							{#if step.state === 'skipped'}
								<span class="ml-auto text-xs text-muted-foreground" aria-hidden="true">skipped</span
								>
							{/if}
						</div>
					{/each}
				</div>
				{#if r < rows.length - 1}
					<!-- Connector to the next row; it fills once every step in this row has finished. -->
					<div
						class="relative ml-[19px] h-2.5 w-0.5 overflow-hidden bg-foreground/10"
						aria-hidden="true"
					>
						<div
							class="absolute inset-0 origin-top bg-success transition-transform duration-[400ms] ease-out
							{row.every((s) => finished.has(s.state)) ? 'scale-y-100' : 'scale-y-0'}"
						></div>
					</div>
				{/if}
			</li>
		{/each}
	</ol>
</div>
