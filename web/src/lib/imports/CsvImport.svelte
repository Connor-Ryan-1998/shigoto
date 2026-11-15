<script lang="ts">
	// CsvImport is the shared upload -> map columns -> server preview ->
	// import workflow for owner-only CSV imports. The server checks every
	// row on preview and again on import (all or nothing); this component
	// only reads the header row to offer the mapping.
	import { goto } from '$app/navigation';
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '../api';
	import Button from '../components/Button.svelte';
	import Field from '../components/Field.svelte';
	import Select from '../components/Select.svelte';
	import Toast from '../components/Toast.svelte';
	import { useUnsavedChanges } from '../forms/unsaved.svelte';
	import { guessMapping, readSample } from './csv';
	import { dateFormats, type ImportField, type ImportProblem, type ImportSummary } from './types';

	let {
		fields,
		previewPath,
		applyPath,
		backHref,
		noun,
		summaryLabel
	}: {
		fields: ImportField[];
		previewPath: string;
		applyPath: string;
		backHref: string;
		noun: string;
		summaryLabel: (s: ImportSummary) => string;
	} = $props();

	let file = $state<File | null>(null);
	let headers = $state<string[]>([]);
	let sample = $state<string[][]>([]);
	let mapping = $state<Record<string, string>>({});
	let dateFormat = $state(dateFormats[0].value);
	let preview = $state<ImportSummary | null>(null);
	let problems = $state<ImportProblem[]>([]);
	let done = $state<ImportSummary | null>(null);
	let busy = $state(false);
	let error = $state('');
	let readError = $state('');
	// fileGeneration invalidates a file read or request that a newer file
	// pick has superseded.
	let fileGeneration = 0;
	let active = true;
	onDestroy(() => {
		active = false;
	});

	const dirty = $derived(file !== null && done === null);
	const { allowLeave } = useUnsavedChanges(() => dirty, () => busy);
	const headerOptions = $derived([{ value: '', label: '— not mapped —' }, ...headers.map((h) => ({ value: h, label: h }))]);
	const canImport = $derived(preview !== null && preview.problems.length === 0 && preview.new > 0 && !busy);

	async function pickFile(event: Event) {
		const picked = (event.currentTarget as HTMLInputElement).files?.[0] ?? null;
		const generation = ++fileGeneration;
		file = picked;
		preview = null;
		problems = [];
		readError = '';
		headers = [];
		sample = [];
		if (!picked) return;
		let text: string;
		try {
			text = await picked.text();
		} catch {
			if (active && generation === fileGeneration) readError = 'The file could not be read.';
			return;
		}
		if (!active || generation !== fileGeneration) return;
		const read = readSample(text);
		if (read.headers.length === 0) {
			readError = 'The file has no header row.';
			return;
		}
		headers = read.headers;
		sample = read.rows;
		mapping = guessMapping(fields, read.headers);
	}

	function changed() {
		preview = null;
		problems = [];
	}

	function body(): FormData {
		const form = new FormData();
		form.append('file', file!);
		form.append('mapping', JSON.stringify({ columns: { ...mapping }, date_format: dateFormat }));
		return form;
	}

	async function send(path: string): Promise<ImportSummary | null> {
		if (busy || !file) return null;
		busy = true;
		error = '';
		const generation = fileGeneration;
		try {
			const result = await api<ImportSummary>('POST', path, body());
			return active && generation === fileGeneration ? result : null;
		} catch (e) {
			if (!active || generation !== fileGeneration) return null;
			error = e instanceof ApiError ? e.message : 'Something went wrong.';
			if (e instanceof ApiError && e.problems) {
				preview = null;
				problems = e.problems;
			}
			return null;
		} finally {
			if (active) busy = false;
		}
	}

	async function runPreview() {
		const result = await send(previewPath);
		if (!result) return;
		preview = result;
		problems = result.problems;
	}

	async function runImport() {
		if (!canImport) return;
		const result = await send(applyPath);
		if (result) done = result;
	}

	function cancel() {
		if (allowLeave()) {
			file = null;
			goto(backHref);
		}
	}
</script>

{#if done}
	<div role="status" class="alert alert-success alert-soft">
		<span>Imported {summaryLabel(done)}{done.existing ? `; ${done.existing} already imported` : ''}.</span>
	</div>
	<p class="mt-4"><a class="link" href={backHref}>Back to {noun}</a></p>
{:else}
	<fieldset disabled={busy} class="flex min-w-0 max-w-4xl flex-col gap-5">
		<Field label="CSV file" id="import-file" hint="Comma-separated, with a header row. Up to 5 MB." error={readError || undefined}>
			<input class="file-input w-full" id="import-file" type="file" accept=".csv,text/csv" onchange={pickFile} />
		</Field>

		{#if headers.length > 0}
			<section class="flex flex-col gap-3" aria-labelledby="import-columns">
				<h2 id="import-columns" class="text-lg font-semibold">Columns</h2>
				<div class="grid gap-3 sm:grid-cols-2">
					{#each fields as field (field.key)}
						<Field label={field.label} id={`map-${field.key}`} hint={field.required ? undefined : 'Optional'}>
							<Select id={`map-${field.key}`} bind:value={mapping[field.key]} options={headerOptions} onchange={changed} />
						</Field>
					{/each}
					<Field label="Date format" id="map-date-format">
						<Select id="map-date-format" bind:value={dateFormat} options={dateFormats} onchange={changed} />
					</Field>
				</div>
			</section>

			{#if sample.length > 0}
				<div class="overflow-x-auto">
					<table class="table table-sm">
						<caption class="text-left text-sm text-base-content/70">First rows of the file</caption>
						<thead><tr>{#each headers as h, i (i)}<th scope="col">{h}</th>{/each}</tr></thead>
						<tbody>
							{#each sample as row, r (r)}
								<tr>{#each headers as _, i (i)}<td>{row[i] ?? ''}</td>{/each}</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		{/if}

		{#if preview}
			<div role="status" class={['alert alert-soft', preview.problems.length ? 'alert-warning' : 'alert-info']}>
				<span>{summaryLabel(preview)}{preview.existing ? `; ${preview.existing} already imported and skipped` : ''}</span>
			</div>
		{/if}

		{#if problems.length > 0}
			<div class="overflow-x-auto">
				<table class="table table-sm">
					<caption class="text-left text-sm text-error">Fix these in the file or the mapping, then preview again.</caption>
					<thead><tr><th scope="col">Row</th><th scope="col">Field</th><th scope="col">Problem</th></tr></thead>
					<tbody>
						{#each problems as p, i (i)}
							<tr><td>{p.row || 'File'}</td><td>{p.field ?? ''}</td><td>{p.message}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<div class="flex flex-wrap gap-3">
			<Button variant="secondary" disabled={!file || headers.length === 0} busy={busy && !preview} onclick={runPreview}>Preview</Button>
			<Button disabled={!canImport} busy={busy && preview !== null} onclick={runImport}>Import</Button>
			<Button variant="secondary" disabled={busy} onclick={cancel}>Cancel</Button>
		</div>
	</fieldset>
{/if}

{#if error}
	<Toast message={error} tone="danger" onDismiss={() => (error = '')} />
{/if}
