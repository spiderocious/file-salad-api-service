package httpx

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeSuccessOmitsError(t *testing.T) {
	b, err := json.Marshal(Envelope{Data: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if want := `{"data":{"k":"v"}}`; s != want {
		t.Fatalf("got %s, want %s", s, want)
	}
}

func TestEnvelopeErrorShape(t *testing.T) {
	b, _ := json.Marshal(Envelope{Error: &APIError{
		Code:        CodeValidationError,
		Message:     "Validation failed",
		FieldErrors: map[string][]string{"email": {"Invalid email"}},
	}})
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["data"]; ok {
		t.Fatal("error envelope should omit data")
	}
	e := out["error"].(map[string]any)
	if e["code"] != "validation_error" {
		t.Fatalf("code = %v", e["code"])
	}
	if _, ok := e["field_errors"]; !ok {
		t.Fatal("missing field_errors")
	}
}

func TestEnvelopeMetaOmittedWhenEmpty(t *testing.T) {
	b, _ := json.Marshal(Envelope{Data: 1})
	if got := string(b); got != `{"data":1}` {
		t.Fatalf("meta should be omitted: %s", got)
	}
}
