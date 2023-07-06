package common

import (
	"crypto/sha256"
	"fmt"
	"github.com/lithammer/dedent"
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

func GetErrorScript(err error) string {
	s := `
	#!/bin/bash
	<<'###ERROR'
	%s
	###ERROR
	exit 1
	`
	return fmt.Sprintf(dedent.Dedent(s), err.Error())
}
