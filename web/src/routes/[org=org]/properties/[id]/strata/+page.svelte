<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../../lib/api';
	import { session } from '../../../../../lib/session.svelte';
	import LoadState from '../../../../../lib/components/LoadState.svelte';
	import ChairpersonCard from '../../../../../lib/modules/property/strata/ChairpersonCard.svelte';
	import SecretaryCard from '../../../../../lib/modules/property/strata/SecretaryCard.svelte';
	import TreasurerCard from '../../../../../lib/modules/property/strata/TreasurerCard.svelte';
	import DocumentsCard from '../../../../../lib/modules/property/strata/DocumentsCard.svelte';
	import type { StrataScheme } from '../../../../../lib/modules/property/types';

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	const scope = $derived(`${org}/${id}`);
	const member = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const canRead = $derived(member?.perms.includes('property_read') ?? false);
	const canWrite = $derived(member?.perms.includes('property_write') ?? false);
	const details = $derived(`/${org}/properties/${id}`);

	let scheme = $state<StrataScheme | null>(null);
	let propertyId = $state(0);
	let loaded = $state('');
	let error = $state('');
	let seq = 0;

	async function load() {
		const token = ++seq;
		const s = scope;
		error = '';
		try {
			const p = await api<{ id: number; strata_scheme: StrataScheme | null }>('GET', `/api/v1/orgs/${org}/properties/${id}`);
			if (token !== seq || s !== scope || !canRead) return;
			scheme = p.strata_scheme;
			propertyId = p.id;
			loaded = s;
		} catch (e) {
			if (token === seq && s === scope) error = e instanceof ApiError ? e.message : 'Could not load strata.';
		}
	}
	$effect(() => {
		scope; canRead;
		untrack(() => { loaded = ''; scheme = null; if (canRead) load(); });
		return () => { seq++; };
	});

	const sharedWith = $derived(scheme?.properties.filter((p) => p.id !== propertyId).map((p) => p.name) ?? []);
	const holdsAny = $derived(!!scheme && (scheme.is_chairperson || scheme.is_secretary || scheme.is_treasurer));
</script>

{#if !canRead}
	<p role="alert">You do not have permission to view properties.</p>
{:else}
	<LoadState loading={loaded !== scope} {error} empty={false} loadingLabel="Loading strata…" onretry={load}>
		{#if !scheme}
			<p class="text-base-content/70">This property is not part of a strata scheme.</p>
			<a class="link link-primary" href={details}>Set up strata on Details</a>
		{:else}
			<div class="grid gap-4">
				<section class="card card-border bg-base-100 p-4" aria-labelledby="strata-scheme-heading">
					<h2 id="strata-scheme-heading" class="text-lg font-bold">{scheme.name}</h2>
					<p>{scheme.cts_number ? `CTS ${scheme.cts_number}` : 'No CTS number'}</p>
					{#if sharedWith.length}<p class="text-base-content/70">Shared with: {sharedWith.join(', ')}</p>{/if}
				</section>
				{#if !holdsAny}
					<p class="text-base-content/70">You don't hold any committee positions in this scheme.</p>
					<a class="link link-primary" href={details}>Choose positions on Details</a>
				{:else}
					<div class="grid gap-4 lg:grid-cols-3">
						{#if scheme.is_chairperson}<ChairpersonCard {org} {scheme} />{/if}
						{#if scheme.is_secretary}<SecretaryCard {org} {scheme} />{/if}
						{#if scheme.is_treasurer}<TreasurerCard {org} {scheme} />{/if}
					</div>
					{#if scheme.is_chairperson || scheme.is_treasurer}<DocumentsCard {org} {scheme} {canWrite} />{/if}
				{/if}
			</div>
		{/if}
	</LoadState>
{/if}
