<script lang="ts">
	import * as Select from '#lib/components/ui/select/index.js';

	let {
		id,
		value = $bindable(),
		models,
		inherit = false,
		disabled = false,
		invalid = false,
		label,
		onchange
	}: {
		id: string;
		/** Selected model; null means "Inherit default" (or nothing chosen without `inherit`). */
		value: string | null;
		/** Models on the LLM server; undefined while unknown (loading or unreachable). */
		models: string[] | undefined;
		/** Offer "Inherit default" as the first option. */
		inherit?: boolean;
		disabled?: boolean;
		invalid?: boolean;
		label: string;
		onchange?: () => void;
	} = $props();

	// Model names are never empty (spec: minLength 1), so '' safely stands for null.
	const selected = $derived(value ?? '');
	// A saved model that is not (or no longer) on the server stays selectable,
	// so the current setting is always visible.
	const options = $derived(
		value !== null && !(models ?? []).includes(value) ? [value, ...(models ?? [])] : (models ?? [])
	);
</script>

<Select.Root
	type="single"
	value={selected}
	onValueChange={(v) => {
		value = v === '' ? null : v;
		onchange?.();
	}}
	{disabled}
>
	<Select.Trigger
		{id}
		class="w-full max-w-72 min-w-44"
		aria-label={label}
		aria-invalid={invalid ? true : undefined}
	>
		<span class={value === null ? 'text-muted-foreground' : 'truncate'}>
			{value ?? (inherit ? 'Inherit default' : 'Choose a model')}
		</span>
	</Select.Trigger>
	<Select.Content class="glass-popover max-h-72">
		{#if inherit}
			<Select.Item value="" label="Inherit default" />
		{/if}
		{#each options as m (m)}
			<Select.Item value={m} label={m} />
		{/each}
		{#if options.length === 0 && !inherit}
			<p class="px-2 py-1.5 text-sm text-muted-foreground">No models available</p>
		{/if}
	</Select.Content>
</Select.Root>
