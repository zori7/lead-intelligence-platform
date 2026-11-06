<script lang="ts">
	import { page } from '$app/state';
	import { Button } from '#lib/components/ui/button/index.js';
</script>

<div class="flex flex-col items-center gap-4 py-24 text-center">
	<p class="text-5xl font-semibold tabular-nums">{page.status}</p>
	<p class="text-muted-foreground">
		{page.status === 404
			? 'This page does not exist.'
			: (page.error?.message ?? 'Something went wrong.')}
	</p>
	<Button href="/">Back to leads</Button>
</div>
