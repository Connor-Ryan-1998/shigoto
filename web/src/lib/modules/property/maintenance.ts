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
