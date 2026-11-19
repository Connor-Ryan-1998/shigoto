// Repair (maintenance) ticket types and labels, shared by the staff
// property pages and the portal (internal/property/routes_maintenance.go).

export type TicketStatus = 'new' | 'viewed' | 'assigned' | 'scheduled' | 'completed' | 'cancelled';
export type Urgency = 'routine' | 'soon' | 'urgent';

export type Ticket = {
	id: number;
	ref: string;
	property_id: number;
	property_address: string;
	lease_id: number | null;
	title: string;
	description: string;
	category: string;
	urgency: Urgency;
	status: TicketStatus;
	supplier: { id: number; name: string } | null;
	access_notes: string;
	raised_by_name: string;
	file_count: number;
	cancel_reason: string;
	created_at: string;
	viewed_at: string | null;
	completed_at: string | null;
	cancelled_at: string | null;
};

export type TicketEvent = {
	id: number;
	kind: string;
	body: string;
	data: Record<string, unknown>;
	actor_name: string;
	tenant_visible: boolean;
	created_at: string;
};

export type TicketFile = { id: number; mime: string; size_bytes: number; original_name: string };

export const CATEGORY_LABEL: Record<string, string> = {
	plumbing: 'Plumbing',
	electrical: 'Electrical',
	appliance: 'Appliance',
	heating_cooling: 'Heating & cooling',
	doors_windows_locks: 'Doors, windows & locks',
	pests: 'Pests',
	garden_exterior: 'Garden & exterior',
	other: 'Other'
};
export const CATEGORY_OPTIONS = Object.entries(CATEGORY_LABEL).map(([value, label]) => ({ value, label }));

export const URGENCY_LABEL: Record<Urgency, string> = { routine: 'Routine', soon: 'Soon', urgent: 'Urgent' };
export const URGENCY_TONE: Record<Urgency, 'info' | 'warning' | 'danger'> = { routine: 'info', soon: 'warning', urgent: 'danger' };

export const STATUS_LABEL: Record<TicketStatus, string> = {
	new: 'New',
	viewed: 'Viewed',
	assigned: 'Tradesperson assigned',
	scheduled: 'Visit scheduled',
	completed: 'Completed',
	cancelled: 'Cancelled'
};
export const STATUS_TONE: Record<TicketStatus, 'info' | 'warning' | 'success' | 'danger'> = {
	new: 'warning',
	viewed: 'info',
	assigned: 'info',
	scheduled: 'info',
	completed: 'success',
	cancelled: 'danger'
};

// EVENT_LABEL is the timeline wording for each event kind.
export function eventLabel(e: TicketEvent): string {
	switch (e.kind) {
		case 'created': return 'Request received';
		case 'viewed': return 'Viewed by your property manager';
		case 'assigned': return `Tradesperson assigned${e.data?.supplier ? `: ${e.data.supplier}` : ''}`;
		case 'completed': return 'Completed';
		case 'cancelled': return 'Cancelled';
		case 'comment': return 'Message';
		case 'note': return 'Internal note';
		case 'file_added': return 'Photo or recording added';
		default: return e.kind.replaceAll('_', ' ');
	}
}

export function isOpen(t: Pick<Ticket, 'status'>): boolean {
	return t.status !== 'completed' && t.status !== 'cancelled';
}

// mediaKind picks the player for a file. WebM's header can't tell audio
// from video, so a WebM named like a voice note (VoiceRecorder names clips
// voice-note-*) or with an audio extension gets the audio player.
export function mediaKind(mime: string, name = ''): 'image' | 'video' | 'audio' | 'other' {
	if (mime === 'video/webm' && /^voice|\.(weba|ogg|opus)$/i.test(name)) return 'audio';
	if (mime.startsWith('image/')) return 'image';
	if (mime.startsWith('video/')) return 'video';
	if (mime.startsWith('audio/')) return 'audio';
	return 'other';
}

export type EntryState = 'permitted' | 'tenant_home' | 'withdrawn' | 'unconfirmed';

export type Visit = {
	id: number;
	supplier: { id: number; name: string };
	starts_at: string;
	ends_at: string;
	status: 'booked' | 'confirmed' | 'change_requested' | 'cancelled' | 'done';
	tenant_confirmed_at: string | null;
	change_note: string;
	entry_state: EntryState;
	entry_granted_at: string | null;
	entry_withdrawn_at: string | null;
};

export type Window = { date: string; slot: 'morning' | 'afternoon' | 'custom'; start: string | null; end: string | null };

export const ENTRY_LABEL: Record<EntryState, string> = {
	permitted: 'Entry permitted',
	tenant_home: 'Tenant will be home',
	withdrawn: 'Entry withdrawn',
	unconfirmed: 'Entry: not yet confirmed'
};

export const ENTRY_TONE: Record<EntryState, 'success' | 'info' | 'danger' | 'warning'> = {
	permitted: 'success',
	tenant_home: 'info',
	withdrawn: 'danger',
	unconfirmed: 'warning'
};

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

// wall reads the date and time written in an RFC 3339 string - the org's
// local time the server sends - ignoring the viewer's own timezone.
function wall(iso: string) {
	const [, y, mo, d, h, mi] = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(iso) ?? [];
	return { y: +y, mo: +mo, d: +d, h: +h, mi: +mi };
}

function clock(h: number, mi: number): string {
	const suffix = h < 12 ? 'am' : 'pm';
	const h12 = h % 12 === 0 ? 12 : h % 12;
	return mi === 0 ? `${h12}${suffix}` : `${h12}:${String(mi).padStart(2, '0')}${suffix}`;
}

// visitWindow formats a visit as "Tue 24 Nov, 9am – 11am" in org time.
export function visitWindow(startISO: string, endISO: string): string {
	const s = wall(startISO);
	const e = wall(endISO);
	const day = DAYS[new Date(Date.UTC(s.y, s.mo - 1, s.d)).getUTCDay()];
	const sameDay = s.y === e.y && s.mo === e.mo && s.d === e.d;
	return `${day} ${s.d} ${MONTHS[s.mo - 1]}, ${clock(s.h, s.mi)} – ${sameDay ? '' : `${e.d} ${MONTHS[e.mo - 1]}, `}${clock(e.h, e.mi)}`;
}

// localInput turns an RFC 3339 org-time string into date and HH:MM parts.
export function localParts(iso: string): { date: string; time: string } {
	return { date: iso.slice(0, 10), time: iso.slice(11, 16) };
}

export function windowLabel(w: Window): string {
	const [y, m, d] = w.date.split('-').map(Number);
	const day = `${DAYS[new Date(Date.UTC(y, m - 1, d)).getUTCDay()]} ${d} ${MONTHS[m - 1]}`;
	if (w.slot === 'morning') return `${day} · Morning (8am–12pm)`;
	if (w.slot === 'afternoon') return `${day} · Afternoon (12–5pm)`;
	const [sh, sm] = (w.start ?? '00:00').split(':').map(Number);
	const [eh, em] = (w.end ?? '00:00').split(':').map(Number);
	return `${day} · ${clock(sh, sm)} – ${clock(eh, em)}`;
}
