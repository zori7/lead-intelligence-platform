<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import type { Lead } from '#lib/api/types.js';
	import LeadStatusBadge from './LeadStatusBadge.svelte';
	import CompanyAvatar from './CompanyAvatar.svelte';
	import ScoreBar from './ScoreBar.svelte';
	import { formatDate } from './labels.js';

	let {
		leads,
		loading,
		filtered,
		oncreate
	}: { leads: Lead[]; loading: boolean; filtered: boolean; oncreate: () => void } = $props();

	const open = (id: string) => goto(`/leads/${id}`);
</script>

<Table.Root>
	<Table.Header>
		<Table.Row class="hover:bg-transparent">
			<Table.Head class="h-12 pl-5 text-muted-foreground">Company</Table.Head>
			<Table.Head class="text-muted-foreground">Contact</Table.Head>
			<Table.Head class="text-muted-foreground">Score</Table.Head>
			<Table.Head class="text-muted-foreground">Status</Table.Head>
			<Table.Head class="pr-5 text-muted-foreground">Last analyzed</Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#if loading}
			{#each { length: 5 }, i (i)}
				<Table.Row class="hover:bg-transparent">
					<Table.Cell class="py-4 pl-5">
						<div class="flex items-center gap-3">
							<Skeleton class="size-9 rounded-full bg-white/[0.06]" />
							<Skeleton class="h-4 w-32 bg-white/[0.06]" />
						</div>
					</Table.Cell>
					{#each { length: 4 }, j (j)}
						<Table.Cell><Skeleton class="h-4 w-full max-w-28 bg-white/[0.06]" /></Table.Cell>
					{/each}
				</Table.Row>
			{/each}
		{:else if leads.length === 0}
			<Table.Row class="hover:bg-transparent">
				<Table.Cell colspan={5} class="py-16 text-center">
					{#if filtered}
						<p class="text-muted-foreground">No leads match these filters.</p>
					{:else}
						<p class="mb-4 text-muted-foreground">No leads yet.</p>
						<Button onclick={oncreate}>Create your first lead</Button>
					{/if}
				</Table.Cell>
			</Table.Row>
		{:else}
			{#each leads as lead (lead.id)}
				<Table.Row
					class="group cursor-pointer"
					tabindex={0}
					onclick={() => open(lead.id)}
					onkeydown={(e: KeyboardEvent) => {
						if (e.key === 'Enter') open(lead.id);
					}}
				>
					<Table.Cell class="py-3.5 pl-5">
						<div class="flex items-center gap-3">
							<CompanyAvatar name={lead.company_name} />
							<div class="min-w-0">
								<a
									href="/leads/{lead.id}"
									class="font-medium group-hover:text-sky-200"
									onclick={(e) => e.stopPropagation()}>{lead.company_name}</a
								>
								{#if lead.industry}
									<div class="text-xs text-muted-foreground">{lead.industry}</div>
								{/if}
							</div>
						</div>
					</Table.Cell>
					<Table.Cell>
						{lead.contact_name ?? '—'}
						{#if lead.contact_title}
							<div class="text-xs text-muted-foreground">{lead.contact_title}</div>
						{/if}
					</Table.Cell>
					<Table.Cell><ScoreBar score={lead.ai_score} /></Table.Cell>
					<Table.Cell><LeadStatusBadge status={lead.status} /></Table.Cell>
					<Table.Cell class="pr-5 text-muted-foreground"
						>{formatDate(lead.last_analyzed_at)}</Table.Cell
					>
				</Table.Row>
			{/each}
		{/if}
	</Table.Body>
</Table.Root>
