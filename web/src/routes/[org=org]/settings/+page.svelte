<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '../../../lib/api';
	import { session } from '../../../lib/session.svelte';
	import PageHeader from '../../../lib/components/PageHeader.svelte';
	import Field from '../../../lib/components/Field.svelte';
	import Select from '../../../lib/components/Select.svelte';
	import Button from '../../../lib/components/Button.svelte';
	import FormActions from '../../../lib/components/FormActions.svelte';
	import { useUnsavedChanges } from '../../../lib/forms/unsaved.svelte';

	type Settings = {
		name: string;
		abn: string;
		gst_registered: boolean;
		bas_frequency: string;
		bank_bsb: string;
		bank_account: string;
		payid: string;
		invoice_lead_days: number;
		auto_send_rent: boolean;
		emergency_phone: string;
		address: string;
		phone: string;
		email: string;
		quote_terms: string;
		licences: { type: string; number: string }[];
		has_logo: boolean;
	};

	const MAX_LICENCES = 5;

	// withProfileDefaults fills profile fields a response may lack, so the
	// form always has a licence list to edit.
	function withProfileDefaults(s: Partial<Settings> & Omit<Settings, 'address' | 'phone' | 'email' | 'quote_terms' | 'licences' | 'has_logo'>): Settings {
		return {
			...s,
			address: s.address ?? '', phone: s.phone ?? '', email: s.email ?? '', quote_terms: s.quote_terms ?? '',
			licences: s.licences ?? [], has_logo: s.has_logo ?? false
		};
	}

	const org = $derived(page.params.org);
	const canManage = $derived(
		session.currentOrg?.slug === org && (session.currentOrg?.perms.includes('org_admin') ?? false)
	);
	let settings = $state<Settings | null>(null);
	let loadedOrg = $state('');
	let loading = $state(false);
	let saving = $state(false);
	let error = $state('');
	let success = $state('');
	let requestSeq = 0;
	let snapshot = $state('');
	const dirty = $derived(settings !== null && loadedOrg === org && JSON.stringify(settings) !== snapshot);
	const { allowLeave } = useUnsavedChanges(() => dirty, () => saving);
	function cancel() {
		if (!snapshot || !allowLeave()) return;
		settings = JSON.parse(snapshot);
		error = ''; success = '';
	}

	async function load(slug: string) {
		const seq = ++requestSeq;
		settings = null;
		loadedOrg = '';
		error = '';
		success = '';
		saving = false;
		loading = true;
		try {
			const result = withProfileDefaults(await api<Settings>('GET', `/api/v1/orgs/${slug}/settings`));
			if (seq !== requestSeq || slug !== org || !canManage) return;
			settings = result;
			snapshot = JSON.stringify(result);
			loadedOrg = slug;
		} catch (e) {
			if (seq !== requestSeq || slug !== org || !canManage) return;
			error = e instanceof ApiError ? e.message : 'Could not load settings.';
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	$effect(() => {
		if (canManage && org) load(org);
		return () => { requestSeq++; };
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!canManage || !settings || loadedOrg !== org || saving) return;
		const slug = org;
		const seq = requestSeq;
		saving = true;
		error = '';
		success = '';
		try {
			// PUT replaces the whole record, including empty optional values.
			// has_logo is response-only: the logo has its own routes.
			const { has_logo: _hasLogo, ...body } = settings;
			const result = withProfileDefaults(await api<Settings>('PUT', `/api/v1/orgs/${slug}/settings`, body));
			if (seq !== requestSeq || slug !== org || !canManage) return;
			settings = result;
			snapshot = JSON.stringify(result);
			success = 'Settings saved.';
			if (session.me) {
				session.setMe({ ...session.me, orgs: session.me.orgs.map((o) => o.slug === slug ? { ...o, name: result.name } : o) });
			}
		} catch (e) {
			if (seq !== requestSeq || slug !== org || !canManage) return;
			error = e instanceof ApiError ? e.message : 'Could not save settings.';
		} finally {
			if (seq === requestSeq) saving = false;
		}
	}

	// The logo saves immediately through its own route, so it never makes
	// the form dirty: only has_logo changes, in both settings and snapshot.
	let logoBusy = $state(false);
	function setHasLogo(value: boolean) {
		if (!settings) return;
		settings.has_logo = value;
		const saved = JSON.parse(snapshot) as Settings;
		saved.has_logo = value;
		snapshot = JSON.stringify(saved);
	}
	async function uploadLogo(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file || !canManage || logoBusy) return;
		const slug = org;
		const seq = requestSeq;
		logoBusy = true;
		error = ''; success = '';
		try {
			const form = new FormData();
			form.append('file', file);
			await api<Settings>('POST', `/api/v1/orgs/${slug}/settings/logo`, form);
			if (seq !== requestSeq || slug !== org) return;
			setHasLogo(true);
			success = 'Logo saved.';
		} catch (err) {
			if (seq !== requestSeq || slug !== org) return;
			error = err instanceof ApiError ? err.message : 'Could not upload the logo.';
		} finally {
			if (seq === requestSeq) logoBusy = false;
			input.value = '';
		}
	}
	async function removeLogo() {
		if (!canManage || logoBusy || !confirm('Remove the logo?')) return;
		const slug = org;
		const seq = requestSeq;
		logoBusy = true;
		error = ''; success = '';
		try {
			await api('DELETE', `/api/v1/orgs/${slug}/settings/logo`);
			if (seq !== requestSeq || slug !== org) return;
			setHasLogo(false);
			success = 'Logo removed.';
		} catch (err) {
			if (seq !== requestSeq || slug !== org) return;
			error = err instanceof ApiError ? err.message : 'Could not remove the logo.';
		} finally {
			if (seq === requestSeq) logoBusy = false;
		}
	}
</script>

<PageHeader title="Organisation settings" />

{#if !canManage}
	<p role="alert">You do not have permission to manage settings.</p>
{:else}
	{#if error}<p role="alert" class="alert alert-error alert-soft mb-4">{error}</p>{/if}
	{#if success}<p role="status" class="alert alert-success alert-soft mb-4">{success}</p>{/if}
	{#if loading || (loadedOrg !== org && !error)}
		<p role="status">Loading settings…</p>
	{:else if settings && loadedOrg === org}
		<form onsubmit={save} class="grid max-w-2xl gap-4">
			<fieldset disabled={saving} class="fieldset grid gap-3 rounded-box border border-base-300 bg-base-100 p-4">
				<legend class="fieldset-legend">Organisation and tax</legend>
				<Field label="Organisation name" id="settings-name">
					<input class="input w-full" id="settings-name" bind:value={settings.name} required />
				</Field>
				<Field label="ABN" id="settings-abn">
					<input class="input w-full" id="settings-abn" bind:value={settings.abn} />
				</Field>
				<label class="label"><input class="checkbox" type="checkbox" bind:checked={settings.gst_registered} /> GST registered</label>
				<Field label="BAS frequency" id="settings-bas">
					<Select id="settings-bas" bind:value={settings.bas_frequency} options={[
						{ value: '', label: 'Not set' },
						{ value: 'quarterly', label: 'Quarterly' },
						{ value: 'monthly', label: 'Monthly' }
					]} />
				</Field>
			</fieldset>
			<fieldset disabled={saving} class="fieldset grid gap-3 rounded-box border border-base-300 bg-base-100 p-4">
				<legend class="fieldset-legend">Business profile</legend>
				<p class="text-base-content/70">Printed on quotes.</p>
				<Field label="Business address" id="settings-address">
					<textarea class="textarea w-full" id="settings-address" rows="3" bind:value={settings.address}></textarea>
				</Field>
				<div class="grid gap-3 sm:grid-cols-2">
					<Field label="Business phone" id="settings-phone">
						<input class="input w-full" id="settings-phone" type="tel" autocomplete="tel" bind:value={settings.phone} />
					</Field>
					<Field label="Business email" id="settings-email">
						<input class="input w-full" id="settings-email" type="email" autocomplete="email" bind:value={settings.email} />
					</Field>
				</div>
				<div class="grid gap-2">
					<span class="font-medium">Licences</span>
					{#each settings.licences as licence, i (i)}
						<div class="grid grid-cols-[1fr_1fr_auto] items-end gap-2">
							<Field label={`Licence ${i + 1} type`} id={`settings-licence-${i}-type`}>
								<input class="input w-full" id={`settings-licence-${i}-type`} placeholder="e.g. QBCC" bind:value={licence.type} required />
							</Field>
							<Field label={`Licence ${i + 1} number`} id={`settings-licence-${i}-number`}>
								<input class="input w-full" id={`settings-licence-${i}-number`} bind:value={licence.number} required />
							</Field>
							<button type="button" class="btn btn-outline" aria-label={`Remove licence ${i + 1}`} onclick={() => settings && settings.licences.splice(i, 1)}>Remove</button>
						</div>
					{/each}
					{#if settings.licences.length < MAX_LICENCES}
						<div><Button variant="secondary" onclick={() => settings && settings.licences.push({ type: '', number: '' })}>Add licence</Button></div>
					{/if}
				</div>
				<Field label="Default quote terms" id="settings-quote-terms" hint="Pre-filled on new quotes.">
					<textarea class="textarea w-full" id="settings-quote-terms" rows="4" bind:value={settings.quote_terms}></textarea>
				</Field>
				{#if settings.has_logo}
					<div class="flex items-center gap-3">
						<img src={`/api/v1/orgs/${org}/settings/logo`} alt="Current logo" class="h-12 w-auto" />
						<Button variant="secondary" onclick={removeLogo} busy={logoBusy}>Remove logo</Button>
					</div>
				{:else}
					<Field label="Logo" id="settings-logo" hint="PNG or JPEG, up to 2 MB.">
						<input class="file-input w-full" id="settings-logo" type="file" accept="image/png,image/jpeg" disabled={logoBusy} onchange={uploadLogo} />
					</Field>
				{/if}
			</fieldset>
			<fieldset disabled={saving} class="fieldset grid gap-3 rounded-box border border-base-300 bg-base-100 p-4">
				<legend class="fieldset-legend">Payment instructions</legend>
				<p class="text-base-content/70">Clearing these instructions can remove payment options from rent and bond invoices.</p>
				<Field label="BSB" id="settings-bsb"><input class="input w-full" id="settings-bsb" bind:value={settings.bank_bsb} /></Field>
				<Field label="Bank account" id="settings-account"><input class="input w-full" id="settings-account" bind:value={settings.bank_account} /></Field>
				<Field label="PayID" id="settings-payid"><input class="input w-full" id="settings-payid" bind:value={settings.payid} /></Field>
			</fieldset>
			<fieldset disabled={saving} class="fieldset grid gap-3 rounded-box border border-base-300 bg-base-100 p-4">
				<legend class="fieldset-legend">Rent invoicing</legend>
				<Field label="Invoice lead days" id="settings-lead">
					<input class="input w-full" id="settings-lead" type="number" min="0" max="14" step="1" bind:value={settings.invoice_lead_days} required />
				</Field>
				<label class="label"><input class="checkbox" type="checkbox" bind:checked={settings.auto_send_rent} /> Send rent invoices automatically</label>
				<p class="text-base-content/70">Automatic sending uses the configured payment provider.</p>
			</fieldset>
			<fieldset disabled={saving} class="fieldset grid gap-3 rounded-box border border-base-300 bg-base-100 p-4">
				<legend class="fieldset-legend">Repairs</legend>
				<Field label="Emergency repairs phone" id="settings-emergency-phone" hint="Shown to tenants who mark a repair as urgent.">
					<input class="input w-full" id="settings-emergency-phone" type="tel" autocomplete="tel" bind:value={settings.emergency_phone} />
				</Field>
			</fieldset>
			<FormActions busy={saving} submitLabel="Save settings" oncancel={cancel} />
		</form>
	{:else}
		<Button onclick={() => org && load(org)}>Retry</Button>
	{/if}
{/if}
