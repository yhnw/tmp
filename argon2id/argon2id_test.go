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

func testConfigs() []Parameter {
	params := []Parameter{}
	for keyLength := range 4 {
		for saltLength := range 4 {
			for memory := range 2 {
				for time := range 2 {
					for parallelism := range max(runtime.NumCPU(), 2) {
						param := Parameter{
							KeyLength:   uint32(keyLength + 4),
							SaltLength:  uint32(saltLength + 1),
							Memory:      uint32(math.Pow(2, float64(memory+10))),
							Time:        uint32(time + 1),
							Parallelism: uint8(parallelism + 1),
						}
						params = append(params, param)
					}
				}
			}
		}
	}
	return params
}

func TestSimple(t *testing.T) {
	for _, password := range []string{"hunter2", "correcthorsebatterystaple"} {
		for _, param := range testConfigs() {
			t.Run("", func(t *testing.T) {
				t.Parallel()
				hash := GenerateFromPassword(param, password)
				got, err := CompareHashAndPassword(hash, password)
				if err != nil {
					t.Fatal(err)
				}
				if got != param {
					t.Fatalf("\ngot\n\t%+v\nwant\n\t%+v", got, param)
				}
			})
		}
	}
}

func TestUpdateParameter(t *testing.T) {
	password := []byte("hunter2")

	param := ParameterSecondRecommendationByRFC9106()
	param.Memory = 16
	hash := GenerateFromPassword(param, password)
	param2, err := CompareHashAndPassword(hash, password)
	if err != nil {
		t.Fatal(err)
	}

	// update parameter
	param2 = ParameterFirstRecommendationByRFC9106()
	param2.Memory = 16

	hash2 := GenerateFromPassword(param2, password)
	if bytes.Equal(hash, hash2) {
		t.Fatalf("hash mismatch after parameter update: %s == %s", hash, hash2)
	}

	param3, err := CompareHashAndPassword(hash2, password)
	if err != nil {
		t.Fatal(err)
	}
	if param3 != param2 {
		t.Fatalf("\ngot\n\t%+v\nwant\n\t%+v", param3, param2)
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
			for _, param := range testConfigs() {
				t.Run("", func(t *testing.T) {
					getRandomSalt = func(_ uint32) []byte { return []byte(salt) }
					got := GenerateFromPassword(param, []byte(password))
					_, err := CompareHashAndPassword(got, password)
					if err != nil {
						t.Fatal(err)
					}

					cmd := exec.Command("argon2", salt, "-e", "-id",
						"-t", strconv.Itoa(int(param.Time)),
						"-m", strconv.Itoa(int(math.Log2(float64(param.Memory)))),
						"-p", strconv.Itoa(int(param.Parallelism)),
						"-l", strconv.Itoa(int(param.KeyLength)))

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
