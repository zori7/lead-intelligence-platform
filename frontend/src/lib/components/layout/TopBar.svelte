<script lang="ts">
	import { page } from '$app/state';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import ThemeToggle from './ThemeToggle.svelte';

	const onLeads = $derived(page.url.pathname === '/' || page.url.pathname.startsWith('/leads'));
</script>

<header class="flex h-16 items-center gap-4 px-4 sm:h-20 sm:gap-8 sm:px-8">
	<a href="/" class="flex items-center gap-2.5 rounded-xl">
		<span class="btn-glow grid size-9 place-items-center rounded-xl" aria-hidden="true">
			<SparklesIcon class="size-4.5" />
		</span>
		<span class="text-lg font-semibold tracking-tight">Lead Intelligence</span>
	</a>
	<nav class="flex items-center gap-1" aria-label="Main">
		<a
			href="/"
			aria-current={onLeads ? 'page' : undefined}
			class={[
				'rounded-full px-5 py-1.5 text-sm font-medium transition-colors',
				onLeads ? 'btn-glow' : 'text-muted-foreground hover:text-foreground'
			]}
		>
			Leads
		</a>
	</nav>
	<div class="ml-auto">
		<ThemeToggle />
	</div>
</header>
