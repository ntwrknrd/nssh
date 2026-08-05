# Migrate nssh 0.2 To 0.3

Audience: agents helping a user move from the latest stable nssh release,
currently 0.2.4, to the future 0.3 release.

## Migration Contract

Treat 0.3 as a configuration and credential-model cutover, not an in-place
schema upgrade.

- 0.3 does not read `config.toml`.
- 0.3 does not open or convert the 0.2 `credentials.age` vault.
- 0.3 does not use contexts or nssh-managed `~/.ssh/conf.d` files as its
  runtime inventory.
- `nssh self import ssh-config` is the supported one-way bridge for OpenSSH
  hosts and options. It does not migrate passwords.
- Existing recordings and audit files remain under the same XDG state paths.
- Do not recommend `nssh self reset` or a non-preserving uninstall as a
  migration step. Both can delete material needed for rollback.

The safe default is side-by-side configuration: preserve the 0.2 files, create
new 0.3 YAML, import SSH data, recreate auth mappings against an external
credential provider, verify, and remove obsolete files only after acceptance.

## What Changed

| 0.2 | 0.3 | Migration action |
| --- | --- | --- |
| `~/.config/nssh/config.toml` | `~/.config/nssh/config.yaml` | Recreate settings in YAML; there is no TOML converter. |
| Local encrypted `credentials.age` vault | SOPS+age, 1Password, or Bitwarden | Move secrets to an external provider and create inventory auth mappings. |
| `age.pub`, `age.key.enc`, or `piv.json` | Provider-owned authentication | Preserve for rollback; 0.3 does not read them. |
| Vault contexts | Inventory provider groups | Convert context domain and fallback auth into local group match/auth policy. |
| Vault host credentials | Inventory host/group auth references | Create provider references; do not put plaintext passwords in nssh YAML. |
| nssh-managed `~/.ssh/conf.d/*` | `~/.config/nssh/inventory/*.yaml` | Import with `nssh self import ssh-config`, then review. |
| `nssh host` and `nssh ctx` | `nssh inv` | Use local inventory hosts and provider-qualified groups. |
| `nssh connect HOST` | `nssh HOST` | Use root OpenSSH grammar; use `--select` or `--target` when needed. |
| `nssh lock` and `nssh unlock` | `nssh agent stop`, `status`, and `reset` | The agent brokers retained provider access; it is not a vault unlock session. |
| Shell integration and generated completions | Unsupported | Remove stale sourced scripts or completions after verifying shell startup. |
| Recording `window_size` | `logging.export.gif.window_size` | Keep the live PTY unsized; configure dimensions only for GIF export. |

## Before Replacing 0.2

1. Record the installed version and stop the old unlocked vault session:

   ```bash
   nssh --version
   nssh lock
   ```

2. Back up the complete nssh and managed SSH surface with permissions intact.
   Respect XDG overrides if the user set them.

   ```bash
   backup_dir="$HOME/nssh-0.2-backup-$(date +%Y%m%d-%H%M%S)"
   mkdir -m 700 "$backup_dir"
   copy_if_present() {
     if [ -e "$1" ]; then
       cp -a "$1" "$2"
     fi
   }
   copy_if_present "$HOME/.config/nssh" "$backup_dir/config-nssh"
   copy_if_present "$HOME/.local/share/nssh" "$backup_dir/share-nssh"
   copy_if_present "$HOME/.local/state/nssh" "$backup_dir/state-nssh"
   copy_if_present "$HOME/.ssh/config" "$backup_dir/ssh-config"
   copy_if_present "$HOME/.ssh/conf.d" "$backup_dir/ssh-conf.d"
   ```

   Include any custom recording, archive, SSH include, or XDG paths from
   `config.toml`.

3. Inventory the old model while the 0.2 binary and vault are still usable:

   ```bash
   nssh ctx list
   nssh host list
   nssh self cfg
   ```

   Capture context names, domains, SSH include files, usernames, host aliases,
   and whether auth is key- or password-based. Transfer passwords directly into
   the chosen external provider; avoid plaintext migration notes or shell
   history. Keep the old binary and backup until every required credential has
   been tested in 0.3.

## Build The 0.3 Configuration

1. Install both `nssh` and the matching `nssh-askpass` helper from the same 0.3
   release. Password authentication fails if the helper is not beside the main
   binary.

2. Run first-time initialization. A retained `config.toml` does not block this
   because 0.3 checks only `config.yaml`.

   ```bash
   nssh self init
   ```

   Select the intended credential provider and local inventory, or add them
   explicitly:

   ```bash
   nssh self init --cred sops-age
   nssh self init --inv local
   ```

   Provider files are written beneath `~/.config/nssh/credential/` and
   `~/.config/nssh/inventory/`; the root YAML activates them through includes.

