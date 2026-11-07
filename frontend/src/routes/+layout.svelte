<script lang="ts">
	import './layout.css';
	import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '#lib/components/ui/sonner/index.js';
	import AppShell from '#lib/components/layout/AppShell.svelte';
	import favicon from '#lib/assets/favicon.svg';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { staleTime: 10_000, refetchOnWindowFocus: true }
		}
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>Lead Intelligence</title>
</svelte:head>

<!-- static/theme-init.js applies the saved mode before render; no inline script. -->
<ModeWatcher defaultMode="dark" disableHeadScriptInjection />

<QueryClientProvider client={queryClient}>
	<AppShell>
		{@render children()}
	</AppShell>
	<Toaster richColors closeButton />
</QueryClientProvider>
