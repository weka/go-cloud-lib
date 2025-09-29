package common

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"time"

	"github.com/lithammer/dedent"
	"github.com/weka/go-cloud-lib/protocol"
)

func ShuffleSlice(slice []string) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(slice), func(i, j int) { slice[i], slice[j] = slice[j], slice[i] })
}

func GetHashedPrivateIp(privateIp string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(privateIp)))[:16]
}

func GetScriptWithReport(message, reportFunctionDef string, protocol protocol.ProtocolGW) string {
	s := `
	#!/bin/bash

	# report function definition
	%s

	PROTOCOL="%s"
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"progress\", \"message\": \"%s\"}"

	echo "%s"
	`
	return fmt.Sprintf(dedent.Dedent(s), reportFunctionDef, protocol, message, message)
}

func GetErrorScript(err error, reportFunctionDef string, protocol protocol.ProtocolGW) string {
	s := `
	#!/bin/bash

	# report function definition
	%s

	PROTOCOL="%s"
	report "{\"hostname\": \"$HOSTNAME\", \"protocol\": \"$PROTOCOL\", \"type\": \"error\", \"message\": \"%s\"}"

	<<'###ERROR'
	%s
	###ERROR
	exit 1
	`
	return fmt.Sprintf(dedent.Dedent(s), reportFunctionDef, protocol, err.Error(), err.Error())
}

func IsItemInList(item string, list []string) bool {
	for _, listItem := range list {
		if item == listItem {
			return true
		}
	}
	return false
}

func GetInstancesNames(instances []protocol.Vm) (vmNames []string) {
	for _, instance := range instances {
		vmNames = append(vmNames, instance.Name)
	}
	return
}
