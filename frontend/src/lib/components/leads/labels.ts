import type { LeadSort, LeadStatus } from '#lib/api/types.js';

export const statusLabel: Record<LeadStatus, string> = {
	new: 'New',
	contacted: 'Contacted',
	qualified: 'Qualified',
	proposal: 'Proposal',
	won: 'Won',
	lost: 'Lost'
};

export const sortLabel: Record<LeadSort, string> = {
	created_desc: 'Newest first',
	score_desc: 'Highest score'
};

const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' });
const dateTimeFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

export const formatDate = (iso: string | null) => (iso ? dateFmt.format(new Date(iso)) : '—');
export const formatDateTime = (iso: string | null) =>
	iso ? dateTimeFmt.format(new Date(iso)) : '—';
