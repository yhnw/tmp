// Package argon2id wraps the golang.org/x/crypto/argon2 package and
// provides APIs similar to the golang.org/x/crypto/bcrypt package.
package argon2id

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Some references:
// https://www.rfc-editor.org/rfc/rfc9106.html#name-parameter-choice
// https://github.com/P-H-C/phc-winner-argon2/blob/f57e61e19229e23c4445b85494dbf7c07de721cb/src/encoding.c#L244C5-L244C61
// https://github.com/P-H-C/phc-string-format
// https://pages.nist.gov/800-63-4/sp800-63b.html#passwordver

// The error returned from CompareHashAndPassword when a password and hash do
// not match.
var ErrMismatchedHashAndPassword = errors.New("argon2id: hashedPassword is not the hash of the given password")

// Config represents input parameters of Argon2id.
// According to RFC 9106, the FIRST RECOMMENDED option is
// m=21(2Gib of RAM), t=1, p=4, T=32, S=16. If much less memory is available,
// the SECOND RECOMMENDED option is m=16(64Mib of RAM), t=3, p=4, T=32, S=16.
type Config struct {
	// Memory is the parameter "m", the memory size in Kib.
	Memory uint32

	// Time is the parameter "t", the number of passes.
	// It is used to tune the running time independently of the memory size.
	Time uint32

	// Parallelism is the parameter "p", the number of parallelism.
	// It determines how many independent computational chains can be run.
	Parallelism uint8

	// KeyLength is the parameter "T", the length of output in bytes.
	KeyLength uint32

	// SaltLength is the parameter "S", the length of salt in bytes.
	SaltLength uint32
}

// NewConfig returns a new instance of Config with RFC 9106's the FIRST RECOMMENDED option.
func NewConfig() Config {
	return Config{
		Memory:      2 * 1024 * 1024,
		Time:        1,
		Parallelism: 4,
		KeyLength:   32,
		SaltLength:  16,
	}
}

var getRandomSalt = randomSalt

func randomSalt(len uint32) []byte {
	salt := make([]byte, len)
	_, err := rand.Read(salt)
	if err != nil {
		panic(err)
	}
	return salt
}

// GenerateFromPassword returns the PHC string format of argon2id hash of the password.
func (cfg Config) GenerateFromPassword(password []byte) []byte {
	salt := getRandomSalt(cfg.SaltLength)
	key := argon2.IDKey([]byte(password), salt, cfg.Time, cfg.Memory, cfg.Parallelism, cfg.KeyLength)
	return fmt.Appendf(nil, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, cfg.Memory, cfg.Time, cfg.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// CompareHashAndPassword compares the PHC string format of an argon2id hashed password with its possible plaintext equivalent.
// It returns parsed Config and nil on success, or the zero Config and an error on failure.
// If a password and hash do not match, it returns the zero Config and ErrMismatchedHashAndPassword.
func CompareHashAndPassword(hashedPassword, password []byte) (Config, error) {
	fields := strings.Split(string(hashedPassword), "$")
	if len(fields) != 6 {
		return Config{}, fmt.Errorf("argon2id: invalid format %q", hashedPassword)
	}

	if fields[1] != "argon2id" {
		return Config{}, fmt.Errorf("argon2id: variant mismatch %q", fields[1])
	}

	var version int
	_, err := fmt.Sscanf(fields[2], "v=%d", &version)
	if err != nil {
		return Config{}, fmt.Errorf("argon2id: %v", err)
	}
	if version != argon2.Version {
		return Config{}, fmt.Errorf("argon2id: version mismatch %q", version)
	}

	var cfg Config
	_, err = fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &cfg.Memory, &cfg.Time, &cfg.Parallelism)
	if err != nil {
		return Config{}, fmt.Errorf("argon2id: %v", err)
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(fields[4])
	if err != nil {
		return Config{}, fmt.Errorf("argon2id: %v", err)
	}
	cfg.SaltLength = uint32(len(salt))

	key, err := base64.RawStdEncoding.Strict().DecodeString(fields[5])
	if err != nil {
		return Config{}, fmt.Errorf("argon2id: %v", err)
	}
	cfg.KeyLength = uint32(len(key))

	otherKey := argon2.IDKey([]byte(password), salt, cfg.Time, cfg.Memory, cfg.Parallelism, cfg.KeyLength)

	if subtle.ConstantTimeCompare(key, otherKey) != 1 {
		return Config{}, ErrMismatchedHashAndPassword
	}
	return cfg, nil
}
