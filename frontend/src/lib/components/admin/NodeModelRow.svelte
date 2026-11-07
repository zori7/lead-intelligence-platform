<script lang="ts">
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-exclamation-point';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import {
		DEFAULT_TEMPERATURE,
		type EffectiveSetting,
		type RowValues
	} from '#lib/schemas/llm-settings.js';
	import ModelSelect from './ModelSelect.svelte';

	let {
		field,
		label,
		hint,
		value = $bindable(),
		models,
		disabled,
		errors,
		effective,
		isDefault = false,
		onedit
	}: {
		/** Error-key prefix, e.g. `default` or `nodes.analyze_fit`. */
		field: string;
		label: string;
		hint?: string;
		value: RowValues;
		models: string[] | undefined;
		disabled: boolean;
		errors: Record<string, string>;
		/** Resolved values for a step row; null while no default model is chosen. */
		effective?: EffectiveSetting | null;
		isDefault?: boolean;
		onedit: (key: string) => void;
	} = $props();

	const id = $derived(field.replaceAll('.', '-'));
	const modelError = $derived(errors[`${field}.model`]);
	const temperatureError = $derived(errors[`${field}.temperature`]);
	const stale = $derived(
		models !== undefined && value.model !== null && !models.includes(value.model)
	);
</script>

<Table.Row class="align-top hover:bg-transparent">
	<Table.Cell class="py-4 pl-5">
		<p class="font-medium">{label}</p>
		{#if hint}<p class="text-xs text-muted-foreground">{hint}</p>{/if}
	</Table.Cell>
	<Table.Cell class="py-4">
		<div class="flex flex-col gap-1.5">
			<ModelSelect
				id="{id}-model"
				label="{label} model"
				bind:value={value.model}
				{models}
				inherit={!isDefault}
				{disabled}
				invalid={!!modelError}
				onchange={() => onedit(`${field}.model`)}
			/>
			{#if stale}
				<Badge variant="outline" class="border-amber-300/30 text-amber-200">
					<TriangleAlertIcon /> not available on server
				</Badge>
			{/if}
			{#if modelError}
				<p id="{id}-model-error" class="text-sm text-destructive">{modelError}</p>
			{/if}
		</div>
	</Table.Cell>
	<Table.Cell class="py-4">
		<div class="flex flex-col gap-1.5">
			<Input
				id="{id}-temperature"
				type="number"
				inputmode="decimal"
				min="0"
				max="2"
				step="0.1"
				class="w-24"
				placeholder={isDefault ? String(DEFAULT_TEMPERATURE) : 'Inherit'}
				aria-label="{label} temperature"
				aria-invalid={temperatureError ? true : undefined}
				aria-describedby={temperatureError ? `${id}-temperature-error` : undefined}
				{disabled}
				value={value.temperature}
				oninput={(e) => {
					value.temperature = e.currentTarget.value;
					onedit(`${field}.temperature`);
				}}
			/>
			{#if temperatureError}
				<p id="{id}-temperature-error" class="text-sm text-destructive">{temperatureError}</p>
			{/if}
		</div>
	</Table.Cell>
	<Table.Cell class="py-4 pr-5 text-sm text-muted-foreground">
		{#if isDefault}
			<span class="opacity-60">—</span>
		{:else if effective}
			<span class="tabular-nums">→ {effective.model} · {effective.temperature}</span>
		{:else}
			<span class="opacity-60">needs a default model</span>
		{/if}
	</Table.Cell>
</Table.Row>
