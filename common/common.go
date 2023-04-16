package common

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"time"
)

func ShuffleSlice(slice []string) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(slice), func(i, j int) { slice[i], slice[j] = slice[j], slice[i] })
}

func GetHashedPrivateIp(privateIp string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(privateIp)))[:16]
}
