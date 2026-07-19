package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)

const failedClaimCleanupTimeout = 5 * time.Second

type Service struct {
	pool   *pgxpool.Pool
	cipher *security.PayloadCipher
	now    func() time.Time
}

type Input struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
	TTL     time.Duration
}

type HTTPResult[T any] struct {
	Status int
	Body   T
}

type Result[T any] struct {
	Status   int
	Body     T
	Replayed bool
}

type record struct {
	ID                uuid.UUID
	RequestHash       string
	ResponseStatus    *int
	EncryptedResponse *string
}

func New(pool *pgxpool.Pool, cipher *security.PayloadCipher) *Service {
	return &Service{pool: pool, cipher: cipher, now: time.Now}
}

func Execute[T any](ctx context.Context, service *Service, input Input, operation func() (HTTPResult[T], error)) (Result[T], error) {
	var zero Result[T]
	if !keyPattern.MatchString(input.Key) {
		return zero, apperror.Validation("Idempotency-Key 无效")
	}
	if input.ActorID == "" || input.Scope == "" {
		return zero, errors.New("idempotency actor and scope are required")
	}
	if input.TTL == 0 {
		input.TTL = 24 * time.Hour
	}
	if input.TTL < time.Second || input.TTL%time.Second != 0 {
		return zero, errors.New("idempotency TTL must contain a positive whole number of seconds")
	}
	canonical, err := CanonicalJSON(input.Payload)
	if err != nil {
		return zero, fmt.Errorf("canonicalize idempotency payload: %w", err)
	}
	digest := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(digest[:])
	claimID := uuid.New()
	ownsClaim := false
	for attempt := 0; attempt < 3; attempt++ {
		claimed, err := service.claim(ctx, claimID, input, requestHash)
		if err != nil {
			return zero, err
		}
		if claimed {
			ownsClaim = true
			break
		}
		existing, err := service.find(ctx, input.ActorID, input.Scope, input.Key, service.now().UTC())
		if err != nil {
			return zero, err
		}
		if existing != nil {
			return replay[T](service, existing, requestHash)
		}
	}
	if !ownsClaim {
		return zero, apperror.Conflict(apperror.CodeResourceConflict, "相同请求正在被并发处理", nil)
	}

	result, err := operation()
	if err != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), failedClaimCleanupTimeout)
		_, cleanupErr := service.pool.Exec(
			cleanupContext,
			"delete from idempotency_records where id = $1 and response_status is null and encrypted_response is null",
			claimID,
		)
		cancel()
		if cleanupErr != nil {
			return zero, errors.Join(err, fmt.Errorf("release failed idempotency claim: %w", cleanupErr))
		}
		return zero, err
	}
	encrypted, err := service.cipher.Encrypt(result.Body)
	if err != nil {
		return zero, err
	}
	completedAt := service.now().UTC()
	command, err := service.pool.Exec(ctx, `
		update idempotency_records
		set response_status = $1, encrypted_response = $2, expires_at = $3
		where id = $4 and request_hash = $5
		  and response_status is null and encrypted_response is null`,
		result.Status, encrypted, completedAt.Add(input.TTL), claimID, requestHash)
	if err != nil {
		return zero, fmt.Errorf("complete idempotency claim: %w", err)
	}
	if command.RowsAffected() != 1 {
		return zero, apperror.Conflict(apperror.CodeResourceConflict, "幂等请求在操作完成前已过期", nil)
	}
	completed, err := service.find(ctx, input.ActorID, input.Scope, input.Key, completedAt)
	if err != nil {
		return zero, err
	}
	if completed == nil || completed.ID != claimID || completed.RequestHash != requestHash {
		return zero, apperror.Conflict(apperror.CodeResourceConflict, "幂等请求在操作完成前已过期", nil)
	}
	return Result[T]{Status: result.Status, Body: result.Body, Replayed: false}, nil
}

func (s *Service) claim(ctx context.Context, claimID uuid.UUID, input Input, requestHash string) (bool, error) {
	now := s.now().UTC()
	var returned uuid.UUID
	err := s.pool.QueryRow(ctx, `
		insert into idempotency_records
			(id, actor_id, scope, key, request_hash, created_at, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (actor_id, scope, key) do update set
			id = excluded.id,
			request_hash = excluded.request_hash,
			response_status = null,
			encrypted_response = null,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at
		where idempotency_records.expires_at <= $6
		returning id`, claimID, input.ActorID, input.Scope, input.Key, requestHash, now, now.Add(input.TTL)).Scan(&returned)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	return returned == claimID, nil
}

func (s *Service) find(ctx context.Context, actorID, scope, key string, now time.Time) (*record, error) {
	var value record
	err := s.pool.QueryRow(ctx, `
		select id, request_hash, response_status, encrypted_response
		from idempotency_records
		where actor_id = $1 and scope = $2 and key = $3 and expires_at > $4`,
		actorID, scope, key, now).Scan(&value.ID, &value.RequestHash, &value.ResponseStatus, &value.EncryptedResponse)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find idempotency key: %w", err)
	}
	return &value, nil
}

func replay[T any](service *Service, existing *record, requestHash string) (Result[T], error) {
	var zero Result[T]
	if existing.RequestHash != requestHash {
		return zero, apperror.Conflict(apperror.CodeIdempotencyKeyReused, "幂等键已用于不同的请求内容", nil)
	}
	if existing.ResponseStatus == nil || existing.EncryptedResponse == nil {
		return zero, apperror.Conflict(apperror.CodeResourceConflict, "相同请求仍在处理中", nil)
	}
	var body T
	if err := service.cipher.Decrypt(*existing.EncryptedResponse, &body); err != nil {
		return zero, fmt.Errorf("decrypt idempotent response: %w", err)
	}
	return Result[T]{Status: *existing.ResponseStatus, Body: body, Replayed: true}, nil
}

// CanonicalJSON matches the legacy stableJson function by sorting every object
// key recursively while preserving array order.
func CanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := writeCanonical(&output, normalized); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonical(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		output.WriteString(strconv.FormatBool(typed))
	case string:
		encoded, _ := json.Marshal(typed)
		output.Write(encoded)
	case json.Number:
		output.WriteString(typed.String())
	case []any:
		output.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonical(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			output.Write(encoded)
			output.WriteByte(':')
			if err := writeCanonical(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}
