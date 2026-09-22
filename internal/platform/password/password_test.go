package password

import "testing"

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
