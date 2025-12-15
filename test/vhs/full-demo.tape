# Demo: nssh full workflow
# Shows initialization, context/host management, and SSH connection with password injection

Set Width 1200
Set Height 900
Set Margin 20
Set MarginFill "#674EFF"
Set WindowBar Colorful
Set WindowBarSize 40
Set BorderRadius 8
Set Padding 10
Set Theme "iTerm2 Pastel Dark Background"
Set Framerate 60
Set TypingSpeed 60ms
Set Shell "bash"
Set FontSize 20
Set FontFamily "JetBrains Mono"
# Set LoopOffset 5

# ============================================================================
# Section 1: Self Init
# ============================================================================

Sleep 500ms

# Start the interactive init
Type "nssh self init"
Sleep 500ms
Enter

# Wait for the user to review
Sleep 2s

# Confirm software security mode
Enter
Sleep 500ms

# Enter passphrase
Type "thisisatestpass"
Enter
Sleep 500ms

# Confirm passphrase
Type "thisisatestpass"
Enter
Sleep 500ms

# Skip dependency installs
Right
Sleep 500ms
Enter
Sleep 500ms
Right
Sleep 500ms
Enter
Sleep 500ms
Right
Sleep 500ms
Enter
Sleep 500ms

# Unlock the vault for subsequent commands
Sleep 200ms
Type "nssh unlock"
Enter

# Enter passphrase
Sleep 200ms
Type "thisisatestpass"
Enter

# Pause for user to review
Sleep 10s
Ctrl+L

# ============================================================================
# Section 2: Context Add & List
# ============================================================================

# Start the interactive ctx add
Type "nssh ctx add"
Sleep 300ms
Enter

# Wait for the form to appear
Sleep 1s

# Fill in context name
Type "test"
Sleep 500ms
Enter
Sleep 300ms

# Confirm SSH include filename (default)
Sleep 2s
Enter
Sleep 300ms

# Skip domain
Sleep 2s
Enter
Sleep 300ms

# Skip credential
Sleep 2s
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter

# Show the created ctx
Type "nssh ctx list"
Sleep 500ms
Enter

# Pause for user to review
Sleep 10s
Ctrl+L

# ============================================================================
# Section 4: Host Add
# ============================================================================

# Start the interactive host add with hostname argument
Type "nssh host add ssh-pwd.demo.local"
Sleep 300ms
Enter

# Wait for the form to appear
Sleep 500ms

# Confirm ctx (default)
Sleep 2s
Enter

# Confirm host (default)
Sleep 2s
Enter

# Confirm hostname (default)
Sleep 2s
Enter

# Confirm port (default 22)
Sleep 2s
Enter
Sleep 300ms

# Fill in User
Type "testuser"
Sleep 2s
Enter
Sleep 300ms

# Select password auth (Key is default, so arrow down to Password)
Sleep 500ms
Down
Sleep 2s
Enter
Sleep 300ms

# Fill in Password
Type "testpass"
Sleep 500ms
Enter

# Wait for processing
Sleep 2s

# Confirm host add
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter

# Show the created host
Type "nssh host list"
Sleep 500ms
Enter

# Pause for user to review
Sleep 10s
Ctrl+L

# ============================================================================
# Section 6: Connect
# ============================================================================

# Connect to the test host (password will be auto-injected)
Type "nssh ssh-pwd"
Sleep 300ms
Enter

# Wait for connection and password injection
Sleep 2s

# Run some commands to show we're connected
Type "whoami"
Enter
Sleep 2s

Type "hostname"
Enter
Sleep 2s

Type@20ms "echo 'Connected via nssh with auto password injection!'"
Enter
Sleep 2s

# Exit the session
Type "exit"
Enter

# Let viewer see the disconnect
Sleep 10s
