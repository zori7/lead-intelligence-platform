<script lang="ts">
	import { goto } from '$app/navigation';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import LeadForm from '#lib/components/leads/LeadForm.svelte';
	import LeadStatusSelect from '#lib/components/leads/LeadStatusSelect.svelte';
	import LeadStatusBadge from '#lib/components/leads/LeadStatusBadge.svelte';
	import CompanyAvatar from '#lib/components/leads/CompanyAvatar.svelte';
	import { formatDateTime } from '#lib/components/leads/labels.js';
	import { ApiError } from '#lib/api/client.js';
	import { deleteLeadMutation, leadQuery, updateStatusMutation } from '#lib/api/queries.js';
	import type { LeadStatus } from '#lib/api/types.js';
	import type { PageProps } from './$types';

	let { params }: PageProps = $props();

	const qc = useQueryClient();
	const detail = createQuery(() => leadQuery(params.id));
	const statusChange = createMutation(() => updateStatusMutation(qc, params.id));
	const remove = createMutation(() => deleteLeadMutation(qc, params.id));

	let editOpen = $state(false);
	let confirmOpen = $state(false);

	const lead = $derived(detail.data?.lead);
	const notFound = $derived(detail.error instanceof ApiError && detail.error.status === 404);

	function changeStatus(status: LeadStatus) {
		statusChange.mutate(status, {
			onError: (err) => toast.error(`Status not saved: ${err.message}`)
		});
	}

	async function confirmDelete() {
		try {
			await remove.mutateAsync();
			confirmOpen = false;
			toast.success('Lead deleted');
			await goto('/');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Delete failed.');
		}
	}
</script>

<div class="flex flex-col gap-6">
	<Button href="/" variant="ghost" size="sm" class="w-fit"><ArrowLeftIcon /> All leads</Button>

	{#if detail.isPending}
		<Skeleton class="h-24 w-full rounded-2xl bg-white/[0.05]" />
		<Skeleton class="h-48 w-full rounded-2xl bg-white/[0.05]" />
	{:else if notFound}
		<div class="flex flex-col items-center gap-3 py-16 text-center">
			<p class="text-lg font-medium">Lead not found</p>
			<p class="text-muted-foreground">It may have been deleted.</p>
		</div>
	{:else if detail.isError}
		<div class="flex flex-col items-center gap-3 py-16 text-center">
			<p class="text-muted-foreground">Could not load lead: {detail.error.message}</p>
			<Button variant="outline" onclick={() => detail.refetch()}>Try again</Button>
		</div>
	{:else if lead}
		<Card.Root class="glow-edge">
			<Card.Content class="flex-row flex-wrap items-center justify-between gap-4">
				<div class="flex items-center gap-4">
					<CompanyAvatar name={lead.company_name} class="size-14 text-base" />
					<div>
						<div class="flex flex-wrap items-center gap-3">
							<h1 class="text-2xl font-semibold tracking-tight">{lead.company_name}</h1>
							<LeadStatusBadge status={lead.status} />
						</div>
						{#if lead.website}
							<a
								href={lead.website}
								target="_blank"
								rel="noopener noreferrer"
								class="text-sm text-sky-300/80 hover:text-sky-200 hover:underline">{lead.website}</a
							>
						{/if}
					</div>
				</div>
				<div class="flex flex-wrap items-center gap-2">
					<LeadStatusSelect value={lead.status} onchange={changeStatus} />
					<Button variant="outline" onclick={() => (editOpen = true)}><PencilIcon /> Edit</Button>
					<Button variant="outline" onclick={() => (confirmOpen = true)}>
						<Trash2Icon /> Delete
					</Button>
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header><Card.Title class="text-base">Details</Card.Title></Card.Header>
			<Card.Content>
				<dl class="grid gap-x-8 gap-y-5 sm:grid-cols-2 lg:grid-cols-3">
					{#each [['Contact', lead.contact_name], ['Title', lead.contact_title], ['Email', lead.contact_email], ['Industry', lead.industry], ['Company size', lead.company_size], ['Created', formatDateTime(lead.created_at)], ['Updated', formatDateTime(lead.updated_at)]] as [label, value] (label)}
						<div>
							<dt class="text-sm text-muted-foreground">{label}</dt>
							<dd>{value ?? '—'}</dd>
						</div>
					{/each}
				</dl>
				{#if lead.notes}
					<div class="mt-6">
						<p class="text-sm text-muted-foreground">Notes</p>
						<p class="whitespace-pre-wrap">{lead.notes}</p>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>

		<LeadForm bind:open={editOpen} {lead} />

		<AlertDialog.Root bind:open={confirmOpen}>
			<AlertDialog.Content>
				<AlertDialog.Header>
					<AlertDialog.Title>Delete {lead.company_name}?</AlertDialog.Title>
					<AlertDialog.Description>
						This permanently removes the lead and its analysis history.
					</AlertDialog.Description>
				</AlertDialog.Header>
				<AlertDialog.Footer>
					<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
					<Button variant="destructive" disabled={remove.isPending} onclick={confirmDelete}>
						{remove.isPending ? 'Deleting…' : 'Delete'}
					</Button>
				</AlertDialog.Footer>
			</AlertDialog.Content>
		</AlertDialog.Root>
	{/if}
</div>
