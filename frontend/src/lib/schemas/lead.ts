// Lead form validation. The rules (lengths, enums, patterns) come from the
// generated zod schema, i.e. from api/openapi.yaml; this module only adapts raw
// form strings to the API shape and turns zod issues into friendly messages.
// The server re-validates everything and stays authoritative.
import type { core } from 'zod';
import { zLeadCreate } from '#lib/api/gen/zod.gen.js';
import type { CompanySize, LeadCreate, LeadStatus } from '#lib/api/types.js';

/** Raw form state: every text field is a string (empty = not set). */
export interface LeadFormValues {
	company_name: string;
	website: string;
	contact_name: string;
	contact_email: string;
	contact_title: string;
	industry: string;
	company_size: CompanySize | '';
	notes: string;
	status: LeadStatus;
}

export type FieldErrors = Partial<Record<keyof LeadFormValues, string>>;

/** Mirrors server normalization: trim, and an empty optional field becomes null. */
export function toLeadCreate(v: LeadFormValues): LeadCreate {
	const opt = (s: string) => (s.trim() === '' ? null : s.trim());
	return {
		company_name: v.company_name.trim(),
		website: opt(v.website),
		contact_name: opt(v.contact_name),
		contact_email: opt(v.contact_email),
		contact_title: opt(v.contact_title),
		industry: opt(v.industry),
		company_size: v.company_size === '' ? null : v.company_size,
		notes: opt(v.notes),
		status: v.status
	};
}

// The spec's `x-pattern-message` texts (the server returns the same wording).
const patternMessages: Partial<Record<keyof LeadFormValues, string>> = {
	website: 'Must be a valid http(s) URL',
	contact_email: 'Must be a valid email address'
};

function message(field: keyof LeadFormValues, issue: core.$ZodIssue): string {
	switch (issue.code) {
		case 'too_small':
			return issue.minimum === 1 ? 'Required' : `Must be at least ${issue.minimum} characters`;
		case 'too_big':
			return `Must be at most ${issue.maximum} characters`;
		case 'invalid_format':
			return patternMessages[field] ?? 'Has an invalid format';
		case 'invalid_value':
			return `Must be one of: ${issue.values.join(', ')}`;
		default:
			return issue.message;
	}
}

export function validateLead(
	values: LeadFormValues
): { ok: true; data: LeadCreate } | { ok: false; errors: FieldErrors } {
	const res = zLeadCreate.safeParse(toLeadCreate(values));
	if (res.success) return { ok: true, data: res.data as LeadCreate };
	const errors: FieldErrors = {};
	for (const issue of res.error.issues) {
		const field = issue.path[0] as keyof LeadFormValues;
		errors[field] ??= message(field, issue);
	}
	return { ok: false, errors };
}
