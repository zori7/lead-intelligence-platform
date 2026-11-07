import type {
	JobStatus,
	Priority,
	StepName,
	StepState,
	Tier,
	WebsiteFetch
} from '#lib/api/types.js';

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

export const stepStateLabel: Record<StepState, string> = {
	pending: 'pending',
	running: 'running',
	done: 'done',
	failed: 'failed',
	skipped: 'skipped'
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
// The colours are theme tokens defined in routes/layout.css.
export const tierTone: Record<Tier, { text: string; stroke: string; pill: string; dot: string }> = {
	hot: {
		text: 'text-tier-hot',
		stroke: 'stroke-tier-hot',
		pill: 'bg-tier-hot/10 text-tier-hot ring-tier-hot/25',
		dot: 'bg-tier-hot'
	},
	warm: {
		text: 'text-tier-warm',
		stroke: 'stroke-tier-warm',
		pill: 'bg-tier-warm/10 text-tier-warm ring-tier-warm/25',
		dot: 'bg-tier-warm'
	},
	cold: {
		text: 'text-tier-cold',
		stroke: 'stroke-tier-cold',
		pill: 'bg-tier-cold/10 text-tier-cold ring-tier-cold/25',
		dot: 'bg-tier-cold'
	}
};

// Priorities reuse the tier hues: high = rose, medium = amber, low = slate-blue.
export const priorityTone: Record<Priority, string> = {
	high: tierTone.hot.pill,
	medium: tierTone.warm.pill,
	low: tierTone.cold.pill
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
