package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/weka/go-cloud-lib/connectors"
	"github.com/weka/go-cloud-lib/lib/jrpc"
	"github.com/weka/go-cloud-lib/lib/weka"
	"strings"
)

type stringList []string

// Implement the flag.Value interface for our type
func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = strings.Split(value, ",")
	return nil
}

func main() {
	fmt.Println("github.com/weka/go-cloud-lib")

	var ips stringList

	flag.Var(&ips, "ips", "comma-separated ips")
	username := flag.String("username", "", "weka username")
	password := flag.String("password", "", "weka password")
	flag.Parse()

	if len(ips) == 0 || *username == "" || *password == "" {
		fmt.Println("Usage: go run examples/jrpc.go --ips ip1,ip2 --username=username --password=password")
		return
	}

	ctx := context.Background()
	jrpcBuilder := func(ip string) *jrpc.BaseClient {
		return connectors.NewJrpcClient(ctx, ip, weka.ManagementJrpcPort, *username, *password)
	}
	jpool := &jrpc.Pool{
		Ips:     ips,
		Clients: map[string]*jrpc.BaseClient{},
		Active:  "",
		Builder: jrpcBuilder,
		Ctx:     ctx,
	}

	systemStatus := weka.StatusResponse{}
	err := jpool.Call(weka.JrpcStatus, struct{}{}, &systemStatus)
	if err != nil {
		fmt.Println("Failed to get system status")
		return
	}
	fmt.Printf("System status: %+v\n", systemStatus)

	debugOverrideList := weka.ManualDebugOverrideListResponse{}
	err = jpool.Call(weka.JrpcManualOverrideList, struct{}{}, &debugOverrideList)
	if err != nil {
		fmt.Println("Failed to get cloud is skip scale down")
		return
	}
	fmt.Printf("Cloud skip scale down: %+v\n", debugOverrideList)
}
