import { describe, expect, it } from 'vitest';
// The same table drives Go's TestResolve (backend/internal/llmsettings).
import cases from '../../../../api/testdata/llm-resolve-cases.json';
import type { LlmSettings } from '#lib/api/types.js';
import {
	fromSettings,
	resolve,
	sameSettings,
	validateSettings,
	type Effective,
	type SettingsInput
} from './llm-settings.js';

const table = cases as unknown as {
	cases: { name: string; settings: SettingsInput; want: Effective | null }[];
};

describe('resolve (shared cases with Go Resolve)', () => {
	it('has cases', () => expect(table.cases.length).toBeGreaterThan(0));
	it.each(table.cases)('$name', ({ settings, want }) => {
		expect(resolve(settings)).toEqual(want);
	});
});

const saved: LlmSettings = {
	default: { model: 'base', temperature: 0.2 },
	nodes: [
		{
			node: 'extract_company',
			model: 'big',
			temperature: null,
			effective_model: 'big',
			effective_temperature: 0.2
		},
		{
			node: 'analyze_fit',
			model: null,
			temperature: null,
			effective_model: 'base',
			effective_temperature: 0.2
		},
		{
			node: 'analyze_intent',
			model: null,
			temperature: 0.4,
			effective_model: 'base',
			effective_temperature: 0.4
		},
		{
			node: 'generate_recommendations',
			model: null,
			temperature: null,
			effective_model: 'base',
			effective_temperature: 0.2
		}
	],
	configured: true,
	updated_at: '2026-01-01T00:00:00Z'
};

describe('validateSettings', () => {
	it('builds the full-replace body', () => {
		const res = validateSettings(fromSettings(saved));
		expect(res).toEqual({
			ok: true,
			data: {
				default: { model: 'base', temperature: 0.2 },
				nodes: {
					extract_company: { model: 'big', temperature: null },
					analyze_fit: { model: null, temperature: null },
					analyze_intent: { model: null, temperature: 0.4 },
					generate_recommendations: { model: null, temperature: null }
				}
			}
		});
	});

	it('requires a default model and keys errors like the server', () => {
		const v = fromSettings(saved);
		v.default.model = null;
		v.nodes.analyze_fit.temperature = '2.5';
		v.nodes.analyze_intent.temperature = 'warm';
		const res = validateSettings(v);
		expect(res.ok).toBe(false);
		if (res.ok) return;
		expect(res.errors).toEqual({
			'default.model': 'Choose a default model',
			'nodes.analyze_fit.temperature': 'Must be at most 2',
			'nodes.analyze_intent.temperature': 'Must be a number'
		});
	});
});

describe('sameSettings', () => {
	it('treats equal numbers written differently as unchanged', () => {
		const a = fromSettings(saved);
		const b = fromSettings(saved);
		b.default.temperature = '0.20';
		expect(sameSettings(a, b)).toBe(true);
		b.nodes.analyze_fit.model = 'big';
		expect(sameSettings(a, b)).toBe(false);
	});
});
