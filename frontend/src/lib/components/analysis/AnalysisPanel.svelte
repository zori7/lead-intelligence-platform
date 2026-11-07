<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import CircleStopIcon from '@lucide/svelte/icons/circle-stop';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-exclamation-point';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import {
		analysisQuery,
		cancelAnalysisMutation,
		configQuery,
		triggerAnalysisMutation
	} from '#lib/api/queries.js';
	import { subscribeToAnalysis } from '#lib/api/live-analysis.js';
	import { isActiveJob, type AnalysisJob } from '#lib/api/types.js';
	import { formatDateTime } from '#lib/components/leads/labels.js';
	import AnalysisProgress from './AnalysisProgress.svelte';
	import AnalysisHistory from './AnalysisHistory.svelte';
	import CompanyInsights from './CompanyInsights.svelte';
	import RecommendationList from './RecommendationList.svelte';
	import ScoreBreakdown from './ScoreBreakdown.svelte';
	import ScoreRing from './ScoreRing.svelte';
	import { percent, stepLabel } from './labels.js';

	let { leadId, latest }: { leadId: string; latest: AnalysisJob | null } = $props();

	const qc = useQueryClient();
	const config = createQuery(() => configQuery());
	const trigger = createMutation(() => triggerAnalysisMutation(qc, leadId));
	const cancel = createMutation(() => cancelAnalysisMutation(qc, leadId));

	// A past job picked from the history; null shows the latest one.
	let selectedId = $state<string | null>(null);
	const jobId = $derived(selectedId ?? latest?.id ?? null);
	const job = createQuery(() => ({
		...analysisQuery(jobId ?? '', latest),
		enabled: jobId !== null
	}));
	const current = $derived(jobId === null ? undefined : job.data);
	const viewingPast = $derived(selectedId !== null && selectedId !== latest?.id);

	// Stream the lead's active job, if any. The stream writes into the query
	// cache; this component only reads it. Keyed on the ID, so progress events
	// do not reopen the connection.
	const liveId = $derived(latest && isActiveJob(latest.status) ? latest.id : null);
	$effect(() => {
		if (!liveId) return;
		return subscribeToAnalysis(qc, liveId);
	});

	const aiConfigured = $derived(config.data?.ai_configured !== false);
	const busy = $derived(trigger.isPending || (latest !== null && isActiveJob(latest.status)));

	function analyze() {
		selectedId = null;
		trigger.mutate(undefined, {
			onError: (err) => toast.error(`Analysis not started: ${err.message}`)
		});
	}

	function cancelJob(id: string) {
		cancel.mutate(id, {
			onError: (err) => toast.error(`Could not cancel: ${err.message}`)
		});
	}

	function select(picked: AnalysisJob) {
		selectedId = picked.id === latest?.id ? null : picked.id;
	}
</script>

