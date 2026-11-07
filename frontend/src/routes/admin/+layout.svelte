<script lang="ts">
	import { adminSession } from '#lib/admin-token.svelte.js';
	import AdminTokenGate from '#lib/components/admin/AdminTokenGate.svelte';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<svelte:head>
	<title>Admin · Lead Intelligence</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<!-- Nothing under /admin renders until the token is verified. -->
{#if adminSession.token !== null}
	{@render children()}
{:else}
	<AdminTokenGate />
{/if}
