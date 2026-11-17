package aws

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucketRateLimiter(t *testing.T) {
	// 2 tokens capacity, refills 10 tokens per second
	tb := NewTokenBucket(2, 10)
	ctx := context.Background()

	// First two waits must return immediately (capacity is 2)
	start := time.Now()
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("second wait failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("expected immediate burst token consumption, took %v", elapsed)
	}

	// Third wait requires refill (~100ms for 1 token at 10/sec)
	start = time.Now()
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("third wait failed: %v", err)
	}
	elapsed = time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected delay for token refill, took only %v", elapsed)
	}
}

func TestTokenBucketContextCancellation(t *testing.T) {
	// 0 capacity, 0.001 refill/sec -> will block
	tb := NewTokenBucket(0, 0.001)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := tb.Wait(ctx)
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}
