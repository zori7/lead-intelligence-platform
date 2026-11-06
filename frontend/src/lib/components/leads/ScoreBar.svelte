<script lang="ts">
	let { score }: { score: number | null } = $props();
</script>

{#if score === null}
	<span class="text-xs text-muted-foreground">Not analyzed</span>
{:else}
	<div class="flex w-36 flex-col gap-1">
		<div class="flex justify-between text-xs">
			<span class="text-muted-foreground">Score</span>
			<span class="font-medium tabular-nums">{score}</span>
		</div>
		<div
			class="h-1.5 overflow-hidden rounded-full bg-white/[0.07] shadow-[inset_0_1px_1px_oklch(0_0_0/0.4)]"
			role="meter"
			aria-label="AI score"
			aria-valuemin={0}
			aria-valuemax={100}
			aria-valuenow={score}
		>
			<div
				class="h-full rounded-full bg-gradient-to-r from-indigo-500 via-blue-500 to-sky-300 shadow-[0_0_10px_oklch(0.7_0.18_250/0.7)]"
				style:width="{Math.max(0, Math.min(100, score))}%"
			></div>
		</div>
	</div>
{/if}
