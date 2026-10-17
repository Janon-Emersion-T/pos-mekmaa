package main

import "testing"

func TestPasswordComplexity(t *testing.T) {
	tests := []struct {
		password string
		valid    bool
	}{
		{"asdf@1234", true},
		{"abcdefgh", false},
		{"12345678", false},
		{"abcd1234", false},
		{"ABC@12345", true},
		{"Abc123!@", true},
		{"short1!", false},
	}

	for _, tt := range tests {
		if got := validPassword(tt.password); got != tt.valid {
			t.Errorf("validPassword(%q) = %v, want %v",
				tt.password, got, tt.valid)
		}
	}
}

func TestGeneratedTemporaryPasswordMeetsPolicy(t *testing.T) {
	for i := 0; i < 100; i++ {
		password, err := generateTemporaryPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(password) != 16 || !validPassword(password) {
			t.Fatalf("generated password does not meet policy: %q", password)
		}
	}
}
