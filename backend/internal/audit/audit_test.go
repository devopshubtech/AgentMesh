package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"testing"
)

// The chain must verify after a jsonb round trip, which reorders keys and
// normalizes whitespace and numbers.
func TestNormalizeDetailsSurvivesJSONBRoundTrip(t *testing.T) {
	original, _ := json.Marshal(map[string]any{"z": 1, "a": []any{"x", 2.5}, "m": map[string]any{"k": nil, "b": true}})
	jsonbText := []byte(`{"a": ["x", 2.5], "m": {"b": true, "k": null}, "z": 1}`)
	a, err := normalizeDetails(original)
	if err != nil {
		t.Fatal(err)
	}
	b, err := normalizeDetails(jsonbText)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("normalized forms differ:\n%s\n%s", a, b)
	}
}

func TestHashChaining(t *testing.T) {
	c := canonical{OrgID: "o", TS: "t", ActorType: ActorUser, Action: "x", Outcome: Success, Details: json.RawMessage("{}")}
	genesis := make([]byte, sha256.Size)
	h1, err := computeHash(genesis, c)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := computeHash(h1, c)
	if bytes.Equal(h1, h2) {
		t.Fatal("identical rows with different predecessors must hash differently")
	}
	c.Action = "y"
	h1b, _ := computeHash(genesis, c)
	if bytes.Equal(h1, h1b) {
		t.Fatal("changing a field must change the hash")
	}
}
