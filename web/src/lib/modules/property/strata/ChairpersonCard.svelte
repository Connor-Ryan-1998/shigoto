<script lang="ts">
	// ChairpersonCard holds the chairperson duties for a strata scheme in which the
	// organisation is chairperson: an assistant answering questions from the
	// scheme's documents, with page citations.
	import AskPanel from './AskPanel.svelte';
	import type { StrataScheme } from '../types';

	let { org, scheme }: { org: string; scheme: StrataScheme } = $props();
	const headingId = $derived(`strata-chairperson-${org}-${scheme.id}`);
</script>

<section class="card card-border bg-base-100 p-4" aria-labelledby={headingId}>
	<h3 id={headingId} class="mb-2 text-lg font-bold">Chairperson</h3>
	<p class="mb-3 text-base-content/70">Ask about committee decisions, motions, by-laws and the CMS.</p>
	<AskPanel {org} schemeId={scheme.id} role="chairperson" />
</section>
