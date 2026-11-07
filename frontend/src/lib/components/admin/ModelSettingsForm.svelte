<script lang="ts">
	import { untrack } from 'svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import InfoIcon from '@lucide/svelte/icons/info';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-exclamation-point';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { ApiError } from '#lib/api/client.js';
	import { llmModelsQuery, saveLlmSettingsMutation } from '#lib/api/queries.js';
	import { LLM_NODES, type LlmNode, type LlmSettings } from '#lib/api/types.js';
	import {
		fromSettings,
		resolve,
		sameSettings,
		toInput,
		validateSettings,
		type FieldErrors
	} from '#lib/schemas/llm-settings.js';
	import { stepLabel } from '#lib/components/analysis/labels.js';
	import NodeModelRow from './NodeModelRow.svelte';

	let { settings }: { settings: LlmSettings } = $props();

	const qc = useQueryClient();
	const models = createQuery(() => llmModelsQuery());
	const save = createMutation(() => saveLlmSettingsMutation(qc));

	const stepHint: Record<LlmNode, string> = {
		extract_company: 'Company profile from lead data and website',
		analyze_fit: 'ICP fit; runs in parallel with intent',
		analyze_intent: 'Buying intent; runs in parallel with fit',
		generate_recommendations: 'Next steps for the sales team'
	};

	let values = $state(fromSettings(untrack(() => settings)));
	let errors = $state<FieldErrors>({});

	// The server's settings are authoritative: after a save, or when another
	// admin's change arrives on refetch, the form shows them. An unchanged
	// refetch keeps the same object (structural sharing) and does not reset edits.
	$effect(() => {
		const next = settings;
		untrack(() => {
			values = fromSettings(next);
			errors = {};
		});
	});

	// A failed refetch keeps the previous list in `data`; it no longer counts as verified.
	const modelIds = $derived(models.isError ? undefined : models.data?.items.map((m) => m.id));
	const unreachable = $derived(
		models.isError && models.error instanceof ApiError && models.error.status === 503
	);
	// Saving needs a verified model list (the server checks every model against it).
	const readOnly = $derived(modelIds === undefined);
	const dirty = $derived(!sameSettings(values, fromSettings(settings)));
	const effective = $derived(resolve(toInput(values)));
	const parallelMismatch = $derived(
		effective !== null && effective.analyze_fit.model !== effective.analyze_intent.model
	);

	function clearError(key: string) {
		if (errors[key]) delete errors[key];
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		const result = validateSettings(values);
		if (!result.ok) {
			errors = result.errors;
			return;
		}
		errors = {};
		try {
			await save.mutateAsync(result.data);
			toast.success('Saved — applies to the next analysis');
		} catch (err) {
			if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
				errors = err.fields;
			} else if (err instanceof ApiError && err.status === 503) {
				toast.error('LLM server unreachable. Nothing was saved.');
				void models.refetch();
			} else if (!(err instanceof ApiError && err.status === 401)) {
				toast.error(err instanceof Error ? err.message : 'Something went wrong.');
			}
		}
	}
</script>

<form novalidate onsubmit={submit}>
	<Card.Root class="glow-edge gap-0 py-0">
		<Card.Header class="border-b border-border py-5">
			<Card.Title class="text-base">LLM models</Card.Title>
			<Card.Description>
				Steps without their own model or temperature inherit the default.
			</Card.Description>
			<Card.Action>
				<Button
					type="button"
					variant="outline"
					size="sm"
					disabled={models.isFetching}
					onclick={() => models.refetch()}
				>
					<RefreshCwIcon class={models.isFetching ? 'animate-spin' : ''} />
					Refresh models
				</Button>
			</Card.Action>
		</Card.Header>

		{#if !settings.configured || unreachable || (models.isError && !unreachable)}
			<div class="flex flex-col gap-3 px-5 pt-5">
				{#if !settings.configured}
					<div
						class="flex items-start gap-3 rounded-2xl bg-info/[0.08] px-4 py-3 ring-1 ring-info/20 ring-inset"
						role="status"
					>
						<InfoIcon class="mt-0.5 size-4 shrink-0 text-info" />
						<div>
							<p class="font-medium">AI is not configured yet</p>
							<p class="text-sm text-muted-foreground">
								Choose a default model and save. Leads cannot be analyzed until then.
							</p>
						</div>
					</div>
				{/if}
				{#if unreachable}
					<div
						class="flex items-start gap-3 rounded-2xl bg-destructive/[0.08] px-4 py-3 ring-1 ring-destructive/20 ring-inset"
						role="alert"
					>
						<TriangleAlertIcon class="mt-0.5 size-4 shrink-0 text-destructive" />
						<div>
							<p class="font-medium">LLM server unreachable</p>
							<p class="text-sm text-muted-foreground">
								Models cannot be verified, so saving is disabled. Start the LLM server and refresh.
							</p>
						</div>
					</div>
				{:else if models.isError}
					<p class="text-sm text-destructive" role="alert">
						Could not load models: {models.error.message}
					</p>
				{/if}
			</div>
		{/if}

		<Table.Root>
			<Table.Header>
				<Table.Row class="hover:bg-transparent">
					<Table.Head class="h-12 pl-5 text-muted-foreground">Step</Table.Head>
					<Table.Head class="text-muted-foreground">Model</Table.Head>
					<Table.Head class="text-muted-foreground">Temperature</Table.Head>
					<Table.Head class="pr-5 text-muted-foreground">Effective</Table.Head>
				</Table.Row>
			</Table.Header>
			<Table.Body>
				<NodeModelRow
					field="default"
					label="Default"
					hint="Used by every step that inherits"
					isDefault
					bind:value={values.default}
					models={modelIds}
					disabled={readOnly}
					{errors}
					onedit={clearError}
				/>
				{#each LLM_NODES as node (node)}
					<NodeModelRow
						field="nodes.{node}"
						label={stepLabel[node]}
						hint={stepHint[node]}
						bind:value={values.nodes[node]}
						models={modelIds}
						disabled={readOnly}
						{errors}
						effective={effective?.[node] ?? null}
						onedit={clearError}
					/>
				{/each}
			</Table.Body>
		</Table.Root>

		<div class="flex flex-wrap items-center justify-between gap-3 border-t border-border px-5 py-4">
			<p class="text-sm text-muted-foreground">
				{#if parallelMismatch}
					<span class="text-warning">
						ICP fit and buying intent run in parallel but use different models. Local servers that
						hold one model in memory may reload it for each call.
					</span>
				{:else}
					Changes apply to the next analysis; running ones keep their models.
				{/if}
			</p>
			<div class="flex gap-2">
				<Button
					type="button"
					variant="outline"
					disabled={!dirty || save.isPending}
					onclick={() => {
						values = fromSettings(settings);
						errors = {};
					}}>Discard</Button
				>
				<Button type="submit" class="btn-glow" disabled={readOnly || !dirty || save.isPending}>
					{save.isPending ? 'Saving…' : 'Save'}
				</Button>
			</div>
		</div>
	</Card.Root>
</form>
