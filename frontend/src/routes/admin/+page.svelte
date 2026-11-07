<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import LockIcon from '@lucide/svelte/icons/lock';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { adminSession } from '#lib/admin-token.svelte.js';
	import { llmSettingsQuery } from '#lib/api/queries.js';
	import ModelSettingsForm from '#lib/components/admin/ModelSettingsForm.svelte';

	const settings = createQuery(() => llmSettingsQuery());
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-wrap items-end justify-between gap-4 pt-2">
		<div>
			<h1 class="text-3xl font-semibold tracking-tight">Model settings</h1>
			<p class="mt-1 text-sm text-muted-foreground">Choose the LLM that runs each analysis step.</p>
		</div>
		<Button variant="outline" class="h-10 rounded-xl px-5" onclick={() => adminSession.lock()}>
			<LockIcon /> Lock
		</Button>
	</div>

	{#if settings.data}
		<ModelSettingsForm settings={settings.data} />
	{:else if settings.isError}
		<div class="flex flex-col items-center gap-3 py-16 text-center">
			<p class="text-muted-foreground">Could not load settings: {settings.error.message}</p>
			<Button variant="outline" onclick={() => settings.refetch()}>Try again</Button>
		</div>
	{:else}
		<Skeleton class="h-96 w-full rounded-2xl" />
	{/if}
</div>
