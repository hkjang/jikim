package password

import "testing"

func TestHashAndVerify(t *testing.T) {
	encoded, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(encoded, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if Verify(encoded, "incorrect horse battery staple") {
		t.Fatal("incorrect password accepted")
	}
}

func TestPasswordValidationAndMalformedHash(t *testing.T) {
	if _, err := Hash("short"); err == nil {
		t.Fatal("short password accepted")
	}
	for _, malformed := range []string{"", "$pbkdf2-sha256$1$bad$bad", "$unknown$310000$a$b"} {
		if Verify(malformed, "anything") {
			t.Fatalf("malformed hash accepted: %q", malformed)
		}
	}
	if _, _, _, ok := parseHash(dummyEncodedHash); !ok {
		t.Fatal("dummy password hash is not a valid PBKDF2 hash")
	}
	if Verify("", "jikim-invalid-account-password") {
		t.Fatal("dummy verification must never authenticate an absent account")
	}
}
