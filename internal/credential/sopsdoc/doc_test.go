package sopsdoc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

	doc, err := Decrypt(context.Background(), runner, "/tmp/credentials.sops.yaml", "/tmp/keys.txt", "")
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

	_, err := Decrypt(context.Background(), runner, "/tmp/credentials.sops.yaml", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error leaked secret payload: %q", err)
	}
}

func TestSOPSDecryptMissingKeyGivesSafeActionableError(t *testing.T) {
	runner := &fakeRunner{
		out: []byte("Failed to get the data key required to decrypt the SOPS file.\nidentity-command-secret"),
		err: errors.New("exit status 128"),
	}
	_, err := Decrypt(context.Background(), runner, "/tmp/credentials.sops.json", "", "")
	if err == nil {
		t.Fatal("expected missing-key error")
	}
	for _, want := range []string{"decryption key unavailable", "age identity", "SOPS_AGE_KEY_CMD"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "identity-command-secret") {
		t.Fatal("error exposed identity command output")
	}
}

func TestSOPSProviderCommandOverridesOnlyChildEnvironment(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY_CMD", "/keys/inherited")
	t.Setenv("SOPS_AGE_KEY", "inherited-private-identity")
	t.Setenv("SOPS_AGE_KEY_FILE", "/keys/inherited.txt")
	script := filepath.Join(t.TempDir(), "sops")
	body := `#!/bin/sh
if [ -n "$SOPS_AGE_KEY" ] || [ -n "$SOPS_AGE_KEY_FILE" ]; then exit 20; fi
printf '{"password":"%s"}\n' "$SOPS_AGE_KEY_CMD"
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"/keys/personal", "/keys/work", "/keys/personal"} {
		doc, err := Decrypt(context.Background(), CLIRunner{Command: script}, "fixture.sops.json", "", command)
		if err != nil {
			t.Fatal(err)
		}
		got, ok, err := doc.Lookup("password")
		if err != nil || !ok || got != command {
			t.Fatalf("wrong provider command: %q, %v, %v", got, ok, err)
		}
	}
	if os.Getenv("SOPS_AGE_KEY_CMD") != "/keys/inherited" || os.Getenv("SOPS_AGE_KEY") != "inherited-private-identity" || os.Getenv("SOPS_AGE_KEY_FILE") != "/keys/inherited.txt" {
		t.Fatal("caller environment changed")
	}
}

func TestSOPSNoExplicitKeySourcePreservesEnvironment(t *testing.T) {
	runner := &fakeRunner{out: []byte(`{"password":"fixture"}`)}
	if _, err := Decrypt(context.Background(), runner, "fixture.sops.json", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls[0].env) != 0 {
		t.Fatalf("unexpected environment override: %v", runner.calls[0].env)
	}
}

func TestSOPSKeyCommandFailureDoesNotExposeOutput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "sops")
	body := "#!/bin/sh\nprintf '%s\\n' 'Failed to get the data key required to decrypt' 'private-identity-output' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := Decrypt(context.Background(), CLIRunner{Command: script}, "fixture.sops.json", "", "/keys/missing")
	if err == nil || !strings.Contains(err.Error(), "decryption key unavailable") || !strings.Contains(err.Error(), "age_key_command") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "private-identity-output") {
		t.Fatal("identity command output leaked")
	}
}
