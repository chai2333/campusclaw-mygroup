package auth

import (
	"encoding/base64"
	"golang.org/x/crypto/argon2"
	"strings"
	"testing"
)

func TestPasswordSaltsAndVerification(t *testing.T) {
	a, err := HashPassword("fixture")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("fixture")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !VerifyPassword(a, "fixture") || VerifyPassword(a, "wrong") {
		t.Fatal("password verification or unique salts failed")
	}
	if _, err := HashPassword(strings.Repeat("x", 73)); err == nil {
		t.Fatal("overlength bcrypt input accepted")
	}
}
func TestLegacyArgonAndLongPassword(t *testing.T) {
	for _, password := range []string{"fixture", strings.Repeat("字", 40)} {
		salt := []byte("fixture-salt-1234")
		sum := argon2.IDKey([]byte(password), salt, 3, 65536, 4, 32)
		encoded := "$argon2id$v=19$m=65536,t=3,p=4$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(sum)
		if !VerifyPassword(encoded, password) || VerifyPassword(encoded, password+"wrong") {
			t.Fatal("legacy verification failed")
		}
		if NeedsUpgrade(encoded, password) != (len([]byte(password)) <= 72) {
			t.Fatal("unsafe upgrade decision")
		}
	}
}
func TestMalformedHash(t *testing.T) {
	for _, encoded := range []string{"plain", "$argon2id$v=19$m=4294967295,t=3,p=4$eA$eA", "$argon2id$v=19$m=65536,t=3,p=0$eA$eA", "$2bad"} {
		if VerifyPassword(encoded, "fixture") {
			t.Fatal("accepted malformed hash")
		}
	}
}
