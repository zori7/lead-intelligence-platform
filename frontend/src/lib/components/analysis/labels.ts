import type { JobStatus, Priority, StepName, Tier, WebsiteFetch } from '#lib/api/types.js';

export const stepLabel: Record<StepName, string> = {
	validate_input: 'Validate input',
	fetch_website: 'Read website',
	extract_company: 'Extract company profile',
	analyze_fit: 'Analyze ICP fit',
	analyze_intent: 'Analyze buying intent',
	score_lead: 'Score lead',
	generate_recommendations: 'Generate recommendations',
	aggregate: 'Assemble result'
};

export const jobStatusLabel: Record<JobStatus, string> = {
	queued: 'Queued',
	running: 'Running',
	succeeded: 'Succeeded',
	failed: 'Failed',
	cancelled: 'Cancelled'
};

export const tierLabel: Record<Tier, string> = { hot: 'Hot', warm: 'Warm', cold: 'Cold' };

// Full class strings (not interpolated) so Tailwind can see them.
// Tier colors: hot = rose, warm = amber, cold = slate-blue (SPEC.md §10.5).
export const tierTone: Record<Tier, { text: string; stroke: string; pill: string }> = {
	hot: {
		text: 'text-rose-300',
		stroke: 'stroke-rose-400',
		pill: 'bg-rose-400/10 text-rose-200 ring-rose-300/25'
	},
	warm: {
		text: 'text-amber-300',
		stroke: 'stroke-amber-400',
		pill: 'bg-amber-400/10 text-amber-200 ring-amber-300/25'
	},
	cold: {
		text: 'text-slate-300',
		stroke: 'stroke-slate-400',
		pill: 'bg-slate-400/10 text-slate-200 ring-slate-300/25'
	}
};

export const priorityTone: Record<Priority, string> = {
	high: 'bg-rose-400/10 text-rose-200 ring-rose-300/25',
	medium: 'bg-amber-400/10 text-amber-200 ring-amber-300/25',
	low: 'bg-slate-400/10 text-slate-200 ring-slate-300/25'
};

/** Where the company insights came from, shown on the insights card. */
export function insightsSource(website: WebsiteFetch): { label: string; detail: string } {
	switch (website.status) {
		case 'fetched':
			return { label: 'From website + AI', detail: hostOf(website.final_url) };
		case 'not_provided':
			return { label: 'AI-inferred', detail: 'no website' };
		case 'blocked':
			return { label: 'AI-inferred', detail: 'website disallows automated access' };
		case 'disabled':
			return { label: 'AI-inferred', detail: 'website reading is turned off' };
		case 'failed':
			return { label: 'AI-inferred', detail: 'website could not be read' };
	}
}

function hostOf(url: string | null): string {
	if (!url) return '';
	try {
		return new URL(url).host;
	} catch {
		return url;
	}
}

export const percent = (fraction: number) => `${Math.round(fraction * 100)}%`;
