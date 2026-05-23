package app

import "testing"

func TestVerifyBunArgon2IDHash(t *testing.T) {
	hash := "$argon2id$v=19$m=65536,t=2,p=1$9Gh1wrVfGclAkVQCJJXAh1BDCdF9R+CGKEDhBb3cGuQ$Oo+nmvbwbz/X960SLZrw6nLFeroEzz3DU+Uz3YLNk4k"
	if !verifyPassword("secret123", hash) {
		t.Fatal("expected Go verifier to accept Bun argon2id hash")
	}
	if verifyPassword("wrong", hash) {
		t.Fatal("expected verifier to reject wrong password")
	}
}
