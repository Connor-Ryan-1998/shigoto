// Components the editor contract can mount, by the name editors/*.yaml use.

import ExpenseNewPage from '../../routes/[org]/expenses/new/+page.svelte';
import InvoicePage from '../../routes/[org]/invoices/[id]/+page.svelte';
import InvoiceNewPage from '../../routes/[org]/invoices/new/+page.svelte';
import ContactDialog from '../modules/core/contacts/ContactDialog.svelte';
import LogTimeDialog from '../modules/timesheets/LogTimeDialog.svelte';
import SettingsPage from '../../routes/[org]/settings/+page.svelte';
import LeasePage from '../../routes/[org]/leases/[id]/+page.svelte';
import LeaseNewPage from '../../routes/[org]/leases/new/+page.svelte';
import ProjectDialog from '../modules/timesheets/ProjectDialog.svelte';
import BillingRunDialog from '../modules/timesheets/BillingRunDialog.svelte';
import PropertyDetailsPage from '../../routes/[org]/properties/[id]/+page.svelte';
import InsuranceDialog from '../modules/property/InsuranceDialog.svelte';
import DepreciationItemDialog from '../modules/property/DepreciationItemDialog.svelte';
import Item21MappingDialog from '../modules/property/Item21MappingDialog.svelte';
import NewTicketDialog from '../modules/property/NewTicketDialog.svelte';
import AssignSupplierDialog from '../modules/property/AssignSupplierDialog.svelte';
import TicketMessageDialog from '../modules/property/TicketMessageDialog.svelte';
import PortalRepairNewPage from '../../routes/portal/[org]/repairs/new/+page.svelte';
import PortalRepairPage from '../../routes/portal/[org]/repairs/[id]/+page.svelte';
import VisitDialog from '../modules/property/VisitDialog.svelte';
import RequestChangeDialog from '../modules/property/RequestChangeDialog.svelte';
import PropertyNotesPage from '../../routes/[org]/properties/[id]/notes/+page.svelte';
import StrataSection from '../modules/property/StrataSection.svelte';
import StrataSchemeDialog from '../modules/property/StrataSchemeDialog.svelte';
import PortalDocumentPage from '../../routes/portal/[org]/documents/[id]/+page.svelte';
import WelcomeDialog from '../modules/clients/WelcomeDialog.svelte';
import ChecklistItemDialog from '../modules/clients/ChecklistItemDialog.svelte';

export const components = {
	PortalDocumentPage,
	ChecklistItemDialog,
	WelcomeDialog,
	AssignSupplierDialog,
	NewTicketDialog,
	RequestChangeDialog,
	TicketMessageDialog,
	VisitDialog,
	BillingRunDialog,
	ContactDialog,
	DepreciationItemDialog,
	ExpenseNewPage,
	InsuranceDialog,
	Item21MappingDialog,
	InvoiceNewPage,
	InvoicePage,
	LeaseNewPage,
	LeasePage,
	LogTimeDialog,
	ProjectDialog,
	PortalRepairNewPage,
	PortalRepairPage,
	PropertyDetailsPage,
	PropertyNotesPage,
	SettingsPage,
	StrataSchemeDialog,
	StrataSection
} as const;
