<script lang="ts">
	// TreasurerCard holds the treasurer duties for a strata scheme in which the
	// organisation is treasurer: an assistant answering questions from the
	// scheme's documents, with page citations.
	import AskPanel from './AskPanel.svelte';
	import type { StrataScheme } from '../types';

	let { org, scheme }: { org: string; scheme: StrataScheme } = $props();
	const headingId = $derived(`strata-treasurer-${org}-${scheme.id}`);
</script>

<section class="card card-border bg-base-100 p-4" aria-labelledby={headingId}>
	<h3 id={headingId} class="mb-2 text-lg font-bold">Treasurer</h3>
	<p class="mb-3 text-base-content/70">Ask about levies, budgets, fund balances and arrears.</p>
	<AskPanel {org} schemeId={scheme.id} role="treasurer" />
</section>
