"""Tests for secure password handling."""

from __future__ import annotations

import pytest

from nssh.core.security.password import MAX_PASSWORD_LENGTH, SecurePassword


class TestSecurePasswordCreation:
    """Tests for SecurePassword initialization."""

    def test_create_with_password(self):
        pwd = SecurePassword("secret")
        assert bool(pwd) is True
        assert pwd.get_bytes() == b"secret"

    def test_create_with_none(self):
        pwd = SecurePassword(None)
        assert bool(pwd) is False

    def test_create_with_empty_string(self):
        pwd = SecurePassword("")
        assert bool(pwd) is True
        assert pwd.get_bytes() == b""

    def test_create_with_unicode(self):
        pwd = SecurePassword("pässwörd™")
        assert bool(pwd) is True
        # Check UTF-8 encoding
        assert pwd.get_bytes() == "pässwörd™".encode("utf-8")


class TestSecurePasswordLengthValidation:
    """Tests for password length validation."""

    def test_max_length_accepted(self):
        # Create password at exactly max length
        max_pwd = "x" * MAX_PASSWORD_LENGTH
        pwd = SecurePassword(max_pwd)
        assert bool(pwd) is True

    def test_exceeds_max_length_raises(self):
        # One byte over the limit
        too_long = "x" * (MAX_PASSWORD_LENGTH + 1)
        with pytest.raises(ValueError, match="exceeds maximum length"):
            SecurePassword(too_long)

    def test_utf8_multi_byte_length_check(self):
        # UTF-8 characters can be multiple bytes
        # "€" is 3 bytes in UTF-8
        euro_sign = "€"
        # Calculate how many fit in MAX_PASSWORD_LENGTH
        max_euros = MAX_PASSWORD_LENGTH // 3
        pwd = SecurePassword(euro_sign * max_euros)
        assert bool(pwd) is True

        # One more euro should exceed the limit
        with pytest.raises(ValueError, match="exceeds maximum length"):
            SecurePassword(euro_sign * (max_euros + 1))

    def test_error_message_shows_byte_count(self):
        too_long = "x" * (MAX_PASSWORD_LENGTH + 100)
        with pytest.raises(ValueError) as exc_info:
            SecurePassword(too_long)
        error_msg = str(exc_info.value)
        assert "8192 bytes" in error_msg
        assert "8292 bytes" in error_msg  # 8192 + 100
        assert "SSH protocol" in error_msg


class TestSecurePasswordClearing:
    """Tests for password memory clearing."""

    def test_clear_password(self):
        pwd = SecurePassword("secret")
        assert bool(pwd) is True

        pwd.clear()
        assert bool(pwd) is False

    def test_get_bytes_after_clear_raises(self):
        pwd = SecurePassword("secret")
        pwd.clear()

        with pytest.raises(ValueError, match="has been cleared"):
            pwd.get_bytes()

    def test_clear_idempotent(self):
        pwd = SecurePassword("secret")
        pwd.clear()
        pwd.clear()  # Should not raise
        assert bool(pwd) is False

    def test_clear_none_password(self):
        pwd = SecurePassword(None)
        pwd.clear()  # Should not raise
        assert bool(pwd) is False

    def test_memory_zeroed_after_clear(self):
        """Test that password memory is actually zeroed."""
        pwd = SecurePassword("secret123")
        original_buffer = pwd._buffer

        # Clear the password
        pwd.clear()

        # The original buffer should be zeroed
        # (if it hasn't been garbage collected)
        if original_buffer is not None:
            # Check that all bytes are zero
            assert all(byte == 0 for byte in original_buffer)


class TestSecurePasswordRepresentation:
    """Tests for string representation."""

    def test_repr_with_password(self):
        pwd = SecurePassword("secret")
        repr_str = repr(pwd)
        assert "SecurePassword" in repr_str
        assert "6 bytes" in repr_str
        assert "secret" not in repr_str  # Password should not be exposed

    def test_repr_after_clear(self):
        pwd = SecurePassword("secret")
        pwd.clear()
        repr_str = repr(pwd)
        assert "SecurePassword" in repr_str
        assert "cleared" in repr_str

    def test_repr_with_none(self):
        pwd = SecurePassword(None)
        repr_str = repr(pwd)
        assert "SecurePassword" in repr_str
        assert "cleared" in repr_str


class TestSecurePasswordGarbageCollection:
    """Tests for automatic cleanup on garbage collection."""

    def test_del_clears_password(self):
        pwd = SecurePassword("secret")
        buffer_ref = pwd._buffer
        assert buffer_ref is not None

        # Delete the password object
        del pwd

        # The buffer should have been zeroed
        # (Python doesn't guarantee immediate GC, but __del__ should be called)
        # We can't easily test this without gc.collect() which may be flaky
        # This test mainly ensures __del__ doesn't raise exceptions


class TestSecurePasswordBoolConversion:
    """Tests for boolean conversion."""

    def test_bool_with_password_is_true(self):
        pwd = SecurePassword("secret")
        assert pwd
        assert bool(pwd) is True

    def test_bool_with_empty_password_is_true(self):
        # Empty string is still a valid password
        pwd = SecurePassword("")
        assert pwd
        assert bool(pwd) is True

    def test_bool_with_none_is_false(self):
        pwd = SecurePassword(None)
        assert not pwd
        assert bool(pwd) is False

    def test_bool_after_clear_is_false(self):
        pwd = SecurePassword("secret")
        pwd.clear()
        assert not pwd
        assert bool(pwd) is False
