// LLM settings form (SPEC.md §7.5, §10.7). Limits come from the generated zod
// schema, i.e. from api/openapi.yaml. This module adapts the form state to the
// API shape, validates it, and computes the effective values shown while the
// form is edited. The server re-validates and its response is authoritative.
import type { core } from 'zod';
import { zLlmSettingsUpdate } from '#lib/api/gen/zod.gen.js';
import {
	LLM_NODES,
	type LlmNode,
	type LlmSettings,
	type LlmSettingsUpdate
} from '#lib/api/types.js';

/** Temperature used when the default row has none (Go: llmsettings.DefaultTemperature). */
export const DEFAULT_TEMPERATURE = 0.2;

// ---- Effective values ---------------------------------------------------------

/** One stored setting. Null inherits the default (or, on the default, means "not set"). */
export interface SettingInput {
	model: string | null;
	temperature: number | null;
}

export interface SettingsInput {
	default: SettingInput;
	nodes: Partial<Record<LlmNode, SettingInput>>;
}

export interface EffectiveSetting {
	model: string;
	temperature: number;
}

export type Effective = Record<LlmNode, EffectiveSetting>;

/**
 * `effective = node override ?? default` for every LLM step; null while no
 * default model is set. Mirrors Go's `llmsettings.Resolve`: both are tested
 * against api/testdata/llm-resolve-cases.json.
 */
export function resolve(s: SettingsInput): Effective | null {
	const defaultModel = s.default.model;
	if (defaultModel === null) return null;
	const defaultTemperature = s.default.temperature ?? DEFAULT_TEMPERATURE;
	const out = {} as Effective;
	for (const node of LLM_NODES) {
		const o = s.nodes[node];
		out[node] = {
			model: o?.model ?? defaultModel,
			temperature: o?.temperature ?? defaultTemperature
		};
	}
	return out;
}

// ---- Form state -----------------------------------------------------------------

/** One table row. `model` null = inherit (default row: none chosen); temperature is the raw input. */
export interface RowValues {
	model: string | null;
	temperature: string;
}

export interface LlmSettingsFormValues {
	default: RowValues;
	nodes: Record<LlmNode, RowValues>;
}

/** Field errors keyed like the server's `fields`, e.g. `nodes.analyze_fit.model`. */
export type FieldErrors = Record<string, string>;

const toRow = (model: string | null, temperature: number | null): RowValues => ({
	model,
	temperature: temperature === null ? '' : String(temperature)
});

export function fromSettings(s: LlmSettings): LlmSettingsFormValues {
	const nodes = {} as Record<LlmNode, RowValues>;
	for (const node of LLM_NODES) {
		const row = s.nodes.find((n) => n.node === node);
		nodes[node] = toRow(row?.model ?? null, row?.temperature ?? null);
	}
	return { default: toRow(s.default.model, s.default.temperature), nodes };
}

/** Empty means "not set"; anything that is not a number becomes NaN (rejected by validation). */
function parseTemperature(text: string): number | null {
	const t = text.trim();
	return t === '' ? null : Number(t);
}

/** The form as stored settings, for the live preview. An invalid temperature counts as unset. */
export function toInput(v: LlmSettingsFormValues): SettingsInput {
	const row = (r: RowValues): SettingInput => {
		const t = parseTemperature(r.temperature);
		return { model: r.model, temperature: t !== null && Number.isFinite(t) ? t : null };
	};
	const nodes: SettingsInput['nodes'] = {};
	for (const node of LLM_NODES) nodes[node] = row(v.nodes[node]);
	return { default: row(v.default), nodes };
}

function toUpdate(v: LlmSettingsFormValues) {
	const row = (r: RowValues) => ({ model: r.model, temperature: parseTemperature(r.temperature) });
	const nodes = {} as Record<LlmNode, ReturnType<typeof row>>;
	for (const node of LLM_NODES) nodes[node] = row(v.nodes[node]);
	return { default: { ...row(v.default), model: v.default.model ?? '' }, nodes };
}

/** True when two form states would save the same settings (`0.20` equals `0.2`). */
export function sameSettings(a: LlmSettingsFormValues, b: LlmSettingsFormValues): boolean {
	const key = (v: LlmSettingsFormValues) =>
		JSON.stringify(toUpdate(v), (_k, x) => (Number.isNaN(x) ? 'NaN' : x));
	return key(a) === key(b);
}

function message(field: string, issue: core.$ZodIssue): string {
	if (field === 'default.model' && issue.code === 'too_small') return 'Choose a default model';
	switch (issue.code) {
		case 'too_small':
			return issue.origin === 'number' ? `Must be at least ${issue.minimum}` : 'Required';
		case 'too_big':
			return issue.origin === 'number'
				? `Must be at most ${issue.maximum}`
				: `Must be at most ${issue.maximum} characters`;
		case 'invalid_type':
			return 'Must be a number';
		default:
			return issue.message;
	}
}

export function validateSettings(
	values: LlmSettingsFormValues
): { ok: true; data: LlmSettingsUpdate } | { ok: false; errors: FieldErrors } {
	const res = zLlmSettingsUpdate.safeParse(toUpdate(values));
	if (res.success) return { ok: true, data: res.data as LlmSettingsUpdate };
	const errors: FieldErrors = {};
	for (const issue of res.error.issues) {
		const field = issue.path.join('.');
		errors[field] ??= message(field, issue);
	}
	return { ok: false, errors };
}
