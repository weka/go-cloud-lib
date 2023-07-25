package common

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"time"

	"github.com/lithammer/dedent"
)

func ShuffleSlice(slice []string) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(slice), func(i, j int) { slice[i], slice[j] = slice[j], slice[i] })
}

func GetHashedPrivateIp(privateIp string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(privateIp)))[:16]
}

func GetScriptWithReport(message, reportFunctionDef string) string {
	s := `
	#!/bin/bash

	# report function definition
	%s

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"progress\", \"message\": \"%s\"}"

	echo "%s"
	`
	return fmt.Sprintf(dedent.Dedent(s), reportFunctionDef, message, message)
}

func GetErrorScript(err error, reportFunctionDef string) string {
	s := `
	#!/bin/bash

	# report function definition
	%s

	report "{\"hostname\": \"$HOSTNAME\", \"type\": \"error\", \"message\": \"%s\"}"

	<<'###ERROR'
	%s
	###ERROR
	exit 1
	`
	return fmt.Sprintf(dedent.Dedent(s), reportFunctionDef, err.Error(), err.Error())
}
