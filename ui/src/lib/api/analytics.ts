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
	cost_add: number;
	cost_change: number;
	cost_remove: number;
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

export interface DailyCostBucket {
	date: string;
	cost_add: number;
	cost_change: number;
	cost_remove: number;
	run_count: number;
}

export interface StackCostSummary {
	stack_id: string;
	stack_name: string;
	cost_add: number;
	cost_change: number;
	cost_remove: number;
	net_delta: number;
	budget_threshold_usd: number | null;
	runs_with_cost: number;
	last_cost_currency: string;
}

export interface CostOverview {
	total_cost_add: number;
	total_cost_change: number;
	total_cost_remove: number;
	net_delta: number;
	runs_with_cost: number;
}

export interface CostAnalytics {
	overview: CostOverview;
	daily: DailyCostBucket[];
	by_stack: StackCostSummary[];
	window_days: number;
}

export interface CostPoint {
	run_id: string;
	queued_at: string;
	cost_add: number;
	cost_change: number;
	cost_remove: number;
	currency: string;
}

export const analyticsApi = {
	getRuns(days = 30): Promise<RunAnalytics> {
		return request<RunAnalytics>(`/analytics/runs?days=${days}`);
	},
	getCosts(days = 30): Promise<CostAnalytics> {
		return request<CostAnalytics>(`/analytics/costs?days=${days}`);
	},
	getStackCostHistory(stackID: string): Promise<CostPoint[]> {
		return request<CostPoint[]>(`/stacks/${stackID}/analytics/costs`);
	}
};
