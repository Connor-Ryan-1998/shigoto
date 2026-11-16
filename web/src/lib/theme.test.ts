import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { theme } from './theme.svelte';

// The themes live in app.css as daisyUI theme blocks. Reading the source
// (rather than computed styles, which jsdom doesn't produce from Tailwind)
// pins the palette so a token tweak can't silently drop text contrast.
// Vitest runs from web/ (vite.config.ts) and stubs CSS imports, so read it.
const css = readFileSync(resolve(process.cwd(), 'src/app.css'), 'utf8');

function themeTokens(name: string): Record<string, string> {
	const blocks = css.match(/@plugin\s+['"]daisyui\/theme['"]\s*\{[^}]*\}/g) ?? [];
	const block = blocks.find((b) => new RegExp(`name:\\s*['"]${name}['"]`).test(b));
	if (!block) throw new Error(`theme ${name} not found in app.css`);
	const tokens: Record<string, string> = {};
	for (const [, key, value] of block.matchAll(/(--color-[\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) tokens[key] = value;
	return tokens;
}

function rgb(hex: string): number[] {
	return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
}

function luminance(channels: number[]): number {
	const [r, g, b] = channels.map((c) => {
		const s = c / 255;
		return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
	});
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

// WCAG 2.x contrast; alpha < 1 blends the foreground over the background
// first, matching Tailwind's text-base-content/70 opacity modifier.
function contrast(fg: string, bg: string, alpha = 1): number {
	const back = rgb(bg);
	const front = rgb(fg).map((c, i) => c * alpha + back[i] * (1 - alpha));
	const [hi, lo] = [luminance(front), luminance(back)].sort((a, b) => b - a);
	return (hi + 0.05) / (lo + 0.05);
}

afterEach(() => {
	theme.current = 'system';
	theme.apply();
});

describe('warm-sage themes', () => {
	it('declares light as default and dark for prefers-dark', () => {
		expect(css).toMatch(/name:\s*['"]shigoto-light['"];[^}]*default:\s*true/);
		expect(css).toMatch(/name:\s*['"]shigoto-dark['"];[^}]*prefersdark:\s*true/);
		expect(css).not.toMatch(/corporate|business/);
	});

	it.each(['shigoto-light', 'shigoto-dark'])('%s keeps readable text', (name) => {
		const t = themeTokens(name);
		for (const bg of [t['--color-base-100'], t['--color-base-200']]) {
			expect(contrast(t['--color-base-content'], bg)).toBeGreaterThanOrEqual(4.5);
			expect(contrast(t['--color-base-content'], bg, 0.7)).toBeGreaterThanOrEqual(4.5);
		}
		expect(contrast(t['--color-primary-content'], t['--color-primary'])).toBeGreaterThanOrEqual(4.5);
		expect(contrast(t['--color-primary'], t['--color-base-100'])).toBeGreaterThanOrEqual(4.5);
		expect(contrast(t['--color-warning-content'], t['--color-warning'])).toBeGreaterThanOrEqual(4.5);
		expect(contrast(t['--color-error-content'], t['--color-error'])).toBeGreaterThanOrEqual(4.5);
	});

	it('applies the shigoto theme names', () => {
		theme.current = 'light';
		theme.apply();
		expect(document.documentElement.dataset.theme).toBe('shigoto-light');
		theme.current = 'dark';
		theme.apply();
		expect(document.documentElement.dataset.theme).toBe('shigoto-dark');
		theme.current = 'system';
		theme.apply();
		expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
	});
});
