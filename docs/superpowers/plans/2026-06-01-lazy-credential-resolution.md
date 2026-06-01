# Lazy Credential Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce connection startup time for password-auth hosts backed by external credential providers by deferring password provider calls until SSH actually prompts for a password.

**Architecture:** Keep host lookup and auth mapping selection in `internal/connect`, but split "credential metadata" from "secret retrieval." The connector receives an optional lazy password resolver and calls it only after prompt detection. Literal usernames stay fast; `username_ref` remains supported but is documented as slower because SSH needs the username before process spawn.

**Tech Stack:** Go, OpenSSH via PTY connector, Cobra command tests, `secret.Secret`, existing nssh agent/provider abstractions, Markdown docs validated with `validate-markdown`.

---

## File Structure

- Modify `internal/connect/resolve.go`: return credential metadata without eagerly fetching the password when possible; resolve `username_ref` only when needed to choose the SSH username.
- Modify `internal/connect/connect.go`: wire lazy password resolution into `connector.Connector`.
- Modify `internal/ssh/connector/connector.go`: add a password resolver field and setter without importing higher-level packages.
- Modify `internal/ssh/connector/password_unix.go`: resolve the password on first prompt and then inject it.
- Modify `internal/ssh/connector/relay_unix.go`: pass context into password injection.
- Modify `internal/credential/onepassword.go`: preserve current provider behavior; add focused tests proving direct password refs avoid username lookups when a literal username is configured.
- Modify `internal/connect/resolve_test.go`: cover lazy direct password refs, eager `username_ref`, and explicit-user mismatch behavior.
- Modify `internal/ssh/connector/password_unix_test.go`: cover lazy resolver success, resolver called once, and resolver failure leaves prompt visible.
- Modify `internal/cli/self/bench/common.go`: fix misleading stage math so benchmarks show real pre-connector, config, credential, first-read, and session I/O timings.
- Modify `docs/examples/config/config.example.toml`: document provider-call cost and recommend literal `username` plus direct password field refs for faster connections.
- Modify `.agents/skills/nssh/references/configuration-inventory-credentials.md`: document the same guidance for agents.
- Modify `docs/examples/output/benchmark-ssh.txt`: update sample output if stage labels change.

## Task 1: Add Lazy Password Resolver To Connector

**Files:**
- Modify: `internal/ssh/connector/connector.go`
- Modify: `internal/ssh/connector/password_unix.go`
- Modify: `internal/ssh/connector/relay_unix.go`
- Test: `internal/ssh/connector/password_unix_test.go`

- [ ] **Step 1: Write failing connector tests**

Add tests that exercise the connector password path without starting SSH:

```go
func TestResolvePasswordUsesLazyResolverOnce(t *testing.T) {
	c := NewConnector("edge01", "netops", nil, nil)
	calls := 0
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		calls++
		return secret.NewFromString("secret"), nil
	})

	first, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("first resolvePassword: %v", err)
	}
	second, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("second resolvePassword: %v", err)
	}
	if first == nil || second == nil {
		t.Fatal("resolved password is nil")
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestResolvePasswordReturnsExistingPasswordWithoutResolver(t *testing.T) {
	c := NewConnector("edge01", "netops", secret.NewFromString("secret"), nil)

	got, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("resolvePassword: %v", err)
	}
	if got == nil {
		t.Fatal("password is nil")
	}
}

func TestResolvePasswordPropagatesResolverError(t *testing.T) {
	c := NewConnector("edge01", "netops", nil, nil)
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		return nil, errors.New("provider unavailable")
	})

	got, err := c.resolvePassword(context.Background())
	if err == nil || err.Error() != "provider unavailable" {
		t.Fatalf("error = %v, want provider unavailable", err)
	}
	if got != nil {
		t.Fatalf("password = %+v, want nil", got)
	}
}
```

- [ ] **Step 2: Run connector tests and verify failure**

Run:

```bash
go test ./internal/ssh/connector -run 'TestResolvePassword' -count=1
```

Expected: compile failure for missing `SetPasswordResolver` or `resolvePassword`.

- [ ] **Step 3: Implement connector resolver**

Add to `Connector`:

