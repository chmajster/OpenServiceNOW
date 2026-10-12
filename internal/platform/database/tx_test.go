package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type fakeTransaction struct {
	commits     int
	rollbacks   int
	commitErr   error
	rollbackErr error
}

func (f *fakeTransaction) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *fakeTransaction) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

func (f *fakeTransaction) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

func (f *fakeTransaction) Commit() error {
	f.commits++
	return f.commitErr
}

func (f *fakeTransaction) Rollback() error {
	f.rollbacks++
	return f.rollbackErr
}

func TestRunTransactionCommitsOnSuccess(t *testing.T) {
	tx := &fakeTransaction{}
	if err := runTransaction(tx, func(Executor) error { return nil }); err != nil {
		t.Fatalf("runTransaction() error = %v", err)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d", tx.commits, tx.rollbacks)
	}
}

func TestRunTransactionRollsBackOnError(t *testing.T) {
	expected := errors.New("operation failed")
	tx := &fakeTransaction{}
	err := runTransaction(tx, func(Executor) error { return expected })
	if !errors.Is(err, expected) {
		t.Fatalf("runTransaction() error = %v", err)
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d", tx.commits, tx.rollbacks)
	}
}

func TestRunTransactionRollsBackBeforeRepanic(t *testing.T) {
	tx := &fakeTransaction{}
	defer func() {
		recovered := recover()
		if recovered != "boom" {
			t.Fatalf("recovered = %#v", recovered)
		}
		if tx.commits != 0 || tx.rollbacks != 1 {
			t.Fatalf("commits=%d rollbacks=%d", tx.commits, tx.rollbacks)
		}
	}()
	_ = runTransaction(tx, func(Executor) error {
		panic("boom")
	})
}

func TestRunTransactionReportsCommitFailure(t *testing.T) {
	expected := errors.New("commit failed")
	tx := &fakeTransaction{commitErr: expected}
	err := runTransaction(tx, func(Executor) error { return nil })
	if !errors.Is(err, expected) {
		t.Fatalf("runTransaction() error = %v", err)
	}
	if !errors.Is(err, ErrCommitUncertain) {
		t.Fatal("commit failure was presented as a confirmed rollback")
	}
	if tx.commits != 1 {
		t.Fatalf("commits=%d", tx.commits)
	}
}

func TestRunTransactionReportsRollbackFailure(t *testing.T) {
	operationErr := errors.New("operation failed")
	rollbackErr := errors.New("rollback failed")
	tx := &fakeTransaction{rollbackErr: rollbackErr}

	err := runTransaction(tx, func(Executor) error { return operationErr })
	if !errors.Is(err, operationErr) {
		t.Fatalf("runTransaction() error = %v", err)
	}
	if tx.rollbacks != 1 {
		t.Fatalf("rollbacks=%d", tx.rollbacks)
	}
	if !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("error does not include rollback failure: %v", err)
	}
}
