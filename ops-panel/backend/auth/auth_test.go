package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCredentialStore struct {
	subject     string
	password    string
	exists      bool
	existsCalls int
	err         error
}

func (s *fakeCredentialStore) Authenticate(_ context.Context, username, password string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	return s.subject, username == s.subject && password == s.password, nil
}

func (s *fakeCredentialStore) SubjectExists(_ context.Context, subject string) (bool, error) {
	s.existsCalls++
	if s.err != nil {
		return false, s.err
	}
	return s.exists && subject == s.subject, nil
}

func TestCredentialStoreTokenChecksSubjectOnEveryVerify(t *testing.T) {
	store := &fakeCredentialStore{subject: "admin@example.com", password: "secret", exists: true}
	svc, err := NewWithCredentialStore(store, "test-token-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewWithCredentialStore: %v", err)
	}

	token, _, err := svc.LoginContext(context.Background(), store.subject, store.password)
	if err != nil {
		t.Fatalf("LoginContext: %v", err)
	}
	for i := 0; i < 2; i++ {
		subject, err := svc.VerifyContext(context.Background(), token)
		if err != nil {
			t.Fatalf("VerifyContext %d: %v", i, err)
		}
		if subject != store.subject {
			t.Fatalf("subject = %q", subject)
		}
	}
	if store.existsCalls != 2 {
		t.Fatalf("SubjectExists calls = %d, want 2", store.existsCalls)
	}

	store.exists = false
	if _, err := svc.VerifyContext(context.Background(), token); err == nil || err.Error() != "unknown subject" {
		t.Fatalf("VerifyContext after delete error = %v", err)
	}
}

func TestCredentialStoreErrorsPropagate(t *testing.T) {
	wantErr := errors.New("store unavailable")
	store := &fakeCredentialStore{err: wantErr}
	svc, err := NewWithCredentialStore(store, "test-token-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewWithCredentialStore: %v", err)
	}
	if _, _, err := svc.Login("admin", "secret"); !errors.Is(err, wantErr) {
		t.Fatalf("Login error = %v, want %v", err, wantErr)
	}
}

func TestStaticCredentialsRemainSupported(t *testing.T) {
	svc, err := New("admin", "secret", "test-token-secret", time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	token, _, err := svc.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if subject, err := svc.Verify(token); err != nil || subject != "admin" {
		t.Fatalf("Verify = (%q, %v)", subject, err)
	}
}
