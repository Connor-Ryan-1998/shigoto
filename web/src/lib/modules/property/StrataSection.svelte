<script lang="ts">
	// StrataSection edits which strata scheme a property is a lot in and the
	// scheme's name, CTS number and the positions the organisation holds.
	// It saves on its own, apart from the property details, because scheme
	// edits apply to every property linked to the scheme.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import { session } from '../../session.svelte';
	import { useUnsavedChanges } from '../../forms/unsaved.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import FormActions from '../../components/FormActions.svelte';
	import StrataSchemeDialog from './StrataSchemeDialog.svelte';
	import { STRATA_POSITIONS, strataCtsError, type PropertyDetail, type StrataScheme } from './types';

	let {
		org,
		property,
		onsaved
	}: {
		org: string;
		property: PropertyDetail;
		onsaved: (scheme: StrataScheme | null) => void;
	} = $props();

	type Form = {
		enabled: boolean; schemeId: number | null; name: string; cts_number: string;
		is_chairperson: boolean; is_secretary: boolean; is_treasurer: boolean;
	};

	const member = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const canWrite = $derived(member?.perms.includes('property_write') ?? false);
	const schemesUrl = $derived(`/api/v1/orgs/${org}/strata-schemes`);

	// linked is the scheme the property is saved as linked to.
	let linked = $state<StrataScheme | null>(null);
	let schemes = $state<StrataScheme[]>([]);
	let schemesLoaded = $state(false);
	let form = $state<Form>(blank());
	let snapshot = $state('');
	let saving = $state(false);
	let error = $state('');
	let ctsError = $state('');
	let success = $state('');
	let creating = $state(false);
	let seq = 0;

	const dirty = $derived(JSON.stringify(form) !== snapshot);
	const { allowLeave } = useUnsavedChanges(() => dirty, () => saving);

	function blank(): Form {
		return { enabled: false, schemeId: null, name: '', cts_number: '', is_chairperson: false, is_secretary: false, is_treasurer: false };
	}
	function fromScheme(s: StrataScheme | null): Form {
		if (!s) return blank();
		return { enabled: true, schemeId: s.id, name: s.name, cts_number: s.cts_number,
			is_chairperson: s.is_chairperson, is_secretary: s.is_secretary, is_treasurer: s.is_treasurer };
	}
	function reset(s: StrataScheme | null) {
		linked = s;
		form = fromScheme(s);
		snapshot = JSON.stringify(form);
	}

	// Re-seed only when a different property is shown, so saving the
	// property's details never discards unsaved strata edits. The id is a
	// primitive $derived so replacing the property object (a details save)
	// does not re-run the effect.
	const propertyId = $derived(property.id);
	$effect(() => {
		propertyId; org;
		untrack(() => {
			reset(property.strata_scheme ?? null);
			schemes = linked ? [linked] : [];
			schemesLoaded = false; saving = false; error = ''; ctsError = ''; success = '';
			if (linked) loadSchemes();
		});
		return () => { seq++; };
	});

	async function loadSchemes() {
		if (!canWrite || schemesLoaded) return;
		const token = seq;
		try {
			const list = await api<StrataScheme[]>('GET', schemesUrl);
			if (token !== seq) return;
			schemes = list;
			schemesLoaded = true;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load strata schemes.';
		}
	}

	const selected = $derived(schemes.find((s) => s.id === form.schemeId) ?? null);
	const sharedWith = $derived(selected?.properties.filter((p) => p.id !== property.id).map((p) => p.name) ?? []);

	function toggled() {
		if (form.enabled) loadSchemes();
	}
	function choose(event: Event) {
		const value = (event.currentTarget as HTMLSelectElement).value;
		const s = schemes.find((x) => String(x.id) === value) ?? null;
		form = s ? fromScheme(s) : { ...blank(), enabled: true };
		ctsError = '';
	}
	function created(s: StrataScheme) {
		schemes = [...schemes, s].sort((a, b) => a.name.localeCompare(b.name) || a.id - b.id);
		form = fromScheme(s);
	}

	function cancel() {
		if (!allowLeave()) return;
		form = JSON.parse(snapshot);
		error = ''; ctsError = ''; success = '';
	}

	const schemeFields = (f: Form) => ({ name: f.name, cts_number: f.cts_number,
		is_chairperson: f.is_chairperson, is_secretary: f.is_secretary, is_treasurer: f.is_treasurer });

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saving || !canWrite) return;
		error = ''; ctsError = ''; success = '';
		if (form.enabled && form.schemeId === null) {
			error = 'Choose or create a scheme.';
			return;
		}
		const f = $state.snapshot(form);
		const target = f.enabled ? f.schemeId : null;
		const token = seq;
		saving = true;
		try {
			let result: StrataScheme | null = f.enabled ? selected : null;
			const current = selected;
			if (f.enabled && current && JSON.stringify(schemeFields(f)) !== JSON.stringify(schemeFields(fromScheme(current)))) {
				result = await api<StrataScheme>('PUT', `${schemesUrl}/${f.schemeId}`, schemeFields(f));
				if (token !== seq) return;
				const saved = result;
				schemes = schemes.map((s) => (s.id === saved.id ? saved : s));
			}
			if (target !== (linked?.id ?? null)) {
				const res = await api<{ strata_scheme: StrataScheme | null }>('PUT', `/api/v1/orgs/${org}/properties/${property.id}/strata`, { strata_scheme_id: target });
				if (token !== seq) return;
				result = res.strata_scheme;
				const saved = result;
				if (saved) schemes = schemes.map((s) => (s.id === saved.id ? saved : s));
			}
			reset(result);
			success = 'Strata saved.';
			onsaved(result);
		} catch (err) {
			if (token !== seq) return;
			const message = err instanceof ApiError ? err.message : 'Could not save strata.';
			ctsError = strataCtsError(message) ?? '';
			error = message;
		} finally {
			if (token === seq) saving = false;
		}
	}
