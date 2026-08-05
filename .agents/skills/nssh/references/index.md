# nssh Reference Index

Select the smallest reference set that covers the request:

- For product contracts, package boundaries, and non-goals, read the file named
  SPEC.md at the repository root, then inspect current source for implementation
  details.
- For config files, includes, inventory groups, local inventory, NetBox,
  containerlab, credential providers, or auth mappings, read
  [configuration-inventory-credentials.md](configuration-inventory-credentials.md).
- For connect or SCP behavior, fuzzy matching, provider refresh, agent
  operations, host keys, legacy SSH fixes, managed proxies, recordings, logs,
  benchmarks, or diagnostics, read
  [connections-logs-troubleshooting.md](connections-logs-troubleshooting.md).
- For highlighting configuration, the Junos profile, remote-command output, or
  the interactive PTY highlighting boundary, read
  [terminal-highlighting.md](terminal-highlighting.md).
- For an upgrade from the latest stable 0.2 release to future 0.3, including
  backups, YAML initialization, OpenSSH import, context/inventory conversion,
  external credentials, retained or obsolete files, validation, and rollback,
  read [migration-0.2-to-0.3.md](migration-0.2-to-0.3.md).

Read both configuration and connection references only when troubleshooting a
resolved host requires tracing inheritance or auth into runtime behavior. Read
the migration reference first for upgrade requests; add another reference only
when the user also needs post-migration internals or troubleshooting.
