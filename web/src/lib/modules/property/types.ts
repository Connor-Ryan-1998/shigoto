// API shapes for the property page (internal/property/routes*.go).

export type Tenancy = {
	lease_id: number;
	rent: number;
	frequency: string;
	primary_tenant_id: number;
	primary_tenant_name: string;
};

export type PropertyDetail = {
	id: number;
	name: string;
	address?: string;
	kind?: string;
	residential: boolean;
	contract_date: string | null;
	settlement_date: string | null;
	purchase_price: number | null;
	stamp_duty: number | null;
	purchase_legal_costs: number | null;
	year_built: number | null;
	lot_plan: string;
	land_area_m2: number | null;
	council_account_ref: string;
	water_account_ref: string;
	notes: string;
	current_tenancy: Tenancy | null;
	strata_scheme: StrataScheme | null;
};

// A QLD strata (body corporate) scheme; the is_* flags are the executive
// committee positions the organisation holds in it.
export type StrataScheme = {
	id: number;
	name: string;
	cts_number: string;
	is_chairperson: boolean;
	is_secretary: boolean;
	is_treasurer: boolean;
	properties: { id: number; name: string }[];
};

export type StrataPosition = 'chairperson' | 'secretary' | 'treasurer';

export const STRATA_POSITIONS: { value: StrataPosition; label: string; field: 'is_chairperson' | 'is_secretary' | 'is_treasurer' }[] = [
	{ value: 'chairperson', label: 'Chairperson', field: 'is_chairperson' },
	{ value: 'secretary', label: 'Secretary', field: 'is_secretary' },
	{ value: 'treasurer', label: 'Treasurer', field: 'is_treasurer' }
];

export type StrataDocType =
	| 'minutes' | 'by_laws' | 'cms' | 'levy_notice' | 'budget' | 'financial_statement' | 'sinking_fund_forecast' | 'other';

export const STRATA_DOC_TYPES: { value: StrataDocType; label: string }[] = [
	{ value: 'minutes', label: 'Minutes' },
	{ value: 'by_laws', label: 'By-laws' },
	{ value: 'cms', label: 'Community management statement' },
	{ value: 'levy_notice', label: 'Levy notice' },
	{ value: 'budget', label: 'Budget' },
	{ value: 'financial_statement', label: 'Financial statement' },
	{ value: 'sinking_fund_forecast', label: 'Sinking fund forecast' },
	{ value: 'other', label: 'Other' }
];

export const strataDocTypeLabel = (t: StrataDocType) => STRATA_DOC_TYPES.find((d) => d.value === t)?.label ?? t;

// A StrataMax document uploaded to a scheme and its indexing state.
export type StrataDocument = {
	id: number;
	doc_type: StrataDocType;
	title: string;
	document_date: string;
	status: 'pending' | 'extracting' | 'indexed' | 'failed';
	error: string;
	page_count: number;
	extract_model: string;
	chunks_total: number;
	chunks_embedded: number;
};

// strataCtsError maps a rejected scheme save to a CTS number field error, or
// null when the message is not about the CTS number.
export const strataCtsError = (message: string) =>
	message.includes('cts_number') ? 'Another scheme already uses this CTS number.' : null;

export type InsuranceType = 'building' | 'landlord' | 'strata_contents' | 'other';

export const INSURANCE_TYPES: { value: InsuranceType; label: string }[] = [
	{ value: 'landlord', label: 'Landlord' },
	{ value: 'building', label: 'Building' },
	{ value: 'strata_contents', label: 'Strata contents' },
	{ value: 'other', label: 'Other' }
];

export type InsurancePolicy = {
	id: number;
	property_id: number;
	insurer: string;
	type: InsuranceType;
	policy_number: string;
	start_date: string;
	renewal_date: string;
	sum_insured: number | null;
	premium: number | null;
	excess: number | null;
	file_id: number | null;
	file_name: string | null;
};

export type DepreciationType = 'capital_works' | 'plant_equipment';

export const DEPRECIATION_TYPES: { value: DepreciationType; label: string }[] = [
	{ value: 'capital_works', label: 'Capital works' },
	{ value: 'plant_equipment', label: 'Plant & equipment' }
];

export type DepreciationItem = {
	id: number;
	property_id: number;
	name: string;
	type: DepreciationType;
	installed_on: string;
	cost: number | null;
	notes: string;
	expense_id: number | null;
	amounts: { fy: number; amount: number }[];
	documents: { id: number; original_name: string }[];
};

export type UploadedFile = { id: number; original_name: string };

// Item 21 labels in rental-schedule order (internal/core/item21.go).
export const ITEM21_LABELS: { value: string; label: string; depreciationOnly?: boolean }[] = [
	{ value: 'advertising', label: 'Advertising for tenants' },
	{ value: 'body_corporate', label: 'Body corporate fees and charges' },
	{ value: 'borrowing', label: 'Borrowing expenses' },
	{ value: 'cleaning', label: 'Cleaning' },
	{ value: 'council_rates', label: 'Council rates' },
	{ value: 'capital_allowances', label: 'Capital allowances (depreciation on plant)', depreciationOnly: true },
	{ value: 'gardening', label: 'Gardening/lawn mowing' },
	{ value: 'insurance', label: 'Insurance' },
	{ value: 'interest', label: 'Interest on loans' },
	{ value: 'land_tax', label: 'Land tax' },
	{ value: 'legal', label: 'Legal fees' },
	{ value: 'pest_control', label: 'Pest control' },
	{ value: 'agent_fees', label: 'Property agent fees/commission' },
	{ value: 'repairs', label: 'Repairs and maintenance' },
	{ value: 'capital_works', label: 'Capital works deductions', depreciationOnly: true },
	{ value: 'stationery', label: 'Stationery, telephone and postage' },
	{ value: 'water', label: 'Water charges' },
	{ value: 'sundry', label: 'Sundry rental expenses' }
];

export const item21Title = (value: string) => ITEM21_LABELS.find((l) => l.value === value)?.label ?? value;

// centsInput renders optional cents as an editable dollar string.
export const centsInput = (c: number | null | undefined) => (c === null || c === undefined ? '' : (c / 100).toFixed(2));
