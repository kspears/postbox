package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

var limits = Limits{PerAccountIP: 5, PerIP: 20, Window: 15 * time.Minute, Block: 15 * time.Minute}

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func blocked(t *testing.T, s *Store, now time.Time, account, ip string) bool {
	t.Helper()
	b, err := s.Blocked(context.Background(), now, account, ip)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFiveFailuresBlockTheAddressFromThatIP(t *testing.T) {
	s, ctx := open(t), context.Background()
	now := time.Unix(1_800_000_000, 0)
	for i := range 4 {
		s.RecordFailure(ctx, now.Add(time.Duration(i)*time.Minute), "a", "ip1", limits)
	}
	if blocked(t, s, now.Add(4*time.Minute), "a", "ip1") {
		t.Fatal("blocked after four failures")
	}
	fifth := now.Add(10 * time.Minute)
	s.RecordFailure(ctx, fifth, "a", "ip1", limits)
	if !blocked(t, s, fifth, "a", "ip1") {
		t.Fatal("not blocked after five failures in 15 minutes")
	}
	if blocked(t, s, fifth, "a", "ip2") || blocked(t, s, fifth, "b", "ip1") {
		t.Error("the block reached another IP or another address")
	}
	if !blocked(t, s, fifth.Add(15*time.Minute-time.Second), "a", "ip1") {
		t.Error("block ended early")
	}
	if blocked(t, s, fifth.Add(15*time.Minute), "a", "ip1") {
		t.Error("block outlasted 15 minutes")
	}
}

func TestFailuresOutsideTheWindowDoNotCount(t *testing.T) {
	s, ctx := open(t), context.Background()
	now := time.Unix(1_800_000_000, 0)
	for i := range 5 {
		s.RecordFailure(ctx, now.Add(time.Duration(i)*4*time.Minute), "a", "ip1", limits)
	}
	if blocked(t, s, now.Add(16*time.Minute), "a", "ip1") {
		t.Error("five failures over 16 minutes caused a block")
	}
}

func TestTwentyFailuresAcrossAddressesBlockTheIP(t *testing.T) {
	s, ctx := open(t), context.Background()
	now := time.Unix(1_800_000_000, 0)
	for i := range 20 {
		account := fmt.Sprintf("user%d", i)
		s.RecordFailure(ctx, now, account, "ip1", limits)
	}
	if !blocked(t, s, now, "someone-new", "ip1") {
		t.Error("twenty failures from one IP did not block it")
	}
	if blocked(t, s, now, "someone-new", "ip2") {
		t.Error("another IP was blocked")
	}
}

func TestClearFailures(t *testing.T) {
	s, ctx := open(t), context.Background()
	now := time.Unix(1_800_000_000, 0)
	for range 4 {
		s.RecordFailure(ctx, now, "a", "ip1", limits)
	}
	s.ClearFailures(ctx, "a", "ip1")
	s.RecordFailure(ctx, now, "a", "ip1", limits)
	if blocked(t, s, now, "a", "ip1") {
		t.Error("failures before a successful sign-in still counted")
	}
}
