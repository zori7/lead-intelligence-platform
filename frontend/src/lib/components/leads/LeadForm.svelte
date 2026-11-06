<script lang="ts">
	import { untrack } from 'svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import * as Sheet from '#lib/components/ui/sheet/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { ApiError } from '#lib/api/client.js';
	import { createLeadMutation, updateLeadMutation } from '#lib/api/queries.js';
	import { COMPANY_SIZES, LEAD_STATUSES, type Lead, type LeadInput } from '#lib/api/types.js';
	import { validateLead, type FieldErrors, type LeadFormValues } from '#lib/schemas/lead.js';
	import { statusLabel } from './labels.js';

	let {
		open = $bindable(false),
		lead = null,
		onsaved
	}: { open?: boolean; lead?: Lead | null; onsaved?: (lead: Lead) => void } = $props();

	const qc = useQueryClient();
	const create = createMutation(() => createLeadMutation(qc));
	// The edit mutation is bound to the lead being edited; it is only used when `lead` is set.
	const update = createMutation(() => updateLeadMutation(qc, lead?.id ?? ''));

	const toValues = (l: Lead | null): LeadFormValues => ({
		company_name: l?.company_name ?? '',
		website: l?.website ?? '',
		contact_name: l?.contact_name ?? '',
		contact_email: l?.contact_email ?? '',
		contact_title: l?.contact_title ?? '',
		industry: l?.industry ?? '',
		company_size: l?.company_size ?? '',
		notes: l?.notes ?? '',
		status: l?.status ?? 'new'
	});

	let values = $state<LeadFormValues>(toValues(null));
	let errors = $state<FieldErrors>({});

	// Reset the form each time the sheet opens.
	$effect(() => {
		if (open) {
			untrack(() => {
				values = toValues(lead);
				errors = {};
			});
		}
	});

	// A field's error disappears as soon as the user edits that field.
	function clearError(key: keyof LeadFormValues) {
		if (errors[key]) delete errors[key];
	}

	const pending = $derived(create.isPending || update.isPending);
	const isEdit = $derived(lead !== null);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		const result = validateLead(values);
		if (!result.ok) {
			errors = result.errors;
			return;
		}
		errors = {};
		try {
			const saved = isEdit
				? await update.mutateAsync(result.data satisfies LeadInput)
				: await create.mutateAsync(result.data);
			toast.success(isEdit ? 'Lead updated' : 'Lead created');
			open = false;
			onsaved?.(saved);
		} catch (err) {
			if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
				errors = err.fields as FieldErrors;
			} else {
				toast.error(err instanceof Error ? err.message : 'Something went wrong.');
			}
		}
	}

	const textFields = [
		{ key: 'company_name', label: 'Company name', required: true, type: 'text' },
		{ key: 'website', label: 'Website', placeholder: 'https://example.com', type: 'url' },
		{ key: 'industry', label: 'Industry', type: 'text' },
		{ key: 'contact_name', label: 'Contact name', type: 'text' },
		{ key: 'contact_title', label: 'Contact title', type: 'text' },
		{ key: 'contact_email', label: 'Contact email', type: 'email' }
	] as const;
</script>

<Sheet.Root bind:open>
	<Sheet.Content class="w-full overflow-y-auto sm:max-w-lg">
		<Sheet.Header>
			<Sheet.Title>{isEdit ? 'Edit lead' : 'New lead'}</Sheet.Title>
			<Sheet.Description>
				Notes and company details give the AI analysis more to work with.
			</Sheet.Description>
		</Sheet.Header>

		<form id="lead-form" class="grid gap-4 px-4" novalidate onsubmit={submit}>
			{#each textFields as f (f.key)}
				<div class="grid gap-1.5">
					<Label for={f.key}>
						{f.label}{#if 'required' in f}<span class="text-destructive">*</span>{/if}
					</Label>
					<Input
						id={f.key}
						type={f.type}
						placeholder={'placeholder' in f ? f.placeholder : undefined}
						aria-invalid={errors[f.key] ? true : undefined}
						aria-describedby={errors[f.key] ? `${f.key}-error` : undefined}
						bind:value={values[f.key]}
						oninput={() => clearError(f.key)}
					/>
					{#if errors[f.key]}
						<p id="{f.key}-error" class="text-sm text-destructive">{errors[f.key]}</p>
					{/if}
				</div>
			{/each}

			<div class="grid grid-cols-2 gap-4">
				<div class="grid gap-1.5">
					<Label for="company_size">Company size</Label>
					<Select.Root
						type="single"
						bind:value={values.company_size}
						onValueChange={() => clearError('company_size')}
					>
						<Select.Trigger id="company_size" class="w-full">
							{values.company_size || 'Unknown'}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="" label="Unknown" />
							{#each COMPANY_SIZES as s (s)}
								<Select.Item value={s} label={s} />
							{/each}
						</Select.Content>
					</Select.Root>
					{#if errors.company_size}
						<p class="text-sm text-destructive">{errors.company_size}</p>
					{/if}
				</div>
				<div class="grid gap-1.5">
					<Label for="status">Status</Label>
					<Select.Root
						type="single"
						bind:value={values.status}
						onValueChange={() => clearError('status')}
					>
						<Select.Trigger id="status" class="w-full">{statusLabel[values.status]}</Select.Trigger>
						<Select.Content>
							{#each LEAD_STATUSES as s (s)}
								<Select.Item value={s} label={statusLabel[s]} />
							{/each}
						</Select.Content>
					</Select.Root>
					{#if errors.status}
						<p class="text-sm text-destructive">{errors.status}</p>
					{/if}
				</div>
			</div>

			<div class="grid gap-1.5">
				<Label for="notes">Notes</Label>
				<Textarea
					id="notes"
					rows={5}
					aria-invalid={errors.notes ? true : undefined}
					bind:value={values.notes}
					oninput={() => clearError('notes')}
				/>
				{#if errors.notes}
					<p class="text-sm text-destructive">{errors.notes}</p>
				{/if}
			</div>
		</form>

		<Sheet.Footer>
			<Button type="submit" form="lead-form" disabled={pending}>
				{pending ? 'Saving…' : isEdit ? 'Save changes' : 'Create lead'}
			</Button>
			<Sheet.Close>
				{#snippet child({ props })}
					<Button {...props} variant="outline">Cancel</Button>
				{/snippet}
			</Sheet.Close>
		</Sheet.Footer>
	</Sheet.Content>
</Sheet.Root>
