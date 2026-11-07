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
	import { fadeIn } from '#lib/motion/attachments.js';
	import InboxIcon from '@lucide/svelte/icons/inbox';
	import SearchXIcon from '@lucide/svelte/icons/search-x';

	let {
		leads,
		loading,
		filtered,
		oncreate
	}: { leads: Lead[]; loading: boolean; filtered: boolean; oncreate: () => void } = $props();

	const open = (id: string) => goto(`/leads/${id}`);

	// The first rows to appear fade up in a short stagger; rows that appear later
	// (filtering, paging) render at once. Deliberately not $state: it is read once
	// per row as the row mounts and must not re-run attachments when it changes.
	const INTRO_ROWS = 12;
	let introPending = true;
	function rowIntro(index: number) {
		if (!introPending || index >= INTRO_ROWS) return undefined;
		// The whole first batch mounts in one flush; close the window right after it.
		queueMicrotask(() => (introPending = false));
		return fadeIn({ y: 8, delay: index * 0.03 });
	}
</script>

<!-- Fixed column widths: the columns must not resize when filtering changes the rows.
     Below the minimum width the table scrolls horizontally instead of squeezing. -->
<Table.Root class="min-w-[50rem] table-fixed">
	<Table.Header>
		<Table.Row class="hover:bg-transparent">
			<Table.Head class="h-12 w-[30%] pl-5 text-muted-foreground">Company</Table.Head>
			<Table.Head class="w-[24%] text-muted-foreground">Contact</Table.Head>
			<Table.Head class="w-[18%] text-muted-foreground">Score</Table.Head>
			<Table.Head class="w-[14%] text-muted-foreground">Status</Table.Head>
			<Table.Head class="w-[14%] pr-5 text-muted-foreground">Last analyzed</Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#if loading}
			{#each { length: 5 }, i (i)}
				<Table.Row class="hover:bg-transparent">
					<Table.Cell class="py-4 pl-5">
						<div class="flex items-center gap-3">
							<Skeleton class="size-9 rounded-full" />
							<Skeleton class="h-4 w-32" />
						</div>
					</Table.Cell>
					{#each { length: 4 }, j (j)}
						<Table.Cell><Skeleton class="h-4 w-full max-w-28" /></Table.Cell>
					{/each}
				</Table.Row>
			{/each}
		{:else if leads.length === 0}
			<Table.Row class="hover:bg-transparent">
				<Table.Cell colspan={5} class="py-16">
					<div class="flex flex-col items-center gap-3 text-center whitespace-normal">
						<span
							class="grid size-12 place-items-center rounded-2xl bg-foreground/[0.05] text-muted-foreground ring-1 ring-foreground/10 ring-inset"
						>
							{#if filtered}<SearchXIcon class="size-5" />{:else}<InboxIcon class="size-5" />{/if}
						</span>
						{#if filtered}
							<p class="font-medium">No leads match these filters</p>
							<p class="text-sm text-muted-foreground">Try another status or search term.</p>
						{:else}
							<p class="font-medium">No leads yet</p>
							<p class="max-w-sm text-sm text-muted-foreground">
								Add a company to start tracking it; the AI analysis can then score it for you.
							</p>
							<Button class="btn-glow mt-1" onclick={oncreate}>Create your first lead</Button>
						{/if}
					</div>
				</Table.Cell>
			</Table.Row>
		{:else}
			{#each leads as lead, i (lead.id)}
				<Table.Row
					{@attach rowIntro(i)}
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
									class="block truncate font-medium group-hover:text-info"
									title={lead.company_name}
									onclick={(e) => e.stopPropagation()}>{lead.company_name}</a
								>
								{#if lead.industry}
									<div class="truncate text-xs text-muted-foreground">{lead.industry}</div>
								{/if}
							</div>
						</div>
					</Table.Cell>
					<Table.Cell>
						<div class="truncate">{lead.contact_name ?? '—'}</div>
						{#if lead.contact_title}
							<div class="truncate text-xs text-muted-foreground">{lead.contact_title}</div>
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
