"""Secure password handling with memory zeroing capabilities."""

from __future__ import annotations

from typing import Optional

# SSH protocol practical limit (RFC 4252, OpenSSH implementation)
MAX_PASSWORD_LENGTH = 8192


class SecurePassword:
    """Mutable password buffer that can be zeroed from memory.

    This class stores passwords in a mutable bytearray and provides a mechanism
    to zero the memory using ctypes.memset after use. While Python's memory
    management makes perfect security impossible, this approach reduces the
    window of vulnerability compared to immutable strings.

    Note: This implementation is specific to CPython and may not work reliably
    on other Python implementations (PyPy, Jython, etc.).

    Example:
        >>> pwd = SecurePassword("secret")
        >>> pwd.get_bytes()
        b'secret'
        >>> pwd.clear()
        >>> bool(pwd)
        False
    """

    def __init__(self, password: str | None) -> None:
        """Initialize secure password buffer.

        Args:
            password: The password string to store securely, or None for no password

        Raises:
            ValueError: If password exceeds MAX_PASSWORD_LENGTH bytes after UTF-8 encoding
        """
        # Initialize attributes first to avoid AttributeError in __del__
        self._buffer: Optional[bytearray] = None
        self._length = 0

        if password is None:
            return

        # Validate length after encoding to check actual byte size
        encoded = password.encode("utf-8")
        if len(encoded) > MAX_PASSWORD_LENGTH:
            raise ValueError(
                f"Password exceeds maximum length of {MAX_PASSWORD_LENGTH} bytes "
                f"(got {len(encoded)} bytes after UTF-8 encoding). "
                f"This limit is based on SSH protocol constraints."
            )

        # Store as mutable bytearray for later zeroing
        self._buffer = bytearray(encoded)
        self._length = len(self._buffer)

    def get_bytes(self) -> bytes:
        """Return password as bytes for transmission.

        Returns:
            The password as bytes

        Raises:
            ValueError: If password has been cleared
        """
        if self._buffer is None:
            raise ValueError("Password has been cleared")
        return bytes(self._buffer)

    def clear(self) -> None:
        """Securely zero password from memory using ctypes.memset.

        This method attempts to overwrite the password data in memory with zeros
        before releasing the reference. While not foolproof due to Python's memory
        management, this reduces the attack surface.

        This operation is idempotent - calling it multiple times is safe.
        """
        if self._buffer is None:
            return

        # Zero the memory using ctypes (lazy import for performance)
        if self._length > 0:
            try:
                import ctypes

                # Get pointer to bytearray's internal buffer and zero it
                buf_ptr = (ctypes.c_char * self._length).from_buffer(self._buffer)
                ctypes.memset(buf_ptr, 0, self._length)
            except (ValueError, TypeError):
                # Buffer might already be cleared or GC'd
                pass

        # Clear reference
        self._buffer = None
        self._length = 0

    def __del__(self) -> None:
        """Ensure cleanup on garbage collection."""
        self.clear()

    def __bool__(self) -> bool:
        """Check if password exists.

        Returns:
            True if password is set, False if cleared or None
        """
        return self._buffer is not None

    def __repr__(self) -> str:
        """Return safe representation without exposing password."""
        if self._buffer is not None:
            return f"SecurePassword(<{self._length} bytes>)"
        return "SecurePassword(<cleared>)"
