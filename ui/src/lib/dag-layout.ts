// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Layered DAG layout for the /dependencies visual graph view.
//
// Hand-rolled rather than pulling in dagre/elkjs because the graph is
// small (org-wide stack deps — typically tens to low hundreds of nodes),
// and the layout doesn't need to be optimal. Longest-path layering +
// barycenter-style ordering is good enough; if a real customer hits a
// graph this can't render, swap in dagre at that point.

import type { DagNode, DagEdge } from './api/client';

export interface LaidOutNode {
	id: string;
	name: string;
	slug: string;
	layer: number; // column index from left
	row: number;   // row within column
	x: number;     // top-left SVG coord
	y: number;
}

export interface LaidOutEdge {
	upstream_id: string;
	downstream_id: string;
	trigger_when_field?: string;
	trigger_when_op?: string;
	trigger_when_value?: string;
	retry_count: number;
	retry_backoff_seconds: number;
	d: string; // SVG path
	labelX: number;
	labelY: number;
	hasBadge: boolean; // true if predicate or retry config set
}

export interface DagLayout {
	width: number;
	height: number;
	nodes: LaidOutNode[];
	edges: LaidOutEdge[];
}

export interface LayoutOpts {
	nodeWidth: number;
	nodeHeight: number;
	colGap: number;
	rowGap: number;
	padding: number;
}

const DEFAULTS: LayoutOpts = {
	nodeWidth: 180,
	nodeHeight: 50,
	colGap: 100,
	rowGap: 16,
	padding: 24
};

// layoutDag converts the API payload into placed nodes + edges.
// Disconnected components stack vertically in the leftmost-empty column;
// graph cycles (shouldn't happen — the backend rejects them at edge create
// time) are surfaced by leaving their downstream nodes in layer 0.
export function layoutDag(nodes: DagNode[], edges: DagEdge[], opts: Partial<LayoutOpts> = {}): DagLayout {
	const o = { ...DEFAULTS, ...opts };
	const byID = new Map<string, DagNode>(nodes.map((n) => [n.id, n]));
	const incoming = new Map<string, string[]>();
	const outgoing = new Map<string, string[]>();
	for (const n of nodes) {
		incoming.set(n.id, []);
		outgoing.set(n.id, []);
	}
	for (const e of edges) {
		if (!byID.has(e.upstream_id) || !byID.has(e.downstream_id)) continue;
		incoming.get(e.downstream_id)!.push(e.upstream_id);
		outgoing.get(e.upstream_id)!.push(e.downstream_id);
	}

	// Longest-path layering via Kahn-style topological pass.
	const layer = new Map<string, number>();
	const remaining = new Map<string, number>(
		nodes.map((n) => [n.id, incoming.get(n.id)!.length])
	);
	const ready: string[] = [];
	for (const [id, n] of remaining) {
		if (n === 0) ready.push(id);
	}
	while (ready.length > 0) {
		const id = ready.shift()!;
		const upLayers = incoming.get(id)!.map((u) => layer.get(u) ?? 0);
		layer.set(id, upLayers.length === 0 ? 0 : Math.max(...upLayers) + 1);
		for (const d of outgoing.get(id)!) {
			const r = (remaining.get(d) ?? 0) - 1;
			remaining.set(d, r);
			if (r === 0) ready.push(d);
		}
	}
	// Any node still without a layer is part of a cycle — pin to layer 0
	// so it shows up rather than disappearing.
	for (const n of nodes) {
		if (!layer.has(n.id)) layer.set(n.id, 0);
	}

	// Bucket by layer
	const layers: string[][] = [];
	for (const n of nodes) {
		const l = layer.get(n.id)!;
		(layers[l] ??= []).push(n.id);
	}

	// Order within each layer by barycenter of incoming nodes' positions.
	// One pass is enough for small graphs; multi-pass crossings minimisation
	// is the swap-in-dagre point.
	const positions = new Map<string, number>(); // node id → row index
	layers.forEach((col, li) => {
		if (li === 0) {
			col.forEach((id, i) => positions.set(id, i));
			return;
		}
		const scored = col.map((id) => {
			const ups = incoming.get(id)!;
			if (ups.length === 0) return { id, score: Infinity };
			const avg = ups.reduce((s, u) => s + (positions.get(u) ?? 0), 0) / ups.length;
			return { id, score: avg };
		});
		scored.sort((a, b) => a.score - b.score);
		scored.forEach((s, i) => positions.set(s.id, i));
		// Mutate the column to match the new order so the layers array
		// stays consistent with positions for any later passes.
		col.sort((a, b) => positions.get(a)! - positions.get(b)!);
	});

	// Place nodes
	const laidOut: LaidOutNode[] = [];
	let maxRow = 0;
	for (const n of nodes) {
		const l = layer.get(n.id)!;
		const r = positions.get(n.id)!;
		if (r > maxRow) maxRow = r;
		laidOut.push({
			id: n.id,
			name: n.name,
			slug: n.slug,
			layer: l,
			row: r,
			x: o.padding + l * (o.nodeWidth + o.colGap),
			y: o.padding + r * (o.nodeHeight + o.rowGap)
		});
	}

	const width = o.padding * 2 + layers.length * o.nodeWidth + Math.max(0, layers.length - 1) * o.colGap;
	const height = o.padding * 2 + (maxRow + 1) * o.nodeHeight + maxRow * o.rowGap;

	// Build edge paths
	const nodeByID = new Map<string, LaidOutNode>(laidOut.map((n) => [n.id, n]));
	const laidEdges: LaidOutEdge[] = [];
	for (const e of edges) {
		const u = nodeByID.get(e.upstream_id);
		const d = nodeByID.get(e.downstream_id);
		if (!u || !d) continue;
		const x1 = u.x + o.nodeWidth;
		const y1 = u.y + o.nodeHeight / 2;
		const x2 = d.x;
		const y2 = d.y + o.nodeHeight / 2;
		const mx = (x1 + x2) / 2;
		laidEdges.push({
			upstream_id: e.upstream_id,
			downstream_id: e.downstream_id,
			trigger_when_field: e.trigger_when_field,
			trigger_when_op: e.trigger_when_op,
			trigger_when_value: e.trigger_when_value,
			retry_count: e.retry_count,
			retry_backoff_seconds: e.retry_backoff_seconds,
			d: `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`,
			labelX: mx,
			labelY: (y1 + y2) / 2,
			hasBadge: !!e.trigger_when_field || e.retry_count > 0
		});
	}

	return { width, height, nodes: laidOut, edges: laidEdges };
}
