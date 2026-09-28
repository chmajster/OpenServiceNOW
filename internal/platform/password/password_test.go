package password

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	params := Params{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
	hash, err := HashWithParams("correct horse battery staple", params)
	if err != nil {
		t.Fatalf("HashWithParams() error = %v", err)
	}
	ok, err := Verify(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !ok {
		t.Fatal("expected password verification to succeed")
	}
	ok, err = Verify(hash, "wrong password")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if ok {
		t.Fatal("expected password verification to fail")
	}
}

func TestHashRejectsShortPassword(t *testing.T) {
	if _, err := HashWithParams("short", Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}); err == nil {
		t.Fatal("expected short password error")
	}
}

func TestHashRejectsOversizedPassword(t *testing.T) {
	plain := strings.Repeat("a", MaxPasswordBytes+1)
	if _, err := HashWithParams(plain, Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}); err == nil {
		t.Fatal("expected oversized password error")
	}
}

func TestVerifyRejectsOversizedCandidateBeforeArgon2(t *testing.T) {
	params := Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}
	hash, err := HashWithParams("correct horse battery staple", params)
	if err != nil {
		t.Fatalf("HashWithParams() error = %v", err)
	}
	ok, err := Verify(hash, strings.Repeat("x", MaxPasswordBytes+1))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if ok {
		t.Fatal("oversized password candidate must not verify")
	}
}

func TestVerifyRejectsExcessiveStoredArgonMemory(t *testing.T) {
	params := Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}
	hash, err := HashWithParams("correct horse battery staple", params)
	if err != nil {
		t.Fatalf("HashWithParams() error = %v", err)
	}
	malicious := strings.Replace(
		hash,
		"m=8192,t=1,p=1",
		"m=1048576,t=1,p=1",
		1,
	)
	if _, err := Verify(malicious, "correct horse battery staple"); err == nil {
		t.Fatal("expected excessive Argon2 memory parameter error")
	}
}

func TestHashRejectsOversizedArgonParameters(t *testing.T) {
	_, err := HashWithParams("correct horse battery staple", Params{
		Memory:      MaxArgonMemoryKiB + 1,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
	if err == nil {
		t.Fatal("expected Argon2 parameter validation error")
	}
}

func TestVerifyRejectsArgonParameterTrailingJunk(t *testing.T) {
	encoded := "$argon2id$v=19$m=8192,t=1,p=1junk$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA"
	if _, err := Verify(encoded, "correct horse battery staple"); err == nil {
		t.Fatal("expected malformed Argon2id parameter error")
	}
}

func TestVerifyRejectsUnexpectedArgonParameterOrder(t *testing.T) {
	encoded := "$argon2id$v=19$t=1,m=8192,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA"
	if _, err := Verify(encoded, "correct horse battery staple"); err == nil {
		t.Fatal("expected unexpected Argon2id parameter order error")
	}
}
