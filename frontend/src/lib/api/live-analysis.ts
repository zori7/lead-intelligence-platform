import type { QueryClient } from '@tanstack/svelte-query';
import { analysisEventsUrl } from './client.js';
import { keys, writeAnalysis } from './queries.js';
import type { AnalysisJob, AnalysisProgressEvent } from './types.js';

const TERMINAL_EVENTS = ['completed', 'failed', 'cancelled'] as const;

/**
 * Opens the job's event stream and writes every event into the TanStack Query
 * cache, so components only ever read `['analysis', jobId]` (SPEC.md §10.3).
 *
 * The server sends a `snapshot` first on every (re)connection, so EventSource's
 * built-in reconnect can never leave the cache stale. Returns a cleanup
 * function that closes the stream, for use as an `$effect` teardown.
 */
export function subscribeToAnalysis(qc: QueryClient, jobId: string): () => void {
	const source = new EventSource(analysisEventsUrl(jobId));
	const parse = <T>(e: Event) => JSON.parse((e as MessageEvent<string>).data) as T;

	source.addEventListener('snapshot', (e) => writeAnalysis(qc, parse<AnalysisJob>(e)));

	source.addEventListener('progress', (e) => {
		const p = parse<AnalysisProgressEvent>(e);
		qc.setQueryData<AnalysisJob>(keys.analysis(jobId), (job) =>
			job
				? {
						...job,
						status: job.status === 'queued' ? 'running' : job.status,
						progress: p.progress,
						steps: job.steps.map((s) => (s.name === p.step ? { ...s, state: p.state } : s))
					}
				: job
		);
	});

	for (const name of TERMINAL_EVENTS) {
		source.addEventListener(name, (e) => {
			// Close first: the server ends the stream next, and EventSource would
			// otherwise treat that as a dropped connection and reconnect.
			source.close();
			const job = parse<AnalysisJob>(e);
			writeAnalysis(qc, job);
			// The lead's score and the history changed with the job.
			void qc.invalidateQueries({ queryKey: keys.lead(job.lead_id) });
			void qc.invalidateQueries({ queryKey: keys.analyses(job.lead_id) });
			void qc.invalidateQueries({ queryKey: keys.leads });
		});
	}

	return () => source.close();
}
