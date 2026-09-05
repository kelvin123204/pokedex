package auth

import (
	"testing"
)

func TestHashingPasswordAndValidatingHash(t *testing.T) {
	hashedPassword, _ := HashPassword("myPassword")
	result, _ := CheckPasswordHash("myPassword", hashedPassword)
	if !result {
		t.Errorf("Expecting result to be true, got %t", result)
	}
}