3. Translate only the 0.2 TOML settings still supported in 0.3. Use
   `nssh self cfg --source` and `internal/config/example_config.yaml` as the
   schema authority. Important differences include:

- remove all `agent.security` and vault lockout/KDF settings
- remove `host.defaults.default_user`; usernames now belong to inventory auth
- move recording dimensions to `logging.export.gif`
- omit archive scheduling and jitter; `nssh log archive` is operator-scheduled
- place reusable OpenSSH options under `ssh.defaults.options`
- keep `agent` settings only for retained 1Password or Bitwarden access

4. Convert each 0.2 context into a local inventory group when the context still
   represents shared policy. A context domain becomes `match.domain_suffix`; a
   context fallback username/password becomes group `auth`. The old
   `git_include_file` has no 0.3 equivalent.

   ```yaml
   inventory:
     providers:
       local:
         type: local
         groups:
           work:
             match:
               domain_suffix:
                 - .example.com
             auth:
               mode: password
               credential_provider: op-work
               username: netops
               password_ref: op://Work/network-admin/password
   ```

   Create groups before importing SSH config. The importer can assign an
   imported host to a single matching local group by domain suffix; ambiguous or
   unmatched hosts remain ungrouped and are still valid.

5. Import the existing OpenSSH tree:

   ```bash
   nssh self import ssh-config
   ```

   The command follows `Include` directives, presents unified diffs for review,
   writes global `Host *` options to `config.yaml`, and writes concrete hosts to
   `inventory/local.yaml`. When `HostName` exists, it becomes the inventory host
   and the original `Host` token becomes an alias. Existing inventory identities
   are skipped rather than overwritten.

   Review every warning. Wildcard or negated host patterns and `Match` blocks
   are not inventory entries. `CanonicalizeHostname` is not imported. Unknown
   per-host directives may be preserved as typed SSH options only when the 0.3
   schema accepts them.

6. Add auth mappings after import. Imported `User` values become usernames, but
   passwords and context fallback credentials do not migrate. Prefer group auth
   for shared credentials and host auth only for exceptions:

   ```bash
   nssh inv set local/work --auth password --cred op-work:op://Work/network-admin/password --user netops
   nssh inv set edge01.example.com --auth key --user netops
   ```

   Confirm the exact `--cred` form with `nssh inv set --help`; interactive
   provider selection is safer when a provider reference contains punctuation.

## State, Data, And Obsolete Files

These paths retain their role and can stay in place:

- `~/.local/state/nssh/casts/`: recordings and sidecar indexes
- `~/.local/state/nssh/archives/`: recording archives
- `~/.local/state/nssh/audit.log*`: audit history
- `~/.local/share/nssh/benchmarks/`: benchmark artifacts

These 0.2 artifacts are ignored by normal 0.3 operation but should remain in the
rollback backup until migration is accepted:

- `~/.config/nssh/config.toml`
- `~/.config/nssh/age.pub`
- `~/.config/nssh/age.key.enc`
- `~/.config/nssh/piv.json`
- `~/.local/share/nssh/credentials.age`
- legacy credential and SSH-config backups under
  `~/.local/share/nssh/backups/`
- `~/.local/share/nssh/nssh-shell-integration.sh` or `.fish`
- old nssh completion files
- nssh-managed host files under `~/.ssh/conf.d/`

Do not delete the whole `~/.ssh/conf.d` directory: it may contain files owned by
the user or other tools. After 0.3 validation, remove only known nssh-owned
includes and shell startup lines. The OpenSSH files may also be intentionally
retained for direct `ssh` compatibility, but nssh 0.3 does not use them as its
inventory authority.

External provider state is new non-secret JSON under
`~/.local/state/nssh/inventory/providers/`. It is a cache, not operator-owned
configuration. Rebuild it with `nssh inv refresh`; do not hand-edit or copy 0.2
vault data into it.

## Verify Before Cleanup

Run bounded checks in this order:

```bash
nssh self status
nssh self cfg --paths
nssh self cfg --source
nssh inv status
nssh inv list
nssh inv get <representative-host>
nssh -v <representative-key-host> true
nssh -v <representative-password-host> true
nssh agent status
```

Also test one host per migrated group, a host-specific credential override, SCP,
and any proxy/jump path. For external inventory, run `nssh inv refresh
<provider>` and recheck the resolved host. Verify a recording only if session
recording is enabled.

Migration is complete only when inventory identity, username, destination,
port, SSH options, auth provider/reference, proxy behavior, and connection all
match the intended 0.2 behavior.

## Rollback

Rollback means reinstalling the saved 0.2 binary and restoring the preserved
0.2 config, vault, SSH includes, and XDG state paths as one set. Do not point a
0.2 binary at 0.3 YAML or a 0.3 binary at `credentials.age`. Keep the 0.3 YAML
separate during rollback so it can be inspected or retried later.