{#snippet analyzeButton(label: string)}
	<Button class="btn-glow" disabled={!aiConfigured || busy} onclick={analyze}>
		<SparklesIcon />
		{trigger.isPending ? 'Starting…' : label}
	</Button>
{/snippet}

<section class="flex flex-col gap-4" aria-labelledby="analysis-heading">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h2 id="analysis-heading" class="text-lg font-semibold tracking-tight">AI analysis</h2>
			{#if viewingPast && current}
				<p class="text-sm text-muted-foreground">
					Viewing the analysis from {formatDateTime(current.created_at)} ·
					<button
						type="button"
						class="text-sky-300 hover:underline"
						onclick={() => (selectedId = null)}>back to latest</button
					>
				</p>
			{/if}
		</div>
		{#if latest && !isActiveJob(latest.status)}
			{@render analyzeButton(latest.status === 'succeeded' ? 'Re-analyze' : 'Analyze')}
		{/if}
	</div>

	{#if !aiConfigured}
		<p class="text-sm text-muted-foreground">
			AI not configured — ask an admin (<a href="/admin" class="text-sky-300 hover:underline"
				>/admin</a
			>).
		</p>
	{/if}

	<div class="grid gap-4 lg:grid-cols-[1fr_18rem]">
		<div class="flex min-w-0 flex-col gap-4">
			{#if jobId === null}
				<Card.Root class="glow-edge">
					<Card.Content class="flex flex-col items-center gap-3 py-8 text-center">
						<p class="text-lg font-medium">Not analyzed yet</p>
						<p class="max-w-md text-muted-foreground">
							The AI reads the company's website, scores fit and buying intent, and suggests next
							steps.
						</p>
						{@render analyzeButton('Analyze')}
					</Card.Content>
				</Card.Root>
			{:else if !current}
				{#if job.isError}
					<p class="text-muted-foreground">Could not load the analysis: {job.error.message}</p>
				{:else}
					<Skeleton class="h-64 w-full rounded-2xl bg-white/[0.05]" />
				{/if}
			{:else if isActiveJob(current.status)}
				<Card.Root class="glow-edge">
					<Card.Header>
						<Card.Title class="text-base">
							{current.status === 'queued' ? 'Waiting for a worker…' : 'Analyzing…'}
						</Card.Title>
						<Card.Description
							>Steps update live; you can leave this page and come back.</Card.Description
						>
						<Card.Action>
							<Button
								variant="outline"
								size="sm"
								disabled={cancel.isPending}
								onclick={() => cancelJob(current.id)}
							>
								<CircleStopIcon />
								{cancel.isPending ? 'Cancelling…' : 'Cancel'}
							</Button>
						</Card.Action>
					</Card.Header>
					<Card.Content>
						<AnalysisProgress steps={current.steps} progress={current.progress} />
					</Card.Content>
				</Card.Root>
			{:else if current.status === 'failed' || current.status === 'cancelled'}
				<Card.Root>
					<Card.Content class="flex flex-col gap-4">
						<div class="flex items-start gap-3">
							{#if current.status === 'cancelled'}
								<CircleStopIcon class="mt-0.5 size-5 shrink-0 text-muted-foreground" />
							{:else}
								<TriangleAlertIcon class="mt-0.5 size-5 shrink-0 text-rose-300" />
							{/if}
							<div class="flex flex-1 flex-col gap-1">
								<p class="font-medium">
									{current.status === 'cancelled' ? 'Analysis cancelled' : 'Analysis failed'}
								</p>
								{#if current.error}
									<p class="text-muted-foreground">{current.error.message}</p>
									{#if current.error.code === 'provider_config'}
										<p class="text-sm text-muted-foreground">
											An admin can change the models in <a
												href="/admin"
												class="text-sky-300 hover:underline">model settings</a
											>.
										</p>
									{/if}
								{/if}
							</div>
							{#if current.error?.retryable && !viewingPast}
								<Button variant="outline" disabled={!aiConfigured || busy} onclick={analyze}>
									<RotateCwIcon /> Retry
								</Button>
							{:else if current.status === 'cancelled' && !viewingPast}
								<Button variant="outline" disabled={!aiConfigured || busy} onclick={analyze}>
									<SparklesIcon /> Analyze again
								</Button>
							{/if}
						</div>
						<AnalysisProgress steps={current.steps} progress={current.progress} />
					</Card.Content>
				</Card.Root>
			{:else if current.result}
				{@const result = current.result}
				<Card.Root class="glow-edge">
					<Card.Content class="flex flex-col items-center gap-6 sm:flex-row sm:items-center">
						<ScoreRing value={result.score.value} tier={result.score.tier} />
						<div class="flex flex-col gap-2">
							<p class="leading-relaxed">{result.summary}</p>
							<p class="text-sm text-muted-foreground">
								Confidence {percent(result.score.confidence)} · analyzed {formatDateTime(
									current.finished_at
								)}
							</p>
						</div>
					</Card.Content>
				</Card.Root>

				{#if result.meta.partial}
					<div
						class="flex items-start gap-3 rounded-2xl bg-amber-400/[0.08] px-4 py-3 ring-1 ring-amber-300/20 ring-inset"
						role="status"
					>
						<TriangleAlertIcon class="mt-0.5 size-4 shrink-0 text-amber-300" />
						<div>
							<p class="font-medium">Partial result</p>
							<p class="text-sm text-muted-foreground">
								{result.meta.warnings.join(' ') ||
									'Part of the analysis failed; the score uses what is available.'}
							</p>
						</div>
					</div>
				{/if}

				<CompanyInsights company={result.company} website={result.meta.website} />
				<ScoreBreakdown fit={result.fit} intent={result.intent} />
				<RecommendationList recommendations={result.recommendations} />

				<p class="text-xs text-muted-foreground">
					Models:
					{#each Object.entries(result.meta.node_models) as [node, model], i (node)}
						{i > 0 ? ' · ' : ''}{node in stepLabel
							? stepLabel[node as keyof typeof stepLabel]
							: node}:
						<span class="text-foreground/80">{model}</span>
					{/each}
					· {(result.meta.duration_ms / 1000).toFixed(1)}s
				</p>
			{/if}
		</div>

		{#if latest}
			<AnalysisHistory {leadId} selectedId={jobId} onselect={select} />
		{/if}
	</div>
</section>
