<script lang="ts">
	import type { LeadStatus } from '#lib/api/types.js';
	import { statusLabel } from './labels.js';

	let { status }: { status: LeadStatus } = $props();

	// Full class strings (not interpolated) so Tailwind can see them.
	const tone: Record<LeadStatus, { pill: string; dot: string }> = {
		new: {
			pill: 'bg-sky-400/10 text-sky-200 ring-sky-300/25',
			dot: 'bg-sky-300 shadow-sky-300/70'
		},
		contacted: {
			pill: 'bg-indigo-400/10 text-indigo-200 ring-indigo-300/25',
			dot: 'bg-indigo-300 shadow-indigo-300/70'
		},
		qualified: {
			pill: 'bg-violet-400/10 text-violet-200 ring-violet-300/25',
			dot: 'bg-violet-300 shadow-violet-300/70'
		},
		proposal: {
			pill: 'bg-amber-400/10 text-amber-200 ring-amber-300/25',
			dot: 'bg-amber-300 shadow-amber-300/70'
		},
		won: {
			pill: 'bg-emerald-400/10 text-emerald-200 ring-emerald-300/25',
			dot: 'bg-emerald-300 shadow-emerald-300/70'
		},
		lost: {
			pill: 'bg-rose-400/10 text-rose-200 ring-rose-300/25',
			dot: 'bg-rose-300 shadow-rose-300/70'
		}
	};
</script>

<span
	class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset {tone[
		status
	].pill}"
>
	<span class="size-1.5 rounded-full shadow-[0_0_6px] {tone[status].dot}"></span>
	{statusLabel[status]}
</span>
