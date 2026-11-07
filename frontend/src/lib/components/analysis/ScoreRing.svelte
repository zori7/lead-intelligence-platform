<script lang="ts">
	import type { Tier } from '#lib/api/types.js';
	import { countUp, drawRing, fadeIn } from '#lib/motion/attachments.js';
	import { tierLabel, tierTone } from './labels.js';

	let { value, tier }: { value: number; tier: Tier } = $props();

	const radius = 52;
	const circumference = 2 * Math.PI * radius;
	const offset = $derived(circumference * (1 - Math.max(0, Math.min(100, value)) / 100));
</script>

<!-- The score is revealed by drawing the ring while the number counts up (900ms). -->
<div class="relative grid size-32 shrink-0 place-items-center">
	<svg viewBox="0 0 120 120" class="absolute inset-0 -rotate-90" aria-hidden="true">
		<circle
			cx="60"
			cy="60"
			r={radius}
			fill="none"
			stroke-width="10"
			class="stroke-foreground/[0.07]"
		/>
		<circle
			cx="60"
			cy="60"
			r={radius}
			fill="none"
			stroke-width="10"
			stroke-linecap="round"
			stroke-dasharray={circumference}
			stroke-dashoffset={offset}
			class={tierTone[tier].stroke}
			{@attach drawRing(offset)}
		/>
	</svg>
	<div
		class="flex flex-col items-center"
		role="img"
		aria-label="Score {value} of 100, {tierLabel[tier]}"
	>
		<span class="text-3xl font-semibold tabular-nums" {@attach countUp(value)}>{value}</span>
		<span
			class="text-xs font-medium tracking-wide uppercase {tierTone[tier].text}"
			{@attach fadeIn({ y: 4, delay: 0.5 })}
		>
			{tierLabel[tier]}
		</span>
	</div>
</div>
