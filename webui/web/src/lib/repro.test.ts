import { describe, it, expect } from 'vitest';
import { hasRepro, reproFingerprint } from './repro';
import type { ReproMeta } from './types';

describe('hasRepro', () => {
  it('returns false for undefined', () => {
    expect(hasRepro(undefined)).toBe(false);
  });

  it('returns false for empty object', () => {
    expect(hasRepro({})).toBe(false);
  });

  it('returns false when all fields are undefined', () => {
    const r: ReproMeta = { claude_code_version: undefined, cwd: undefined };
    expect(hasRepro(r)).toBe(false);
  });

  it('returns true when a hash field is present', () => {
    expect(hasRepro({ prompts_hash: 'abc123def' })).toBe(true);
  });

  it('returns true when only claude_code_version is present', () => {
    expect(hasRepro({ claude_code_version: '1.0.0' })).toBe(true);
  });

  it('returns true when catacomb_version is present', () => {
    expect(hasRepro({ catacomb_version: '0.5.0' })).toBe(true);
  });
});

describe('reproFingerprint', () => {
  it('returns empty string for undefined', () => {
    expect(reproFingerprint(undefined)).toBe('');
  });

  it('returns empty string when all 4 hashes are absent', () => {
    expect(reproFingerprint({ claude_code_version: '1.0.0', cwd: '/home' })).toBe('');
  });

  it('returns first 6 chars of single hash when only one present', () => {
    expect(reproFingerprint({ prompts_hash: 'abcdef1234' })).toBe('abcdef');
  });

  it('joins all 4 hashes with dash, each truncated to 6 chars', () => {
    const r: ReproMeta = {
      prompts_hash: '1a2b3cXXX',
      skills_hash: '4d5e6fXXX',
      subagents_hash: '7g8h9iXXX',
      catacomb_config_hash: 'jk1234XXX',
    };
    expect(reproFingerprint(r)).toBe('1a2b3c-4d5e6f-7g8h9i-jk1234');
  });

  it('skips missing hashes and joins remaining ones', () => {
    const r: ReproMeta = {
      prompts_hash: 'aaaaaa1234',
      catacomb_config_hash: 'bbbbbb1234',
    };
    expect(reproFingerprint(r)).toBe('aaaaaa-bbbbbb');
  });

  it('handles hash shorter than 6 chars', () => {
    expect(reproFingerprint({ prompts_hash: 'abc' })).toBe('abc');
  });
});
