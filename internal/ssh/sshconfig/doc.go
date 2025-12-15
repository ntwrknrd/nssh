// Package sshconfig provides SSH config file parsing and manipulation.
//
// This package reads and writes OpenSSH config files (~/.ssh/config and
// included files), supporting host lookup, fuzzy matching, and programmatic
// modification.
//
// # Parsing
//
// The [Parser] reads SSH config files following OpenSSH conventions:
//   - Host blocks with pattern matching
//   - Include directives with glob expansion
//   - Standard directives (HostName, User, Port, IdentityFile, etc.)
//
// # Host Lookup
//
// Use [Parser.FindHost] for exact matches or [Parser.MatchHost] for fuzzy
// matching with suggestions:
//
//	parser := sshconfig.NewParser()
//	host, err := parser.FindHost("myserver")
//	if err != nil {
//	    // Handle not found or parse error
//	}
//
// # Modification
//
// The package supports adding, updating, and removing host entries:
//
//	parser.AddHost(entry, "servers.conf")  // Add to include file
//	parser.RemoveHost("oldserver")          // Remove by name
//	parser.WriteFile(config)                // Save changes
//
// # Include Files
//
// nssh organizes hosts into include files (e.g., work.conf, home.conf) that
// map to credential contexts. The parser tracks which file each host comes
// from via [HostEntry.SourceFile].
//
// # Compatibility Fixes
//
// Use [ApplyCompatFixes] to add legacy algorithm support for older SSH servers.
// This modifies host entries to include KexAlgorithms, Ciphers, MACs, or
// HostKeyAlgorithms directives as needed.
package sshconfig