```go
passwordResolver func(context.Context) (*secret.Secret, error)
```

Add setter:

```go
func (c *Connector) SetPasswordResolver(fn func(context.Context) (*secret.Secret, error)) {
	c.passwordResolver = fn
}
```

Add helper in `password_unix.go`:

```go
func (c *Connector) resolvePassword(ctx context.Context) (*secret.Secret, error) {
	if c.password != nil {
		return c.password, nil
	}
	if c.passwordResolver == nil {
		return nil, nil
	}
	pw, err := c.passwordResolver(ctx)
	if err != nil {
		return nil, err
	}
	c.password = pw
	return c.password, nil
}
```

Change `injectPassword()` to `injectPassword(ctx context.Context)` and call `resolvePassword(ctx)` before writing to the PTY. In `relay_unix.go`, change the prompt path to call `c.injectPassword(ctx)`.

- [ ] **Step 4: Run connector tests and verify pass**

Run:

```bash
go test ./internal/ssh/connector -run 'TestResolvePassword' -count=1
```

Expected: PASS.

## Task 2: Split Credential Metadata From Secret Retrieval

**Files:**
- Modify: `internal/connect/resolve.go`
- Modify: `internal/connect/connect.go`
- Test: `internal/connect/resolve_test.go`

- [ ] **Step 1: Write failing connect resolver tests**

Add a fake provider that records `GetRef` calls:

```go
type countingCredentialProvider struct {
	calls []config.CredentialRefConfig
	record *credential.Record
}

func (p *countingCredentialProvider) GetHost(host string) (*credential.Record, error) {
	return nil, nil
}

func (p *countingCredentialProvider) GetGroup(group string) (*credential.Record, error) {
	return nil, nil
}

func (p *countingCredentialProvider) GetRef(ref config.CredentialRefConfig) (*credential.Record, error) {
	p.calls = append(p.calls, ref)
	return p.record, nil
}
```

Add tests:

```go
func TestResolveInventoryCredentialDefersDirectPasswordRefWithLiteralUsername(t *testing.T) {
	provider := &countingCredentialProvider{
		record: &credential.Record{Username: "netops", Secret: secret.NewFromString("secret"), Ref: "op://Network/Edge/password"},
	}
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{"op-network": provider}}
	auth := config.InventoryAuthResolution{
		CredentialProvider: "op-network",
		PasswordRef:        "op://Network/Edge/password",
		Username:           "netops",
		Source:             "group local/customer",
	}

	cred, err := resolveInventoryCredential(registry, auth, "")
	if err != nil {
		t.Fatalf("resolveInventoryCredential: %v", err)
	}
	if cred == nil || cred.Username != "netops" || cred.Password != nil || cred.PasswordResolver == nil {
		t.Fatalf("credential = %+v, want lazy resolver with literal username", cred)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider calls = %d, want 0 before password prompt", len(provider.calls))
	}
}

func TestResolveInventoryCredentialResolvesUsernameRefBeforeSSHStart(t *testing.T) {
	provider := &countingCredentialProvider{
		record: &credential.Record{Username: "netops", Secret: secret.NewFromString("secret"), Ref: "op://Network/Edge/password"},
	}
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{"op-network": provider}}
	auth := config.InventoryAuthResolution{
		CredentialProvider: "op-network",
		PasswordRef:        "op://Network/Edge/password",
		UsernameRef:        "op://Network/Edge/username",
	}

	cred, err := resolveInventoryCredential(registry, auth, "")
	if err != nil {
		t.Fatalf("resolveInventoryCredential: %v", err)
	}
	if cred == nil || cred.Username != "netops" {
		t.Fatalf("credential = %+v, want username resolved before SSH start", cred)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1 for username_ref", len(provider.calls))
	}
}
```

- [ ] **Step 2: Run connect tests and verify failure**

Run:

```bash
go test ./internal/connect -run 'TestResolveInventoryCredential' -count=1
```

Expected: compile failure for missing `PasswordResolver` or behavior failure because the provider is called eagerly.

- [ ] **Step 3: Implement lazy credential metadata**

Extend `ResolvedCredential`:

```go
type ResolvedCredential struct {
	Username         string
	Password         *secret.Secret
	PasswordResolver func(context.Context) (*secret.Secret, error)
	Source           string
}
```

In `resolveInventoryCredential`, build a resolver closure around `provider.GetRef`. If `auth.Username` or `explicitUser` supplies the username and the password ref is a direct secret ref, return without calling the provider. If `auth.UsernameRef` is set and no literal or explicit username is available, call the provider before returning because SSH needs the username before spawn.

In `connect.newConnector`, preserve current eager-password behavior when `Credential.Password` is non-nil and also call:

```go
conn.SetPasswordResolver(resolved.Credential.PasswordResolver)
```

- [ ] **Step 4: Run connect tests and verify pass**

Run:

```bash
go test ./internal/connect -run 'TestResolveInventoryCredential|TestResolveBoundCredential|TestSelectConnectionUsername' -count=1
```

Expected: PASS.

## Task 3: Keep Provider Semantics And Document Username Cost

**Files:**
- Modify: `internal/credential/onepassword_test.go`
- Modify: `docs/examples/config/config.example.toml`
- Modify: `.agents/skills/nssh/references/configuration-inventory-credentials.md`

- [ ] **Step 1: Add provider call-count tests**

Add explicit tests to show current 1Password call behavior:

```go
func TestOnePasswordDirectPasswordRefWithLiteralUsernameUsesOneProviderRead(t *testing.T) {
	runner := &fakeOPRunner{outs: []fakeOPOut{{data: "secret"}}}
	provider := &onePasswordProvider{
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {
				Ref:      "op://Network/Edge 01/password",
				Username: "netops",
			},
		},
		runner: runner,
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "netops" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(runner.calls) != 1 {
		t.Fatalf("op calls = %d, want 1", len(runner.calls))
	}
	if strings.Join(runner.calls[0].args, " ") != "read op://Network/Edge 01/password" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}
```

- [ ] **Step 2: Run provider tests**

Run:

```bash
go test ./internal/credential -run 'TestOnePassword.*Ref' -count=1
```

Expected: PASS before and after the lazy connect change, proving provider behavior stays compatible.

- [ ] **Step 3: Update config example**

In `docs/examples/config/config.example.toml`, add this guidance near the host auth override example:

```toml
# Fastest password-auth form:
# - Use a literal username when possible.
# - Point password_ref at the exact password field.
# - Avoid username_ref unless the username must also come from the provider.
# Each external provider call adds connection startup time.
#
# auth = { credential_provider = "op-network", password_ref = "op://Network/edge01/password", username = "netops" }
#
# Supported but slower because it requires an extra provider lookup before SSH starts:
# auth = { credential_provider = "op-network", password_ref = "op://Network/edge01/password", username_ref = "op://Network/edge01/username" }
```

- [ ] **Step 4: Update agent skill reference**

In `.agents/skills/nssh/references/configuration-inventory-credentials.md`, add the same rule in prose: literal `username` plus direct password field ref is fastest; `username_ref` and item-base refs remain supported but can add provider calls; each external provider call increases connection time.

- [ ] **Step 5: Validate Markdown**

Run:

```bash
validate-markdown --file .agents/skills/nssh/references/configuration-inventory-credentials.md
```

Expected: PASS.

## Task 4: Fix Benchmark Stage Reporting

**Files:**
- Modify: `internal/cli/self/bench/common.go`
- Modify: `docs/examples/output/benchmark-ssh.txt`

- [ ] **Step 1: Add benchmark stats tests**

Add tests in `internal/cli/self/bench/common_test.go`:

