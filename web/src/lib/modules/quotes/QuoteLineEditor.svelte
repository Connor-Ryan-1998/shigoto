<script lang="ts" module>
	import { parseCents, parseQuantityMilli } from '../../format';
	import type { QuoteLine, QuoteLineKind } from './types';

	// LineRow is one editable line: raw text, parsed only on save.
	export type LineRow = {
		id: number;
		kind: QuoteLineKind;
		description: string;
		quantity: string;
		unitAmount: string;
		gstCode: string;
	};

	let nextId = 0;
	export function emptyRow(kind: QuoteLineKind = 'labour', gstCode = 'GST'): LineRow {
		return { id: nextId++, kind, description: '', quantity: '', unitAmount: '', gstCode };
	}

	function milliText(milli: number): string {
		return String(milli / 1000);
	}

	export function rowsFromLines(lines: QuoteLine[]): LineRow[] {
		return lines.map((l) => ({
			id: nextId++,
			kind: l.kind,
			description: l.description,
			quantity: milliText(l.quantity_milli),
			unitAmount: (l.unit_amount / 100).toFixed(2),
			gstCode: l.gst_code
		}));
	}

	// parseRows returns the request lines, or null when there are none or
	// any row is incomplete or invalid.
	export function parseRows(rows: LineRow[]): QuoteLine[] | null {
		if (rows.length === 0) return null;
		const out: QuoteLine[] = [];
		for (const r of rows) {
			const description = r.description.trim();
			const quantity = parseQuantityMilli(r.quantity);
			const unit = parseCents(r.unitAmount);
			if (description === '' || quantity === null || quantity <= 0 || unit === null) return null;
			out.push({ kind: r.kind, description, quantity_milli: quantity, unit_amount: unit, gst_code: r.gstCode });
		}
		return out;
	}
</script>

<script lang="ts">
	import Field from '../../components/Field.svelte';
	import Select from '../../components/Select.svelte';
	import Button from '../../components/Button.svelte';

	const GST_OPTIONS = [
		{ value: 'GST', label: 'GST' },
		{ value: 'FRE', label: 'GST-free' },
		{ value: 'N-T', label: 'Not taxable (N-T)' }
	];
	const KIND_OPTIONS = [
		{ value: 'labour', label: 'Labour' },
		{ value: 'material', label: 'Material' }
	];

	let {
		rows = $bindable(),
		busy = false,
		defaultGst = 'GST',
		onchange
	}: { rows: LineRow[]; busy?: boolean; defaultGst?: string; onchange?: () => void } = $props();

	const uid = $props.id();

	function quantityError(row: LineRow): string | undefined {
		if (row.quantity.trim() === '') return undefined;
		const parsed = parseQuantityMilli(row.quantity);
		if (parsed === null) return 'Enter a valid quantity, up to 3 decimal places.';
		if (parsed <= 0) return 'Quantity must be greater than zero.';
		return undefined;
	}

	function amountError(row: LineRow): string | undefined {
		if (row.unitAmount.trim() === '') return undefined;
		if (parseCents(row.unitAmount) === null) return 'Enter a valid amount, up to 2 decimal places.';
		return undefined;
	}

	function add(kind: QuoteLineKind) {
		if (busy) return;
		rows = [...rows, emptyRow(kind, defaultGst)];
		onchange?.();
	}

	// Reassigned rather than mutated, so the labels follow the kind even
	// when the parent passes rows without binding them.
	function setKind(id: number, kind: string) {
		rows = rows.map((r) => (r.id === id ? { ...r, kind: kind as QuoteLineKind } : r));
	}

	function remove(id: number) {
		if (busy) return;
		rows = rows.filter((r) => r.id !== id);
		onchange?.();
	}
</script>

<fieldset disabled={busy} class="flex min-w-0 flex-col gap-4" oninput={() => onchange?.()} onchange={() => onchange?.()}>
	<legend class="sr-only">Quote lines</legend>
	{#each rows as row, i (row.id)}
		{@const id = `qline-${uid}-${row.id}`}
		<div
			class="grid grid-cols-1 items-end gap-3 border-b border-base-300 pb-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] [&_select]:w-full [&_select]:min-w-0"
		>
			<Field label="Line type" id={`${id}-kind`}>
				<Select id={`${id}-kind`} value={row.kind} options={KIND_OPTIONS} onchange={(v) => setKind(row.id, v)} />
			</Field>
			<Field label="Description" id={`${id}-description`}>
				<input class="input w-full" id={`${id}-description`} bind:value={row.description} />
			</Field>
			<Field label={row.kind === 'labour' ? 'Hours' : 'Quantity'} id={`${id}-quantity`} error={quantityError(row)}>
				<input class="input w-full" id={`${id}-quantity`} inputmode="decimal" bind:value={row.quantity} />
			</Field>
			<Field label={row.kind === 'labour' ? 'Rate ($/h)' : 'Unit price ($)'} id={`${id}-amount`} error={amountError(row)}>
				<input class="input w-full" id={`${id}-amount`} inputmode="decimal" bind:value={row.unitAmount} />
			</Field>
			<Field label="GST code" id={`${id}-gst`}>
				<Select id={`${id}-gst`} bind:value={row.gstCode} options={GST_OPTIONS} />
			</Field>
			<Button variant="secondary" onclick={() => remove(row.id)}>
				Remove&#32;<span class="sr-only">line {i + 1}</span>
			</Button>
		</div>
	{/each}

	<div class="flex flex-wrap gap-3">
		<Button variant="secondary" onclick={() => add('labour')}>Add labour</Button>
		<Button variant="secondary" onclick={() => add('material')}>Add material</Button>
	</div>
</fieldset>
