import { describe, expect, it } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
	it('renders emphasis, lists and tables', () => {
		const html = renderMarkdown('**Carried** [1]\n\n- one\n- two\n\n| Fund | Balance |\n| --- | --- |\n| Sinking | $1.00 |');
		expect(html).toContain('<strong>Carried</strong>');
		expect(html).toContain('<li>one</li>');
		expect(html).toContain('<table>');
		expect(html).toContain('<td>Sinking</td>');
	});

	it('strips scripts and event handlers', () => {
		const html = renderMarkdown('<script>alert(1)</script><img src=x onerror="alert(1)"><b onclick="x()">hi</b>');
		expect(html).not.toContain('<script');
		expect(html).not.toContain('onerror');
		expect(html).not.toContain('onclick');
		expect(html).toContain('hi');
	});

	it('drops javascript: links and hardens the rest', () => {
		const html = renderMarkdown('[bad](javascript:alert(1)) [good](https://example.com)');
		expect(html).not.toContain('javascript:');
		expect(html).toContain('href="https://example.com"');
		expect(html).toContain('rel="noopener noreferrer"');
	});
});
