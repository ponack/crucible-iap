// Shared trigger badge styling for run lists.
export interface TriggerBadge {
	classes: string;
	label: string;
}

const badges: Record<string, TriggerBadge> = {
	push:            { classes: 'bg-blue-900 text-blue-300',    label: 'push' },
	pull_request:    { classes: 'bg-cyan-900 text-cyan-300',    label: 'PR' },
	dependency:      { classes: 'bg-violet-900 text-violet-300', label: 'dependency' },
	drift_detection: { classes: 'bg-yellow-900 text-yellow-400', label: 'drift' },
	auto_remediate:  { classes: 'bg-orange-900 text-orange-400', label: 'auto-remediate' },
	manual:          { classes: 'bg-zinc-800 text-zinc-400',    label: 'manual' },
	api:             { classes: 'bg-teal-900 text-teal-300',    label: 'api' }
};

export function triggerBadge(trigger: string): TriggerBadge {
	return badges[trigger] ?? { classes: 'bg-zinc-800 text-zinc-400', label: trigger };
}
