package utils

import (
	"math/rand"
	"time"
)

func GeneratePassword(passwordLength, minLower, minUpper, minNumbers int) string {
	lowerCase := "abcdefghijklmnopqrstuvwxyz" // lowercase
	upperCase := "ABCDEFGHIJKLMNOPQRSTUVWXYZ" // uppercase
	numbers := "0123456789"                   // numbers

	password := make([]byte, passwordLength)
	source := rand.NewSource(time.Now().UnixNano())
	rng := rand.New(source)

	// Ensure minimum required characters are present
	for i := 0; i < minLower; i++ {
		randCharNum := rng.Intn(len(lowerCase))
		password[i] = lowerCase[randCharNum]
	}

	for i := 0; i < minUpper; i++ {
		randCharNum := rng.Intn(len(upperCase))
		password[minLower+i] = upperCase[randCharNum]
	}

	for i := 0; i < minNumbers; i++ {
		randCharNum := rng.Intn(len(numbers))
		password[minLower+minUpper+i] = numbers[randCharNum]
	}

	// Fill the remaining characters randomly
	for i := minLower + minUpper + minNumbers; i < passwordLength; i++ {
		randNum := rng.Intn(4)
		switch randNum {
		case 0:
			randCharNum := rng.Intn(len(lowerCase))
			password[i] = lowerCase[randCharNum]
		case 1:
			randCharNum := rng.Intn(len(upperCase))
			password[i] = upperCase[randCharNum]
		case 2:
			randCharNum := rng.Intn(len(numbers))
			password[i] = numbers[randCharNum]
		}
	}

	// Shuffle the password to avoid predictable sequences
	rng.Shuffle(passwordLength, func(i, j int) {
		password[i], password[j] = password[j], password[i]
	})
	return string(password)
}
