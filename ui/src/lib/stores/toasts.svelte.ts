type ToastType = 'error' | 'success' | 'info';

export interface Toast {
	id: number;
	type: ToastType;
	message: string;
}

let list = $state<Toast[]>([]);
let _seq = 0;

function add(type: ToastType, message: string, duration = 4500) {
	const id = ++_seq;
	list = [...list, { id, type, message }];
	setTimeout(() => dismiss(id), duration);
}

export function dismiss(id: number) {
	list = list.filter((t) => t.id !== id);
}

export const toast = {
	get list() {
		return list;
	},
	error: (msg: string) => add('error', msg),
	success: (msg: string) => add('success', msg),
	info: (msg: string) => add('info', msg),
};
