<script lang="ts">
	import type { LeadStatus } from '#lib/api/types.js';
	import { statusLabel } from './labels.js';

	let { status }: { status: LeadStatus } = $props();

	// Full class strings (not interpolated) so Tailwind can see them. Each status has
	// its own hue; the light theme uses darker shades for contrast on white.
	const tone: Record<LeadStatus, { pill: string; dot: string }> = {
		new: {
			pill: 'bg-sky-500/10 text-sky-700 ring-sky-600/20 dark:bg-sky-400/10 dark:text-sky-200 dark:ring-sky-300/25',
			dot: 'bg-sky-500 shadow-sky-500/50 dark:bg-sky-300 dark:shadow-sky-300/70'
		},
		contacted: {
			pill: 'bg-indigo-500/10 text-indigo-700 ring-indigo-600/20 dark:bg-indigo-400/10 dark:text-indigo-200 dark:ring-indigo-300/25',
			dot: 'bg-indigo-500 shadow-indigo-500/50 dark:bg-indigo-300 dark:shadow-indigo-300/70'
		},
		qualified: {
			pill: 'bg-violet-500/10 text-violet-700 ring-violet-600/20 dark:bg-violet-400/10 dark:text-violet-200 dark:ring-violet-300/25',
			dot: 'bg-violet-500 shadow-violet-500/50 dark:bg-violet-300 dark:shadow-violet-300/70'
		},
		proposal: {
			pill: 'bg-amber-500/10 text-amber-700 ring-amber-600/20 dark:bg-amber-400/10 dark:text-amber-200 dark:ring-amber-300/25',
			dot: 'bg-amber-500 shadow-amber-500/50 dark:bg-amber-300 dark:shadow-amber-300/70'
		},
		won: {
			pill: 'bg-emerald-500/10 text-emerald-700 ring-emerald-600/20 dark:bg-emerald-400/10 dark:text-emerald-200 dark:ring-emerald-300/25',
			dot: 'bg-emerald-500 shadow-emerald-500/50 dark:bg-emerald-300 dark:shadow-emerald-300/70'
		},
		lost: {
			pill: 'bg-rose-500/10 text-rose-700 ring-rose-600/20 dark:bg-rose-400/10 dark:text-rose-200 dark:ring-rose-300/25',
			dot: 'bg-rose-500 shadow-rose-500/50 dark:bg-rose-300 dark:shadow-rose-300/70'
		}
	};
</script>

<span
	class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 transition-[color,background-color,box-shadow] duration-300 ring-inset {tone[
		status
	].pill}"
>
	<span
		class="size-1.5 rounded-full shadow-[0_0_6px] transition-[background-color,box-shadow] duration-300 {tone[
			status
		].dot}"
	></span>
	{statusLabel[status]}
</span>
