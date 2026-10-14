package handlers

import "testing"

func TestPasswordResetToken(t *testing.T) {
	token, hash, err := newPasswordResetToken()
	if err != nil {
		t.Fatalf("newPasswordResetToken() error = %v", err)
	}
	if !validResetToken(token) {
		t.Fatal("generated reset token should be valid")
	}
	if hash == token || hash != hashResetToken(token) {
		t.Fatal("stored reset token should be a one-way hash")
	}
	if validResetToken(token[:len(token)-1]) {
		t.Fatal("truncated reset token should be invalid")
	}
}

func TestValidateNewPassword(t *testing.T) {
	for _, password := range []string{"", "short", string(make([]byte, 73))} {
		if err := validateNewPassword(password); err == nil {
			t.Errorf("validateNewPassword(%q) should reject password", password)
		}
	}
	if err := validateNewPassword("correct-horse"); err != nil {
		t.Errorf("validateNewPassword() rejected a valid password: %v", err)
	}
}
