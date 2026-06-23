package credential

import (
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/agent"
)

type fakeAgentProviderClient struct {
	reqs      []agent.ProviderRequest
	responses []*agent.ProviderResponse
	errs      []error
	resp      *agent.ProviderResponse
	err       error
}

func (f *fakeAgentProviderClient) ProviderRequest(req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	f.reqs = append(f.reqs, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	if len(f.responses) > 0 {
		resp := f.responses[0]
		f.responses = f.responses[1:]
		return resp, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeAgentProviderClient) Close() error {
	return nil
}

func stubProviderAgent(t *testing.T, resp *agent.ProviderResponse) *fakeAgentProviderClient {
	t.Helper()
	oldConnect := connectProviderAgent
	oldSpawn := spawnRuntimeAgent
	client := &fakeAgentProviderClient{resp: resp}
	connectProviderAgent = func() (agentProviderClient, error) { return client, nil }
	spawnRuntimeAgent = func() error { t.Fatal("unexpected agent spawn"); return nil }
	t.Cleanup(func() {
		connectProviderAgent = oldConnect
		spawnRuntimeAgent = oldSpawn
	})
	return client
}

func revealTestSecret(t *testing.T, record *Record) string {
	t.Helper()
	if record == nil || record.Secret == nil {
		return ""
	}
	var value string
	if err := record.Secret.Use(func(data []byte) error {
		value = string(data)
		return nil
	}); err != nil {
		t.Fatalf("secret use: %v", err)
	}
	return value
}

func waitForCredentialAgent(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err := agent.Connect()
		if err == nil {
			_ = client.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not start in time")
}
