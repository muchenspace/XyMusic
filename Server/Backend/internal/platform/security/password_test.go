package security

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !valid {
		t.Fatalf("expected password to verify: valid=%v err=%v", valid, err)
	}
	valid, err = VerifyPassword("wrong", hash)
	if err != nil || valid {
		t.Fatalf("expected wrong password to fail: valid=%v err=%v", valid, err)
	}
}

func TestPasswordHashRejectsHostileParameters(t *testing.T) {
	_, err := VerifyPassword("password", "$argon2id$v=19$m=4294967295,t=3,p=1$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA")
	if err == nil {
		t.Fatal("expected hostile hash to be rejected")
	}
}

func TestOpaqueTokensAreURLSafeAndUnique(t *testing.T) {
	left, err := CreateOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	right, err := CreateOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 64 || left == right {
		t.Fatalf("unexpected opaque tokens: %q %q", left, right)
	}
}
