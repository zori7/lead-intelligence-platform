<script lang="ts">
	import { mode, toggleMode } from 'mode-watcher';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import SunIcon from '@lucide/svelte/icons/sun';
	import { Button } from '#lib/components/ui/button/index.js';

	const dark = $derived(mode.current !== 'light');
</script>

<Button
	variant="ghost"
	size="icon"
	class="rounded-full"
	aria-label={dark ? 'Switch to light theme' : 'Switch to dark theme'}
	title={dark ? 'Light theme' : 'Dark theme'}
	onclick={toggleMode}
>
	{#if dark}
		<SunIcon />
	{:else}
		<MoonIcon />
	{/if}
</Button>
