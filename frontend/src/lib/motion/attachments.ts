// Svelte attachments for the motion moments of SPEC.md §10.5. Use them as
// `{@attach fadeIn()}`; Svelte runs the returned cleanup when the element is
// removed or the attachment's arguments change. Each one is a no-op when the
// user prefers reduced motion (see withMotion).
import type { Attachment } from 'svelte/attachments';
import { durations, easings, gsap, withMotion } from './gsap.js';

// Only the properties tweened here are cleared afterwards, so inline styles set
// by Svelte (`style:width`, …) survive.
const CLEAR = 'opacity,transform';

type FadeOptions = { y?: number; duration?: number; delay?: number };

/** Fades the element in while it rises `y` pixels into place. */
export function fadeIn({
	y = 8,
	duration = durations.base,
	delay = 0
}: FadeOptions = {}): Attachment<HTMLElement> {
	return (node) =>
		withMotion(node, () => {
			gsap.from(node, { opacity: 0, y, duration, delay, ease: easings.out, clearProps: CLEAR });
		});
}

/** Fades the element's direct children in one after another (at most `max`). */
export function staggerChildren({
	y = 8,
	duration = durations.base,
	delay = 0,
	stagger = 0.06,
	max = 12
}: FadeOptions & { stagger?: number; max?: number } = {}): Attachment<HTMLElement> {
	return (node) =>
		withMotion(node, () => {
			const items = Array.from(node.children).slice(0, max);
			if (items.length === 0) return;
			gsap.from(items, {
				opacity: 0,
				y,
				duration,
				delay,
				stagger,
				ease: easings.out,
				clearProps: CLEAR
			});
		});
}

/** Scales a small element (an icon) in from nothing: a state change landing. */
export function popIn({ duration = durations.base } = {}): Attachment<Element> {
	return (node) =>
		withMotion(node, () => {
			gsap.from(node, {
				opacity: 0,
				scale: 0.4,
				rotate: -30,
				duration,
				ease: easings.expo,
				transformOrigin: '50% 50%',
				clearProps: CLEAR
			});
		});
}

/**
 * Counts the element's text up from 0 to `value`. The element's only child must
 * be the text node Svelte renders `{value}` into: the tween edits that node in
 * place, so Svelte keeps ownership of it, and the final text is always `value`.
 */
export function countUp(value: number, { duration = durations.reveal } = {}): Attachment {
	return (node) =>
		withMotion(node, () => {
			const text = node.firstChild;
			if (!(text instanceof Text)) return;
			const final = String(value);
			const counter = { n: 0 };
			text.nodeValue = '0';
			gsap.to(counter, {
				n: value,
				duration,
				ease: easings.expo,
				onUpdate: () => {
					text.nodeValue = String(Math.round(counter.n));
				}
			});
			return () => {
				text.nodeValue = final;
			};
		});
}

/**
 * Draws an SVG ring stroke from empty to `offset` (its final stroke-dashoffset;
 * the empty ring's offset is its stroke-dasharray, the circumference).
 */
export function drawRing(
	offset: number,
	{ duration = durations.reveal } = {}
): Attachment<SVGElement> {
	return (node) =>
		withMotion(node, () => {
			const circumference = Number.parseFloat(node.getAttribute('stroke-dasharray') ?? '');
			if (!Number.isFinite(circumference)) return;
			gsap.fromTo(
				node,
				{ attr: { 'stroke-dashoffset': circumference } },
				{ attr: { 'stroke-dashoffset': offset }, duration, ease: easings.expo }
			);
		});
}
