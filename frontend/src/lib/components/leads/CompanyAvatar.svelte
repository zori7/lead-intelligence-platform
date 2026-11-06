<script lang="ts">
	import { cn } from '#lib/utils.js';

	let { name, class: className = '' }: { name: string; class?: string } = $props();

	const initials = $derived(
		name
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((w) => [...w][0]?.toUpperCase() ?? '')
			.join('')
	);

	// Stable hue per company within the cool part of the palette (blue → violet → cyan).
	const hue = $derived.by(() => {
		let h = 0;
		for (const ch of name) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
		return 200 + (h % 110);
	});
</script>

<span
	aria-hidden="true"
	class={cn(
		'inline-flex size-9 shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white shadow-[inset_0_1px_0_oklch(1_0_0/0.3),0_4px_12px_-4px_oklch(0_0_0/0.6)] ring-1 ring-white/20',
		className
	)}
	style:background="linear-gradient(145deg, oklch(0.72 0.15 {hue}), oklch(0.45 0.18 {hue + 25}))"
>
	{initials}
</span>
