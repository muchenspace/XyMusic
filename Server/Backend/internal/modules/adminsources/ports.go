package adminsources

import (
	"context"
	"encoding/json"
	"time"
)

type Store interface {
	ListRootViews(context.Context, RootQuery) ([]RootView, int, error)
	FindRootView(context.Context, string) (RootView, error)
	FindRoot(context.Context, string) (Root, error)
	CreateRoot(context.Context, string, string, RootMutation) (RootView, error)
	UpdateRoot(context.Context, UpdateRootCommand) (RootView, error)
	DeleteRoot(context.Context, DeleteRootCommand) error
	ListFiles(context.Context, string, FileQuery) ([]SourceFile, int, error)
	ProcessingSummary(context.Context, string) (ProcessingSummary, error)
	ListRuns(context.Context, string, PageQuery) ([]ScanRun, int, error)
	FindRun(context.Context, string, string) (ScanRun, error)
	EnqueueScan(context.Context, EnqueueScanCommand) (ScanRun, error)
	CancelScan(context.Context, CancelScanCommand) error
}

type WorkerStore interface {
	InitializeScans(context.Context, time.Time) error
	EnsureDefaultRoot(context.Context, RootMutation) (Root, error)
	StartupRootIDs(context.Context) ([]string, error)
	EnqueueScan(context.Context, EnqueueScanCommand) (ScanRun, error)
	EnqueueScheduledScans(context.Context, time.Time) error
	ClaimNextScan(context.Context, string, time.Time, time.Duration) (*ClaimedScan, error)
	HeartbeatScan(context.Context, string, string, string, time.Time, time.Duration) (bool, error)
	ScanControl(context.Context, string, string, string) (cancelled bool, owned bool, err error)
	UpdateScanProgress(context.Context, string, string, string, ScanProgress, time.Time) (bool, error)
	CompleteScan(context.Context, ClaimedScan, string, string, ScanResult, time.Time) (bool, error)
	FinalizeScanFailure(context.Context, ClaimedScan, string, string, ScanStatus, *string, time.Time) (bool, error)
}

type Scanner interface {
	Scan(context.Context, ScanInput) (ScanResult, error)
}

type ScanInput struct {
	ScanRunID       string
	RootID          string
	Directory       string
	IncludePatterns []string
	ExcludePatterns []string
	IsCancelled     func(context.Context) (bool, error)
	OnProgress      func(context.Context, ScanProgress) error
}

type WorkerAvailability func(context.Context) (bool, error)

type IdempotencyInput struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
}

type IdempotencyResponse struct {
	Status int
	Body   json.RawMessage
}

type IdempotencyResult struct {
	Status   int
	Body     json.RawMessage
	Replayed bool
}

type Idempotency interface {
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}

type ScanExecutor interface {
	Initialize(context.Context) error
	RunNextScan(context.Context) (bool, error)
}
