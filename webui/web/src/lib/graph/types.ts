import type { Node, Edge } from '../types';

export type { Node, Edge };

export interface Hierarchy {
  childrenOf(id: string): string[];
  parentOf(id: string): string | undefined;
  ancestorsOf(id: string): string[];
  descendantsOf(id: string): string[];
  roots: string[];
  orphans: string[];
}

export interface Aggregate {
  count: number;
  tokensIn: number;
  tokensOut: number;
  costUsd: number;
  status: 'ok' | 'running' | 'error';
  hasError: boolean;
  durationMs: number;
}
