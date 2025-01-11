package argon2id

import (
	"bytes"
	"io"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
)

func testConfigs() []Config {
	cfgs := []Config{}
	for keyLength := range 4 {
		for saltLength := range 4 {
			for memory := range 2 {
				for time := range 2 {
					for parallelism := range max(runtime.NumCPU(), 2) {
						cfg := Config{
							KeyLength:   uint32(keyLength + 4),
							SaltLength:  uint32(saltLength + 1),
							Memory:      uint32(math.Pow(2, float64(memory+10))),
							Time:        uint32(time + 1),
							Parallelism: uint8(parallelism + 1),
						}
						cfgs = append(cfgs, cfg)
					}
				}
			}
		}
	}
	return cfgs
}

func TestSimple(t *testing.T) {
	for _, password := range []string{"hunter2", "correcthorsebatterystaple"} {
		for _, cfg := range testConfigs() {
			t.Run("", func(t *testing.T) {
				t.Parallel()
				hash := cfg.GenerateFromPassword([]byte(password))
				got, err := CompareHashAndPassword(hash, []byte(password))
				if err != nil {
					t.Fatal(err)
				}
				if got != cfg {
					t.Fatalf("\ngot\n\t%+v\nwant\n\t%+v", got, cfg)
				}
			})
		}
	}
}

func TestUpdateConfig(t *testing.T) {
	password := []byte("hunter2")

	cfg := NewConfig()
	cfg.Memory = 16
	hash := cfg.GenerateFromPassword(password)
	cfg2, err := CompareHashAndPassword(hash, password)
	if err != nil {
		t.Fatal(err)
	}

	cfg2.Memory++
	cfg2.Time++

	hash2 := cfg2.GenerateFromPassword(password)
	if bytes.Equal(hash, hash2) {
		t.Fatalf("%s == %s", hash, hash2)
	}

	cfg3, err := CompareHashAndPassword(hash2, password)
	if err != nil {
		t.Error(err)
	}
	if cfg3 != cfg2 {
		t.Fatalf("\ngot\n\t%+v\nwant\n\t%+v", cfg3, cfg2)
	}
}

func TestWithArgon2(t *testing.T) {
	if _, err := exec.LookPath("argon2"); err != nil {
		t.Log(`"argon2" command not found, skipping TestArgon2`)
		t.Skip()
	}

	defer func() { getRandomSalt = randomSalt }()

	for _, password := range []string{"hunter2", "correcthorsebatterystaple"} {
		for _, salt := range []string{"somesalt", "atleast8"} {
			for _, cfg := range testConfigs() {
				t.Run("", func(t *testing.T) {
					getRandomSalt = func(_ uint32) []byte { return []byte(salt) }
					got := cfg.GenerateFromPassword([]byte(password))
					_, err := CompareHashAndPassword(got, []byte(password))
					if err != nil {
						t.Fatal(err)
					}

					cmd := exec.Command("argon2", salt, "-e", "-id",
						"-t", strconv.Itoa(int(cfg.Time)),
						"-m", strconv.Itoa(int(math.Log2(float64(cfg.Memory)))),
						"-p", strconv.Itoa(int(cfg.Parallelism)),
						"-l", strconv.Itoa(int(cfg.KeyLength)))

					// println(cmd.String())
					stdin, err := cmd.StdinPipe()
					if err != nil {
						t.Fatal(err)
					}
					go func() {
						defer stdin.Close()
						io.WriteString(stdin, string(password))
					}()
					want, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatal(err)
					}

					want = bytes.TrimSpace(want)
					if !bytes.Equal(got, want) {
						t.Fatalf("\ngot\n\t%s\nwant\n\t%s", got, want)
					}
				})
			}
		}
	}
}
