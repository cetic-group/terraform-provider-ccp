// Tests for the ccp_iam_policy_document data source.
//
// The data source is a pure transformation — no HTTP backend. We exercise
// `canonicalizeJSON` directly (it does all the heavy lifting) and verify
// bit-for-bit reproducibility.
package iampolicydocument

import (
	"encoding/json"
	"strings"
	"testing"
)

// ─── 1. Canonicalisation is bit-for-bit stable across runs ─────────────────

func TestCanonicalizeJSON_DeterministicKeyOrder(t *testing.T) {
	input := map[string]any{
		"version": "2026-05-10",
		"statements": []any{
			map[string]any{
				"effect":    "Allow",
				"actions":   []any{"registry:Push", "registry:Pull"},
				"resources": []any{"arn:ccp:registry:*:t:*"},
			},
		},
	}
	out, err := canonicalizeJSON(input)
	if err != nil {
		t.Fatalf("canonicalizeJSON: %v", err)
	}
	// Keys at top level: "statements" before "version" (lexicographic).
	if !strings.HasPrefix(out, `{"statements":`) {
		t.Errorf("expected canonical form to start with statements, got: %s", out)
	}
	// Re-run with shuffled input map — same output expected.
	input2 := map[string]any{
		"statements": []any{
			map[string]any{
				"resources": []any{"arn:ccp:registry:*:t:*"},
				"actions":   []any{"registry:Push", "registry:Pull"},
				"effect":    "Allow",
			},
		},
		"version": "2026-05-10",
	}
	out2, err := canonicalizeJSON(input2)
	if err != nil {
		t.Fatalf("canonicalizeJSON (shuffled): %v", err)
	}
	if out != out2 {
		t.Errorf("canonicalisation not stable across maps:\n  %s\nvs\n  %s", out, out2)
	}
}

// ─── 2. Simple Allow on registry:* ─────────────────────────────────────────

func TestCanonicalize_SimpleAllow(t *testing.T) {
	doc := map[string]any{
		"version": "2026-05-10",
		"statements": []any{
			map[string]any{
				"sid":       "AllowRegistry",
				"effect":    "Allow",
				"actions":   []any{"registry:*"},
				"resources": []any{"*"},
			},
		},
	}
	got, err := canonicalizeJSON(doc)
	if err != nil {
		t.Fatalf("canonicalizeJSON: %v", err)
	}
	// Validate it round-trips JSON.
	var back any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, got)
	}
}

// ─── 3. Deny statement renders effect=Deny ─────────────────────────────────

func TestCanonicalize_Deny(t *testing.T) {
	doc := map[string]any{
		"version": "2026-05-10",
		"statements": []any{
			map[string]any{
				"effect":    "Deny",
				"actions":   []any{"registry:DeleteRegistry"},
				"resources": []any{"*"},
			},
		},
	}
	got, err := canonicalizeJSON(doc)
	if err != nil {
		t.Fatalf("canonicalizeJSON: %v", err)
	}
	if !strings.Contains(got, `"effect":"Deny"`) {
		t.Errorf("Deny effect not present: %s", got)
	}
}

// ─── 4. Multi-statement preserves order ────────────────────────────────────

func TestCanonicalize_MultiStatement(t *testing.T) {
	doc := map[string]any{
		"version": "2026-05-10",
		"statements": []any{
			map[string]any{
				"sid":       "S1",
				"effect":    "Allow",
				"actions":   []any{"registry:Pull"},
				"resources": []any{"*"},
			},
			map[string]any{
				"sid":       "S2",
				"effect":    "Deny",
				"actions":   []any{"registry:DeleteRegistry"},
				"resources": []any{"*"},
			},
		},
	}
	got, err := canonicalizeJSON(doc)
	if err != nil {
		t.Fatalf("canonicalizeJSON: %v", err)
	}
	// S1 must appear before S2 in the rendered output (list order is preserved).
	idxS1 := strings.Index(got, `"sid":"S1"`)
	idxS2 := strings.Index(got, `"sid":"S2"`)
	if idxS1 < 0 || idxS2 < 0 || idxS1 > idxS2 {
		t.Errorf("statement order not preserved in canonical output:\n  S1=%d S2=%d\n  %s",
			idxS1, idxS2, got)
	}
}

// ─── 5. With condition — composes {test:{var:[vals]}} ──────────────────────

func TestCanonicalize_WithCondition(t *testing.T) {
	doc := map[string]any{
		"version": "2026-05-10",
		"statements": []any{
			map[string]any{
				"effect":    "Allow",
				"actions":   []any{"registry:Push"},
				"resources": []any{"*"},
				"condition": map[string]map[string][]string{
					"IpAddress": {
						"SourceIp": {"203.0.113.0/24"},
					},
				},
			},
		},
	}
	got, err := canonicalizeJSON(doc)
	if err != nil {
		t.Fatalf("canonicalizeJSON: %v", err)
	}
	if !strings.Contains(got, `"condition":{"IpAddress":{"SourceIp":["203.0.113.0/24"]}}`) {
		t.Errorf("condition block did not canonicalise as expected: %s", got)
	}
}

// ─── 6. Output is bit-for-bit stable on identical input ────────────────────

func TestCanonicalize_BitForBitStable(t *testing.T) {
	doc := map[string]any{
		"version": "2026-05-10",
		"statements": []any{
			map[string]any{
				"sid":       "S1",
				"effect":    "Allow",
				"actions":   []any{"registry:Push", "registry:Pull", "bucket:GetObject"},
				"resources": []any{"arn:ccp:registry:rnn:t1:*", "arn:ccp:bucket:rnn:t1:bucket/*"},
				"condition": map[string]map[string][]string{
					"StringEquals": {
						"OrgId": {"org-1"},
					},
					"IpAddress": {
						"SourceIp": {"10.0.0.0/8", "192.168.0.0/16"},
					},
				},
			},
		},
	}
	const N = 25
	var first string
	for i := 0; i < N; i++ {
		got, err := canonicalizeJSON(doc)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("iter %d diverged from iter 0:\n  %s\n  vs\n  %s", i, got, first)
		}
	}
}
