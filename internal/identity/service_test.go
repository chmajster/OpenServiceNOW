package identity

import (
	"errors"
	"testing"
)

func TestPasswordCheckSemaphoreReleasesSlot(t *testing.T) {
	service := &Service{passwordChecks: make(chan struct{}, 1)}
	calls := 0

	if err := service.withPasswordCheck(func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("first withPasswordCheck() error = %v", err)
	}
	if err := service.withPasswordCheck(func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("second withPasswordCheck() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestPasswordCheckSemaphoreRejectsSaturation(t *testing.T) {
	service := &Service{passwordChecks: make(chan struct{}, 1)}
	service.passwordChecks <- struct{}{}
	defer func() { <-service.passwordChecks }()

	called := false
	err := service.withPasswordCheck(func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrAuthBusy) {
		t.Fatalf("error = %v want ErrAuthBusy", err)
	}
	if called {
		t.Fatal("password check executed despite saturated semaphore")
	}
}
