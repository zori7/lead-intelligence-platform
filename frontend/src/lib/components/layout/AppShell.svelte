<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { fadeIn } from '#lib/motion/attachments.js';
	import TopBar from './TopBar.svelte';

	let { children }: { children: Snippet } = $props();
</script>

<div class="min-h-dvh p-2 sm:p-6">
	<div
		class="glass-frame mx-auto min-h-[calc(100dvh-1rem)] max-w-7xl rounded-[1.75rem] sm:min-h-[calc(100dvh-3rem)]"
	>
		<TopBar />
		<main class="px-4 pb-10 sm:px-8">
			<!-- Page transition: each route fades in with a short rise. Query-string
			     changes (filters, paging) keep the same page and do not animate. -->
			{#key page.url.pathname}
				<div {@attach fadeIn({ y: 6, duration: 0.2 })}>
					{@render children()}
				</div>
			{/key}
		</main>
	</div>
</div>
