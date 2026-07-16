#!/usr/bin/env bash
# Scenario 60 — secret redaction invariant end-to-end: "payloads only ever leave
# through the redaction policy." The committed transcript template carries the
# literal __SECRET__ (never a real secret-shaped string, so gitleaks stays green);
# this scenario injects a FAKE-but-pattern-matching GitHub token at RUNTIME, benches
# it through the capture pipeline, and proves the raw token never lands in the
# persisted evidence — it is replaced by the ‹redacted:github-token› placeholder.
# The pack step re-proves the same over the third-party-auditor bundle. A standing
# non-vacuity guard asserts the raw token really is present in the pre-redaction
# source, so it is redaction (not a typo) that removes it. Sourced by run.sh with
# lib.sh loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.60 redaction: inject fake secret, bench through capture, assert scrubbed =="
w="$WORK/redaction"; mkdir -p "$w/cellwork" "$w/runs"

# Runtime-only fake GitHub token: matches redact's reGitHubToken (\bgh[pousr]_[A-Za-z0-9]{36,}\b)
# yet is obviously fake and never committed. It is assembled from inert chunks so no
# single committed line is itself secret-shaped (gitleaks scans committed lines); bash
# concatenates the full pattern-matching token only at runtime, in this process.
gh_prefix="ghp_"
gh_body="FAKEfakeFAKEfake0123456789ABCDEF012345"
fake_secret="${gh_prefix}${gh_body}"

# Render the transcript template, replacing the committed __SECRET__ placeholder with
# the runtime fake token. fake_secret is [A-Za-z0-9_] only, so it is sed-safe.
sed "s/__SECRET__/$fake_secret/g" "$PROD/fixtures/redaction.jsonl.tmpl" > "$w/redaction.jsonl.tmpl"

echo "== prod.60 redaction: non-vacuity — raw token present in pre-redaction source =="
rc=0; grep -Fq "$fake_secret" "$w/redaction.jsonl.tmpl" || rc=1
record "$rc" "non-vacuity: raw fake token present in the source transcript before redaction"

sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/redaction.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-redaction basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

echo "== prod.60 redaction: captured session.jsonl scrubbed at capture time =="
rid="bench-prod-redaction-redaction-baseline-r1"
sess="$w/runs/$rid/session.jsonl"
rc=0; [ -f "$sess" ] || rc=1
record "$rc" "captured session.jsonl exists in the run dir"
rc=0; if grep -Fq "$fake_secret" "$sess"; then rc=1; fi
record "$rc" "captured session.jsonl does NOT contain the raw fake token"
rc=0; grep -Fq '‹redacted:github-token›' "$sess" || rc=1
record "$rc" "captured session.jsonl carries the ‹redacted:github-token› placeholder"

echo "== prod.60 redaction: pack bundle (third-party-auditor path) also scrubbed =="
run_json 0 "$w/pack.out" "pack prod-redaction basket for external audit" -- \
  catacomb pack label:basket=prod-redaction --runs-dir "$w/runs" --out "$w/pack"
pack_sess="$w/pack/$rid/session.jsonl"
rc=0; [ -f "$pack_sess" ] || rc=1
record "$rc" "packed bundle carries the run's session.jsonl"
rc=0; if grep -Fq "$fake_secret" "$pack_sess"; then rc=1; fi
record "$rc" "packed session.jsonl does NOT contain the raw fake token"
rc=0; grep -Fq '‹redacted:github-token›' "$pack_sess" || rc=1
record "$rc" "packed session.jsonl carries the ‹redacted:github-token› placeholder"
