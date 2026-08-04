package resourcebudget

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBudgetHonorsWeightedCapacity(t *testing.T) {
	budget, err := New(3)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Acquire(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		if err := budget.Acquire(context.Background(), 2); err == nil {
			close(acquired)
		}
	}()
	select {
	case <-acquired:
		t.Fatal("weighted budget allowed more than its capacity")
	case <-time.After(20 * time.Millisecond):
	}
	budget.Release(2)
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("weighted budget did not release capacity")
	}
	budget.Release(2)
}

func TestBudgetCancellationReturnsPartialTokens(t *testing.T) {
	budget, err := New(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Acquire(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := budget.Acquire(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire error = %v, want context cancellation", err)
	}
	budget.Release(2)
	if err := budget.Acquire(context.Background(), 2); err != nil {
		t.Fatalf("budget leaked tokens after cancellation: %v", err)
	}
	budget.Release(2)
}

func TestBudgetRejectsOversizedRequest(t *testing.T) {
	budget, err := New(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Acquire(context.Background(), 3); !errors.Is(err, ErrRequestExceedsCapacity) {
		t.Fatalf("acquire error = %v, want capacity error", err)
	}
}

func TestBudgetIsSharedAcrossIndependentWorkers(t *testing.T) {
	budget, err := New(3)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	acquired := make(chan error, 2)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var group sync.WaitGroup

	worker := func(units int) {
		defer group.Done()
		<-start
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := budget.Acquire(ctx, units); err != nil {
			acquired <- err
			return
		}
		current := active.Add(int32(units))
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		acquired <- nil
		<-release
		active.Add(-int32(units))
		budget.Release(units)
	}

	group.Add(2)
	go worker(2)
	go worker(1)
	close(start)
	var firstErr error
	for index := 0; index < 2; index++ {
		select {
		case err := <-acquired:
			if err != nil && firstErr == nil {
				firstErr = err
			}
		case <-time.After(2 * time.Second):
			firstErr = errors.New("timed out waiting for both workers to acquire shared budget")
			index = 2
		}
	}
	close(release)
	group.Wait()
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	if actual := maximum.Load(); actual > int32(budget.Capacity()) {
		t.Fatalf("shared budget allowed %d active units with capacity %d", actual, budget.Capacity())
	}
	if actual := maximum.Load(); actual != 3 {
		t.Fatalf("shared budget did not account for both workers, maximum active units = %d", actual)
	}
}
