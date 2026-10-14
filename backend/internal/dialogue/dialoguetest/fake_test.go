package dialoguetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/dialogue"
)

func TestFakeGatewayStreamsDeltasAndAccumulates(t *testing.T) {
	g := &FakeGateway{Deltas: []string{"第一步 ", "先分析 ", "已知条件。"}}
	var seen []string
	full, err := g.Stream(context.Background(), dialogue.Request{
		Model:    "test-model",
		Messages: []dialogue.Message{{Role: "system", Content: "系统提示"}},
	}, func(text string) error {
		seen = append(seen, text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "第一步 |先分析 |已知条件。" {
		t.Fatalf("unexpected delta order: %q", seen)
	}
	if full != "第一步 先分析 已知条件。" {
		t.Fatalf("unexpected accumulated text: %q", full)
	}
	if g.Calls != 1 || g.DeltaCalls != 3 {
		t.Fatalf("call counters wrong: %d/%d", g.Calls, g.DeltaCalls)
	}
	if g.LastRequest.Model != "test-model" || len(g.LastRequest.Messages) != 1 {
		t.Fatalf("request not recorded: %+v", g.LastRequest)
	}
}

func TestFakeGatewayFullOverride(t *testing.T) {
	g := &FakeGateway{Deltas: []string{"partial"}, Full: "final guarded text"}
	full, err := g.Stream(context.Background(), dialogue.Request{}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if full != "final guarded text" {
		t.Fatalf("expected override, got %q", full)
	}
}

func TestFakeGatewayProviderError(t *testing.T) {
	g := &FakeGateway{Deltas: []string{"开头"}, Err: dialogue.ErrUnavailable}
	var seen int
	full, err := g.Stream(context.Background(), dialogue.Request{}, func(string) error {
		seen++
		return nil
	})
	if !errors.Is(err, dialogue.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if seen != 1 || full != "开头" {
		t.Fatalf("deltas before error lost: seen=%d full=%q", seen, full)
	}
}

func TestFakeGatewayOnDeltaAbort(t *testing.T) {
	g := &FakeGateway{Deltas: []string{"a", "b", "c"}}
	abort := errors.New("writer closed")
	_, err := g.Stream(context.Background(), dialogue.Request{}, func(text string) error {
		if text == "b" {
			return abort
		}
		return nil
	})
	if !errors.Is(err, abort) {
		t.Fatalf("expected abort error, got %v", err)
	}
	if g.DeltaCalls != 2 {
		t.Fatalf("expected stream to stop at second delta, got %d", g.DeltaCalls)
	}
}

func TestFakeGatewayCancellationDuringDelay(t *testing.T) {
	g := &FakeGateway{Deltas: []string{"a", "b", "c", "d"}, Delay: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := g.Stream(ctx, dialogue.Request{}, func(string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if g.DeltaCalls > 2 {
		t.Fatalf("expected cancellation between deltas, got %d emitted", g.DeltaCalls)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("cancellation did not interrupt the delay: %s", elapsed)
	}
}

func TestFakeGatewayImmediateCancellation(t *testing.T) {
	g := &FakeGateway{Deltas: []string{"a"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.Stream(ctx, dialogue.Request{}, func(string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if g.DeltaCalls != 0 {
		t.Fatalf("no delta should be emitted, got %d", g.DeltaCalls)
	}
}
