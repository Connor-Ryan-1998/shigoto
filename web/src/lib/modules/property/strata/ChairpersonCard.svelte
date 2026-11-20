<script lang="ts">
	// ChairpersonCard holds the chairperson duties for a strata scheme in which the
	// organisation is chairperson. Workflows arrive in a later change.
	import type { StrataScheme } from '../types';

	let { org, scheme }: { org: string; scheme: StrataScheme } = $props();
	const headingId = $derived(`strata-chairperson-${org}-${scheme.id}`);
</script>

<section class="card card-border bg-base-100 p-4" aria-labelledby={headingId}>
	<h3 id={headingId} class="mb-2 text-lg font-bold">Chairperson</h3>
	<p class="text-base-content/70">Duties for this role will appear here.</p>
</section>
