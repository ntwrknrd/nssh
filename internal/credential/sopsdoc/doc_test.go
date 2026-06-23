package sopsdoc

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	out   []byte
	err   error
	calls []fakeCall
}

type fakeCall struct {
	env  []string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, env []string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeCall{env: append([]string(nil), env...), args: append([]string(nil), args...)})
	return f.out, f.err
}

func TestSOPSDocLookupScalarPath(t *testing.T) {
	doc, err := Parse([]byte(`{"expedient":{"username":"cj","password":"secret"}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, ok, err := doc.Lookup("expedient.password")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok || got != "secret" {
		t.Fatalf("Lookup = %q, %v", got, ok)
	}
}

func TestSOPSDocLookupMissingPath(t *testing.T) {
	doc, err := Parse([]byte(`{"expedient":{"password":"secret"}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, ok, err := doc.Lookup("expedient.username")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok || got != "" {
		t.Fatalf("Lookup = %q, %v, want missing", got, ok)
	}
}

func TestSOPSDocLookupRejectsNonString(t *testing.T) {
	doc, err := Parse([]byte(`{"expedient":{"password":{"value":"secret"}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	_, _, err = doc.Lookup("expedient.password")
	if err == nil {
		t.Fatal("expected non-string error")
	}
	if !strings.Contains(err.Error(), "not a string") {
		t.Fatalf("error = %q", err)
	}
}

func TestSOPSDecryptUsesAgeKeyFileEnv(t *testing.T) {
	runner := &fakeRunner{out: []byte(`{"expedient":{"password":"secret"}}`)}

	doc, err := Decrypt(context.Background(), runner, "/tmp/credentials.sops.yaml", "/tmp/keys.txt")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if doc == nil {
		t.Fatal("doc is nil")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.calls))
	}
	if got := strings.Join(runner.calls[0].args, " "); got != "--decrypt --output-type json /tmp/credentials.sops.yaml" {
		t.Fatalf("args = %q", got)
	}
	if got := strings.Join(runner.calls[0].env, "\n"); !strings.Contains(got, "SOPS_AGE_KEY_FILE=/tmp/keys.txt") {
		t.Fatalf("env = %q", got)
	}
}

func TestSOPSDecryptDoesNotLogSecretPayload(t *testing.T) {
	runner := &fakeRunner{
		out: []byte(`{"expedient":{"password":"secret-value"}`),
		err: errors.New("exit status 1"),
	}

	_, err := Decrypt(context.Background(), runner, "/tmp/credentials.sops.yaml", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error leaked secret payload: %q", err)
	}
}
