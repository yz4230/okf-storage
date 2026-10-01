package localbundle

import (
	"encoding/json"
	"testing"

	"github.com/yz4230/okf-storage/internal/okf"
)

func TestMatch(t *testing.T) {
	const fm = `
type: Concept
draft: false
year: 2024
offset: -2
ratio: 0.5
empty: null
tags: [go, mcp, 2]
owner:
  name: yz
  teams: [a, b]
items:
  - {id: 1}
  - {id: 2}
`
	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{"empty filter", `{}`, true},
		{"string", `{"type": "Concept"}`, true},
		{"string mismatch", `{"type": "Guide"}`, false},
		{"missing key", `{"status": "done"}`, false},
		{"all keys", `{"type": "Concept", "year": 2024}`, true},
		{"one key mismatch", `{"type": "Concept", "year": 2023}`, false},
		{"bool", `{"draft": false}`, true},
		{"bool mismatch", `{"draft": true}`, false},
		{"null", `{"empty": null}`, true},
		{"null vs value", `{"type": null}`, false},

		{"uint64 vs float64", `{"year": 2024}`, true},
		{"int64 vs float64", `{"offset": -2}`, true},
		{"float64", `{"ratio": 0.5}`, true},
		{"number vs string", `{"year": "2024"}`, false},
		{"string vs number", `{"type": 1}`, false},
		{"bool vs number", `{"draft": 0}`, false},

		{"list contains", `{"tags": "mcp"}`, true},
		{"list contains number", `{"tags": 2}`, true},
		{"list lacks", `{"tags": "rust"}`, false},
		{"list contains all", `{"tags": ["go", "mcp"]}`, true},
		{"list contains some", `{"tags": ["go", "rust"]}`, false},
		{"empty list filter", `{"tags": []}`, true},
		{"list filter vs scalar", `{"type": ["Concept"]}`, false},

		{"nested", `{"owner": {"name": "yz"}}`, true},
		{"nested mismatch", `{"owner": {"name": "other"}}`, false},
		{"nested missing key", `{"owner": {"email": "x"}}`, false},
		{"nested list", `{"owner": {"teams": "b"}}`, true},
		{"map filter vs scalar", `{"type": {"name": "yz"}}`, false},
		{"scalar filter vs map", `{"owner": "yz"}`, false},
		{"list of maps", `{"items": {"id": 2}}`, true},
		{"list of maps lacks", `{"items": {"id": 3}}`, false},
		{"scalar filter vs list of maps", `{"items": 1}`, false},
	}

	doc, err := okf.ParseDocument("---" + fm + "---\n")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter map[string]any
			if err := json.Unmarshal([]byte(tt.filter), &filter); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", tt.filter, err)
			}
			if got := match(map[string]any(doc.Frontmatter), filter); got != tt.want {
				t.Errorf("match(%s) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}
