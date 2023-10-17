package weka_events

import (
	"context"

	"github.com/weka/go-cloud-lib/lib/jrpc"
	"github.com/weka/go-cloud-lib/lib/types"
	"github.com/weka/go-cloud-lib/lib/weka"
	"github.com/weka/go-cloud-lib/logging"
)

func EmitCustomEventUsingJPool(ctx context.Context, message string, jpool *jrpc.Pool) error {
	logger := logging.LoggerFromCtx(ctx)

	input := types.JsonDict{
		"message": message,
	}

	err := jpool.Call(weka.JrpcEmitCustomEvent, input, nil)
	if err != nil {
		logger.Error().Err(err).Send()
		return err
	}
	return nil
}
