// renderMarkdown turns model-written markdown (strata assistant answers)
// into HTML that is safe to insert with {@html}: marked renders it, then
// DOMPurify strips scripts, event handlers and dangerous URLs, and every
// remaining link opens without access to this window.

import DOMPurify from 'dompurify';
import { marked } from 'marked';

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
	if (node.tagName === 'A') node.setAttribute('rel', 'noopener noreferrer');
});

export function renderMarkdown(md: string): string {
	const html = marked.parse(md, { async: false, gfm: true }) as string;
	return DOMPurify.sanitize(html, { USE_PROFILES: { html: true }, FORBID_TAGS: ['img', 'style', 'form', 'input', 'iframe'] });
}
