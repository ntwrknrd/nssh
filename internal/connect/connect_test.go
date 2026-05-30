package connect

import (
	"errors"
	"testing"

	"github.com/ntwrknrd/nssh/internal/exit"
)

func TestHostNotFoundErrorCarriesHostname(t *testing.T) {
	var err error = &HostNotFoundError{Hostname: "edge01"}
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatal("HostNotFoundError should support errors.As")
	}
	if notFound.Hostname != "edge01" || notFound.Error() != "host not found: edge01" {
		t.Fatalf("notFound = %+v error=%q", notFound, notFound.Error())
	}
}

func TestIsCompatibilityError(t *testing.T) {
	if !isCompatibilityError(&exit.ExitError{Code: exit.ExitConnectionFailed}) {
		t.Fatal("connection failed exit should be compatibility candidate")
	}
	if !isCompatibilityError(&exit.ExitError{Code: 255}) {
		t.Fatal("ssh exit 255 should be compatibility candidate")
	}
	if isCompatibilityError(&exit.ExitError{Code: exit.ExitAuthFailed}) {
		t.Fatal("auth failure should not be compatibility candidate")
	}
	if isCompatibilityError(errors.New("plain error")) {
		t.Fatal("plain error should not be compatibility candidate")
	}
}

func TestExtractExplicitUser(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		sshArgs  []string
		want     string
	}{
		{name: "user at host", hostname: "admin@edge01", want: "admin"},
		{name: "split login flag", hostname: "edge01", sshArgs: []string{"-l", "admin"}, want: "admin"},
		{name: "joined login flag", hostname: "edge01", sshArgs: []string{"-ladmin"}, want: "admin"},
		{name: "no explicit user", hostname: "edge01", sshArgs: []string{"-p", "2222"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractExplicitUser(tt.hostname, tt.sshArgs); got != tt.want {
				t.Fatalf("extractExplicitUser() = %q, want %q", got, tt.want)
			}
		})
	}
}
