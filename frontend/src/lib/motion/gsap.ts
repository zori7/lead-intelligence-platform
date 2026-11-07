import { gsap } from 'gsap';

export { gsap };

/** Media queries for the user's motion preference. */
export const MOTION_OK = '(prefers-reduced-motion: no-preference)';
export const REDUCED_MOTION = '(prefers-reduced-motion: reduce)';

/** Motion scale (SPEC.md §10.5): restrained, 150–450ms, plus the 900ms score reveal. */
export const durations = { fast: 0.15, base: 0.3, slow: 0.45, reveal: 0.9 } as const;
export const easings = { out: 'power2.out', expo: 'expo.out' } as const;

/**
 * Runs `setup` only while the user allows motion, scoped to `scope`.
 *
 * `gsap.matchMedia` wraps `gsap.context`, so every tween created in `setup` is
 * recorded and reverted together: by the returned cleanup (the element left the
 * DOM) or when the OS setting switches to "reduce motion". With reduced motion
 * nothing runs, so content is shown in its final state immediately and never
 * depends on an animation finishing. `setup` may return extra cleanup for state
 * GSAP does not own (e.g. text it wrote).
 */
export function withMotion(scope: Element, setup: () => void | (() => void)): () => void {
	const mm = gsap.matchMedia(scope);
	mm.add(MOTION_OK, () => setup());
	return () => mm.revert();
}
