// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export interface DailyBucket {
	date: string;
	total: number;
	finished: number;
	failed: number;
	other: number;
}

export interface StackSummary {
	stack_id: string;
	stack_name: string;
	total: number;
	finished: number;
	failed: number;
	plan_add: number;
	plan_change: number;
	plan_destroy: number;
}

export interface RunOverview {
	total_runs: number;
	finished: number;
	failed: number;
	success_rate: number;
	total_add: number;
	total_change: number;
	total_destroy: number;
}

export interface RunAnalytics {
	overview: RunOverview;
	daily: DailyBucket[];
	by_stack: StackSummary[];
	window_days: number;
}

export const analyticsApi = {
	getRuns(days = 30): Promise<RunAnalytics> {
		return request<RunAnalytics>(`/analytics/runs?days=${days}`);
	}
};
