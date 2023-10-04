package weka_events

import (
	"context"

	"github.com/weka/go-cloud-lib/connectors"
	"github.com/weka/go-cloud-lib/lib/jrpc"
	"github.com/weka/go-cloud-lib/lib/types"
	"github.com/weka/go-cloud-lib/lib/weka"
	"github.com/weka/go-cloud-lib/logging"
)

type EmitEventParams struct {
	Username   string
	Password   string
	BackendIps []string
	Message    string
}

func EmitCustomEvent(ctx context.Context, params EmitEventParams) error {
	logger := logging.LoggerFromCtx(ctx)

	jrpcBuilder := func(ip string) *jrpc.BaseClient {
		return connectors.NewJrpcClient(ctx, ip, weka.ManagementJrpcPort, params.Username, params.Password)
	}

	jpool := &jrpc.Pool{
		Ips:     params.BackendIps,
		Clients: map[string]*jrpc.BaseClient{},
		Active:  "",
		Builder: jrpcBuilder,
		Ctx:     ctx,
	}

	input := types.JsonDict{
		"message": params.Message,
	}

	err := jpool.Call(weka.JrpcEmitCustomEvent, input, nil)
	if err != nil {
		logger.Error().Err(err).Send()
		return err
	}
	return nil
}
