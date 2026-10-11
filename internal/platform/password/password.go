package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	MaxPasswordBytes    = 1024
	MaxEncodedHashBytes = 512
	MinArgonMemoryKiB   = 8 * 1024
	MaxArgonMemoryKiB   = 256 * 1024
	MaxArgonIterations  = 10
	MaxArgonParallelism = 16
	MaxArgonSaltBytes   = 64
	MaxArgonKeyBytes    = 64
)

type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

func Hash(plain string) (string, error) {
	return HashWithParams(plain, DefaultParams)
}

func HashWithParams(plain string, p Params) (string, error) {
	return hashWithParams(plain, p, 12)
}

// HashWithMinimumLength applies a caller-specific password policy while retaining
// the standard Argon2id parameters and maximum input size.
func HashWithMinimumLength(plain string, minimumBytes int) (string, error) {
	if minimumBytes < 1 || minimumBytes > MaxPasswordBytes {
		return "", errors.New("password minimum length is outside the allowed range")
	}
	return hashWithParams(plain, DefaultParams, minimumBytes)
}

func hashWithParams(plain string, p Params, minimumBytes int) (string, error) {
	if len(plain) < minimumBytes {
		return "", fmt.Errorf("password must contain at least %d bytes", minimumBytes)
	}
	if len(plain) > MaxPasswordBytes {
		return "", fmt.Errorf("password exceeds %d bytes", MaxPasswordBytes)
	}
	if err := validateParams(p); err != nil {
		return "", err
	}

	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.Memory,
		p.Iterations,
		p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func Verify(encoded, plain string) (bool, error) {
	if len(plain) > MaxPasswordBytes {
		return false, nil
	}
	p, salt, expected, err := parse(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parse(encoded string) (Params, []byte, []byte, error) {
	if len(encoded) > MaxEncodedHashBytes {
		return Params{}, nil, nil, errors.New("encoded password hash is too large")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("invalid password hash format")
	}

	versionText := strings.TrimPrefix(parts[2], "v=")
	version, err := strconv.Atoi(versionText)
	if err != nil || version != argon2.Version {
		return Params{}, nil, nil, errors.New("unsupported Argon2id version")
	}

	p, err := parseParams(parts[3])
	if err != nil {
		return Params{}, nil, nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 {
		return Params{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 {
		return Params{}, nil, nil, errors.New("invalid Argon2id hash")
	}

	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(expected))
	if err := validateParams(p); err != nil {
		return Params{}, nil, nil, err
	}
	return p, salt, expected, nil
}

func parseParams(raw string) (Params, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 3 {
		return Params{}, errors.New("invalid Argon2id parameters")
	}

	memory, err := parseParam(parts[0], "m", 32)
	if err != nil {
		return Params{}, err
	}
	iterations, err := parseParam(parts[1], "t", 32)
	if err != nil {
		return Params{}, err
	}
	parallelism, err := parseParam(parts[2], "p", 8)
	if err != nil {
		return Params{}, err
	}

	return Params{
		Memory:      uint32(memory),
		Iterations:  uint32(iterations),
		Parallelism: uint8(parallelism),
	}, nil
}

func parseParam(raw, expectedKey string, bitSize int) (uint64, error) {
	key, value, ok := strings.Cut(raw, "=")
	if !ok || key != expectedKey || value == "" {
		return 0, errors.New("invalid Argon2id parameters")
	}
	parsed, err := strconv.ParseUint(value, 10, bitSize)
	if err != nil {
		return 0, errors.New("invalid Argon2id parameters")
	}
	return parsed, nil
}

func validateParams(p Params) error {
	switch {
	case p.Memory < MinArgonMemoryKiB || p.Memory > MaxArgonMemoryKiB:
		return errors.New("Argon2id memory parameter is outside the allowed range")
	case p.Iterations < 1 || p.Iterations > MaxArgonIterations:
		return errors.New("Argon2id iteration parameter is outside the allowed range")
	case p.Parallelism < 1 || p.Parallelism > MaxArgonParallelism:
		return errors.New("Argon2id parallelism parameter is outside the allowed range")
	case p.SaltLength < 16 || p.SaltLength > MaxArgonSaltBytes:
		return errors.New("Argon2id salt length is outside the allowed range")
	case p.KeyLength < 16 || p.KeyLength > MaxArgonKeyBytes:
		return errors.New("Argon2id key length is outside the allowed range")
	default:
		return nil
	}
}
