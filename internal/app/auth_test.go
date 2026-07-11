package app

import "testing"

func TestPasswordHash(t *testing.T) {
	hash, err := hashPassword("a-strong-password")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "a-strong-password") {
		t.Fatal("valid password rejected")
	}
	if verifyPassword(hash, "wrong-password") {
		t.Fatal("invalid password accepted")
	}
}

func TestShortPasswordRejected(t *testing.T) {
	if _, err := hashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}
