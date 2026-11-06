// Client-side lead validation. Mirrors backend/internal/lead/validate.go so most
// errors are caught before a round-trip; the server stays authoritative.
import { z } from 'zod';
import { COMPANY_SIZES, LEAD_STATUSES, type LeadInput } from '#lib/api/types.js';

/** Character count in code points, matching the server (runes) and Postgres char_length. */
const chars = (s: string) => [...s].length;

const maxChars = (max: number) =>
	z.string().refine((v) => chars(v) <= max, `Must be at most ${max} characters`);

const emptyToNull = (v: string) => (v === '' ? null : v);

const optionalText = (max: number) => z.string().trim().pipe(maxChars(max)).transform(emptyToNull);

const isHttpUrl = (v: string) => {
	try {
		const u = new URL(v);
		return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== '';
	} catch {
		return false;
	}
};

const emailRe = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export const leadFormSchema = z.object({
	company_name: z.string().trim().min(1, 'Required').pipe(maxChars(200)),
	website: optionalText(300).refine(
		(v) => v === null || isHttpUrl(v),
		'Must be a valid http(s) URL'
	),
	contact_name: optionalText(200),
	contact_email: optionalText(320).refine(
		(v) => v === null || emailRe.test(v),
		'Must be a valid email address'
	),
	contact_title: optionalText(200),
	industry: optionalText(100),
	company_size: z.enum([...COMPANY_SIZES, '']).transform((v) => (v === '' ? null : v)),
	notes: optionalText(5000),
	status: z.enum(LEAD_STATUSES)
}) satisfies z.ZodType<LeadInput, LeadFormValues>;

/** Raw form state: every field is a string (empty = not set). */
export interface LeadFormValues {
	company_name: string;
	website: string;
	contact_name: string;
	contact_email: string;
	contact_title: string;
	industry: string;
	company_size: (typeof COMPANY_SIZES)[number] | '';
	notes: string;
	status: (typeof LEAD_STATUSES)[number];
}

export type FieldErrors = Partial<Record<keyof LeadFormValues, string>>;

export function validateLead(
	values: LeadFormValues
): { ok: true; data: LeadInput } | { ok: false; errors: FieldErrors } {
	const res = leadFormSchema.safeParse(values);
	if (res.success) return { ok: true, data: res.data };
	const errors: FieldErrors = {};
	for (const issue of res.error.issues) {
		const key = issue.path[0] as keyof LeadFormValues;
		errors[key] ??= issue.message;
	}
	return { ok: false, errors };
}
