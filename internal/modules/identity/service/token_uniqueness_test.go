package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/test"
)

type failingRefreshRepo struct{ fakeUserRepository }

func (*failingRefreshRepo) CreateRefreshToken(context.Context, *domain.RefreshToken) error {
	return errors.New("refresh storage unavailable")
}

func TestRefreshIssuanceFailsWhenStorageFails(t *testing.T) {
	svc := NewTokenService(test.TestConfig(), &failingRefreshRepo{})
	user := test.CreateTestUserWithDefaults()
	if token, err := svc.GenerateRefreshToken(context.Background(), user); err == nil || token != "" {
		t.Fatal("must not return an unusable refresh token after a storage failure")
	}
	if pair, err := svc.GenerateTokenPairWithTx(context.Background(), nil, user); err == nil || pair != nil {
		t.Fatal("must not return a token pair after a storage failure without a transaction")
	}
}

// Both public issuance paths must distinguish sessions even when all timestamp
// claims land in one second. Checking jti also makes this independent of clocks.
func TestRefreshSessionsHaveUniqueIDs(t *testing.T) {
	svc := NewTokenService(test.TestConfig())
	user := test.CreateTestUserWithDefaults()
	const sessions = 64
	tokens := make(chan string, sessions)
	var wg sync.WaitGroup
	for i := range sessions {
		wg.Go(func() {
			var token string
			var err error
			if i%2 == 0 {
				token, err = svc.GenerateRefreshToken(context.Background(), user)
			} else {
				pair, pairErr := svc.GenerateTokenPairWithTx(context.Background(), nil, user)
				err = pairErr
				if pair != nil {
					token = pair.RefreshToken
				}
			}
			if err != nil {
				t.Error(err)
				return
			}
			tokens <- token
		})
	}
	wg.Wait()
	close(tokens)
	seen := make(map[string]bool)
	for token := range tokens {
		claims := &jwt.RegisteredClaims{}
		_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
			return svc.refreshSigningKey(), nil
		}, jwt.WithAudience(audienceRefresh), jwt.WithValidMethods([]string{"HS256"}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := uuid.Parse(claims.ID); err != nil {
			t.Fatalf("session must have a UUID jti: %v", err)
		}
		if seen[claims.ID] {
			t.Fatal("two sessions share one refresh token identifier")
		}
		seen[claims.ID] = true
		if claims.Subject != user.ID.String() {
			t.Fatal("wrong session owner")
		}
	}
	if len(seen) != sessions {
		t.Fatalf("got %d sessions, want %d", len(seen), sessions)
	}
}
