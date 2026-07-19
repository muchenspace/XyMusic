package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

var passwordWork = make(chan struct{}, max(1, min(4, runtime.GOMAXPROCS(0))))

const (
	argonMemory      uint32 = 64 * 1024
	argonIterations  uint32 = 3
	argonParallelism uint8  = 1
	argonSaltLength         = 16
	argonKeyLength   uint32 = 32
	opaqueTokenBytes        = 48
)

func HashPassword(password string) (string, error) {
	passwordWork <- struct{}{}
	defer func() { <-passwordWork }()
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword accepts the standard PHC argon2id representation emitted by
// Bun.password so existing credentials remain valid after the Go cutover.
func VerifyPassword(password, encoded string) (bool, error) {
	passwordWork <- struct{}{}
	defer func() { <-passwordWork }()
	parameters, salt, expected, err := parseArgon2ID(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(
		[]byte(password), salt, parameters.iterations, parameters.memory,
		parameters.parallelism, uint32(len(expected)),
	)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func CreateOpaqueToken() (string, error) {
	bytes := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate opaque token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func HashSecret(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest)
}

type argonParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2ID(encoded string) (argonParameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return argonParameters{}, nil, nil, errors.New("password hash has an unsupported format")
	}
	parameters := argonParameters{}
	for _, parameter := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(parameter, "=")
		if !ok {
			return argonParameters{}, nil, nil, errors.New("password hash has invalid parameters")
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argonParameters{}, nil, nil, errors.New("password hash has invalid parameters")
		}
		switch key {
		case "m":
			parameters.memory = uint32(parsed)
		case "t":
			parameters.iterations = uint32(parsed)
		case "p":
			if parsed > 255 {
				return argonParameters{}, nil, nil, errors.New("password hash has invalid parallelism")
			}
			parameters.parallelism = uint8(parsed)
		default:
			return argonParameters{}, nil, nil, errors.New("password hash has unknown parameters")
		}
	}
	if parameters.memory < 8 || parameters.iterations < 1 || parameters.parallelism < 1 {
		return argonParameters{}, nil, nil, errors.New("password hash has unsafe parameters")
	}
	// Bound hostile database values before allocating memory.
	if parameters.memory > 1024*1024 || parameters.iterations > 20 || parameters.parallelism > 32 {
		return argonParameters{}, nil, nil, errors.New("password hash parameters exceed supported limits")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return argonParameters{}, nil, nil, errors.New("password hash has an invalid salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 128 {
		return argonParameters{}, nil, nil, errors.New("password hash has an invalid digest")
	}
	return parameters, salt, expected, nil
}
