<script lang="ts">
	import './layout.css';
	import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import { Toaster } from '#lib/components/ui/sonner/index.js';
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

<QueryClientProvider client={queryClient}>
	<div class="min-h-dvh p-2 sm:p-6">
		<div
			class="glass-frame mx-auto min-h-[calc(100dvh-1rem)] max-w-7xl rounded-[1.75rem] sm:min-h-[calc(100dvh-3rem)]"
		>
			<header class="flex h-16 items-center gap-8 px-4 sm:h-20 sm:px-8">
				<a href="/" class="flex items-center gap-2.5">
					<span
						class="grid size-9 place-items-center rounded-xl bg-gradient-to-br from-sky-400 to-indigo-600 shadow-[inset_0_1px_0_oklch(1_0_0/0.4),0_6px_20px_-6px_oklch(0.62_0.2_260/0.9)]"
					>
						<SparklesIcon class="size-4.5 text-white" />
					</span>
					<span class="text-lg font-semibold tracking-tight">Lead Intelligence</span>
				</a>
				<nav class="hidden items-center gap-1 sm:flex" aria-label="Main">
					<a
						href="/"
						aria-current="page"
						class="btn-glow rounded-full px-5 py-1.5 text-sm font-medium"
					>
						Leads
					</a>
				</nav>
			</header>
			<main class="px-4 pb-10 sm:px-8">
				{@render children()}
			</main>
		</div>
	</div>
	<Toaster theme="dark" richColors closeButton />
</QueryClientProvider>
