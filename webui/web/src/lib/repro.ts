import type { ReproMeta } from './types';

export function hasRepro(repro: ReproMeta | undefined): boolean {
  if (repro === undefined) return false;
  return Object.values(repro).some((v) => v !== undefined);
}

export function reproFingerprint(repro: ReproMeta | undefined): string {
  if (repro === undefined) return '';
  const segments = [
    repro.prompts_hash,
    repro.skills_hash,
    repro.subagents_hash,
    repro.catacomb_config_hash,
  ]
    .map((h) => (h ? h.slice(0, 6) : ''))
    .filter((s) => s !== '');
  return segments.join('-');
}