```go
func TestComputeSessionIODeltasUsesPerSampleDurations(t *testing.T) {
	samples := []map[string]time.Duration{
		{connector.TimingFirstRead: 100 * time.Millisecond, connector.TimingSessionEnd: 140 * time.Millisecond},
		{connector.TimingFirstRead: 90 * time.Millisecond, connector.TimingSessionEnd: 130 * time.Millisecond},
	}

	stats := computeDeltaStats("session_io", samples, connector.TimingSessionEnd, connector.TimingFirstRead)
	if stats.Mean != 40*time.Millisecond || stats.Min != 40*time.Millisecond || stats.Max != 40*time.Millisecond {
		t.Fatalf("stats = %+v, want all 40ms", stats)
	}
}

func TestComputeStartupOverheadUsesPerSampleDurations(t *testing.T) {
	wallClocks := []time.Duration{100 * time.Millisecond, 120 * time.Millisecond}
	samples := []map[string]time.Duration{
		{connector.TimingTotal: 70 * time.Millisecond},
		{connector.TimingTotal: 80 * time.Millisecond},
	}

	stats := computeWallMinusStageStats("pre_connector", wallClocks, samples, connector.TimingTotal)
	if stats.Mean != 35*time.Millisecond || stats.Min != 30*time.Millisecond || stats.Max != 40*time.Millisecond {
		t.Fatalf("stats = %+v", stats)
	}
}
```

- [ ] **Step 2: Run benchmark tests and verify failure**

Run:

```bash
go test ./internal/cli/self/bench -run 'TestCompute.*Stats|TestComputeSessionIODeltas' -count=1
```

Expected: compile failure for missing helpers.

- [ ] **Step 3: Implement per-sample delta helpers**

Add helpers that compute deltas before aggregating:

```go
func computeDeltaStats(name string, samples []map[string]time.Duration, endStage, startStage string) StageStats {
	durations := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		end, okEnd := sample[endStage]
		start, okStart := sample[startStage]
		if okEnd && okStart && end >= start {
			durations = append(durations, end-start)
		}
	}
	return computeStatsFromDurations(name, durations)
}

func computeWallMinusStageStats(name string, wallClocks []time.Duration, samples []map[string]time.Duration, stage string) StageStats {
	limit := len(samples)
	if len(wallClocks) < limit {
		limit = len(wallClocks)
	}
	durations := make([]time.Duration, 0, limit)
	for i := 0; i < limit; i++ {
		if d, ok := samples[i][stage]; ok && wallClocks[i] >= d {
			durations = append(durations, wallClocks[i]-d)
		}
	}
	return computeStatsFromDurations(name, durations)
}
```

Use those helpers in `renderResults` and `renderResultsToString`. Rename the displayed `startup` row to `pre_connector` unless separate process-start timing is added; current math is wall clock minus connector total, not pure Go startup.

- [ ] **Step 4: Run benchmark tests**

Run:

```bash
go test ./internal/cli/self/bench -count=1
```

Expected: PASS.

## Task 5: Verify End-To-End Behavior

**Files:**
- No new files.

- [ ] **Step 1: Run targeted tests**

Run:

```bash
go test ./internal/ssh/connector ./internal/connect ./internal/credential ./internal/cli/self/bench -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Build the binary**

Run:

```bash
make build
```

Expected: build succeeds.

- [ ] **Step 4: Benchmark a direct password-ref host**

Run after installing or using the built binary:

```bash
nssh self bench ssh acm-lab-agg-sw1 --warmups 1 --samples 5
```

Expected: the benchmark includes `credential_lookup` only for hosts that actually need a provider call before SSH start. Direct password refs with literal usernames should move most provider delay out of `pre_connector` and into the password prompt path, overlapping with SSH startup where possible.

- [ ] **Step 5: Benchmark a key-auth host**

Run:

```bash
nssh self bench ssh rpi-a --warmups 1 --samples 5
```

Expected: no credential provider lookup for key-auth hosts; no regression in total time beyond normal noise.

## Risk Notes

- `username_ref` cannot be fully lazy when no explicit or literal username exists, because OpenSSH needs the username before process spawn.
- Item refs that require `op item get --reveal` may remain one provider call before or at prompt time depending on whether the username is needed early.
- Do not add long-lived password caching to the agent. That changes the product contract and security model.
- Keep SCP behavior aligned with connect; both use `ResolveHostForConnect`.

## Self-Review

- Spec coverage: the plan preserves `username_ref`, documents why it is slower, defers password provider calls for the fast path, and fixes benchmark reporting so improvements are measurable.
- Placeholder scan: no `TBD`, `TODO`, or unspecified "add tests" steps remain.
- Type consistency: the plan consistently uses `PasswordResolver func(context.Context) (*secret.Secret, error)` from connect through connector.
