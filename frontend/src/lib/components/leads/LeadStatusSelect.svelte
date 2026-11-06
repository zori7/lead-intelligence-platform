<script lang="ts">
	import * as Select from '#lib/components/ui/select/index.js';
	import { LEAD_STATUSES, type LeadStatus } from '#lib/api/types.js';
	import { statusLabel } from './labels.js';

	let {
		value,
		onchange,
		disabled = false
	}: { value: LeadStatus; onchange: (status: LeadStatus) => void; disabled?: boolean } = $props();
</script>

<Select.Root
	type="single"
	{value}
	{disabled}
	onValueChange={(v) => {
		if (v && v !== value) onchange(v as LeadStatus);
	}}
>
	<Select.Trigger class="w-40" aria-label="Lead status">{statusLabel[value]}</Select.Trigger>
	<Select.Content>
		{#each LEAD_STATUSES as s (s)}
			<Select.Item value={s} label={statusLabel[s]} />
		{/each}
	</Select.Content>
</Select.Root>
