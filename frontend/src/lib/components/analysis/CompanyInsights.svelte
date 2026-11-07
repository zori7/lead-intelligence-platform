<script lang="ts">
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import * as Card from '#lib/components/ui/card/index.js';
	import type { CompanyInsights, WebsiteFetch } from '#lib/api/types.js';
	import { insightsSource, percent } from './labels.js';

	let { company, website }: { company: CompanyInsights; website: WebsiteFetch } = $props();

	const source = $derived(insightsSource(website));
</script>

<Card.Root>
	<Card.Header>
		<Card.Title class="text-base">Company insights</Card.Title>
		<Card.Description class="flex flex-wrap items-center gap-1.5">
			{#if website.status === 'fetched'}
				<GlobeIcon class="size-3.5 text-sky-300" />
			{:else}
				<SparklesIcon class="size-3.5" />
			{/if}
			<span class="font-medium text-foreground/90">{source.label}</span>
			{#if source.detail}<span>· {source.detail}</span>{/if}
			<span class="ml-auto">Confidence {percent(company.confidence)}</span>
		</Card.Description>
	</Card.Header>
	<Card.Content class="flex flex-col gap-5">
		<p class="leading-relaxed">{company.description}</p>
		<dl class="grid gap-x-8 gap-y-4 sm:grid-cols-3">
			{#each [['Industry', company.industry], ['Size', company.size_estimate], ['Business model', company.business_model]] as [label, value] (label)}
				<div>
					<dt class="text-sm text-muted-foreground">{label}</dt>
					<dd>{value || '—'}</dd>
				</div>
			{/each}
		</dl>
		{#if company.key_needs.length > 0}
			<div>
				<p class="mb-2 text-sm text-muted-foreground">Key needs</p>
				<ul class="flex flex-wrap gap-2">
					{#each company.key_needs as need (need)}
						<li
							class="rounded-full bg-white/[0.05] px-3 py-1 text-xs ring-1 ring-white/[0.08] ring-inset"
						>
							{need}
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</Card.Content>
</Card.Root>
