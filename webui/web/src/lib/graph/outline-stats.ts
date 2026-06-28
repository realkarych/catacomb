import type { Node, Aggregate } from './types';
import { formatTokens, formatCost, formatDuration } from '../format/format';
import { isToolNode } from '../conversation';
import { statusColor } from '../status';

export interface RowStatOptions {
  collapsed: boolean;
  hasChildren: boolean;
  aggregate?: Aggregate;
}

export interface RowStat {
  text: string;
  title: string;
  color: string;
}

function aggregateStat(agg: Aggregate): RowStat {
  let text = `${agg.count} nodes · in ${formatTokens(agg.tokensIn)} · out ${formatTokens(agg.tokensOut)} · ${formatCost(agg.costUsd)}`;
  let title = `${agg.count} nodes · tokens in ${agg.tokensIn.toLocaleString('en-US')}, out ${agg.tokensOut.toLocaleString('en-US')}, cost ${formatCost(agg.costUsd)}`;
  if (agg.durationMs > 0) {
    text += ` · ${formatDuration(agg.durationMs)}`;
    title += `, duration ${formatDuration(agg.durationMs)}`;
  }
  return { text, title, color: statusColor(agg.status) };
}

function assistantStat(node: Node): RowStat {
  const parts: string[] = [];
  const titleParts: string[] = [];
  if (node.tokens_in !== undefined) {
    parts.push(`in ${formatTokens(node.tokens_in)}`);
    titleParts.push(`input tokens ${node.tokens_in.toLocaleString('en-US')}`);
  }
  if (node.tokens_out !== undefined) {
    parts.push(`out ${formatTokens(node.tokens_out)}`);
    titleParts.push(`output tokens ${node.tokens_out.toLocaleString('en-US')}`);
  }
  if (node.cost_usd !== undefined) {
    parts.push(formatCost(node.cost_usd));
    titleParts.push(`cost ${formatCost(node.cost_usd)}`);
  }
  if (node.duration_ms !== undefined) {
    parts.push(formatDuration(node.duration_ms));
    titleParts.push(`duration ${formatDuration(node.duration_ms)}`);
  }
  return { text: parts.join(' · '), title: titleParts.join(' · '), color: statusColor(node.status ?? '') };
}

function toolStat(node: Node): RowStat {
  if (node.duration_ms === undefined) {
    return { text: '', title: '', color: statusColor(node.status ?? '') };
  }
  const dur = formatDuration(node.duration_ms);
  return { text: dur, title: `Duration ${dur}`, color: statusColor(node.status ?? '') };
}

export function rowStatLine(node: Node, opts: RowStatOptions): RowStat {
  if (opts.collapsed && opts.hasChildren && opts.aggregate) {
    return aggregateStat(opts.aggregate);
  }
  if (node.type === 'assistant_turn') {
    return assistantStat(node);
  }
  if (isToolNode(node.type)) {
    return toolStat(node);
  }
  return { text: '', title: '', color: statusColor(node.status ?? '') };
}