</script>

<section class="mt-6" aria-labelledby="strata-heading">
	<form onsubmit={save} class="grid gap-3">
		<h2 id="strata-heading" class="text-lg font-bold">Strata</h2>
		{#if error}<p role="alert" class="alert alert-error alert-soft">{error}</p>{/if}
		{#if success}<p role="status" class="alert alert-success alert-soft">{success}</p>{/if}
		<fieldset disabled={saving || !canWrite} class="fieldset grid gap-3 rounded-box border border-base-300 bg-base-100 p-4 sm:grid-cols-2">
			<label class="label sm:col-span-2"><input class="checkbox" type="checkbox" bind:checked={form.enabled} onchange={toggled} /> Part of a strata scheme</label>
			{#if form.enabled}
				{#if canWrite}
					<div class="flex items-end gap-2 sm:col-span-2">
						<div class="grow">
							<Field label="Scheme" id="strata-scheme">
								<select class="select w-full" id="strata-scheme" value={form.schemeId === null ? '' : String(form.schemeId)} onchange={choose}>
									<option value="" disabled>Choose a scheme</option>
									{#each schemes as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
								</select>
							</Field>
						</div>
						<Button variant="secondary" onclick={() => (creating = true)}>New scheme…</Button>
					</div>
				{/if}
				{#if form.schemeId !== null}
					<Field label="Scheme name" id="strata-name"><input class="input w-full" id="strata-name" bind:value={form.name} required /></Field>
					<Field label="CTS number" id="strata-cts" error={ctsError || undefined}><input class="input w-full" id="strata-cts" bind:value={form.cts_number} /></Field>
					<div class="grid gap-1 sm:col-span-2" role="group" aria-label="Positions held">
						<span class="text-sm font-semibold">Positions held</span>
						{#each STRATA_POSITIONS as p (p.value)}
							<label class="label"><input class="checkbox" type="checkbox" bind:checked={form[p.field]} /> {p.label}</label>
						{/each}
					</div>
					{#if sharedWith.length}
						<p class="text-sm text-base-content/70 sm:col-span-2">Shared with: {sharedWith.join(', ')}. Changes apply to every linked property.</p>
					{/if}
				{/if}
			{/if}
		</fieldset>
		{#if canWrite}<FormActions busy={saving} submitLabel="Save strata" oncancel={cancel} />{/if}
	</form>
</section>
{#if canWrite}
	<StrataSchemeDialog bind:open={creating} {org} oncreated={created} />
{/if}
