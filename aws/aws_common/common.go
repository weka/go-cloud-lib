package aws_common

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/rs/zerolog/log"
	"github.com/weka/go-cloud-lib/aws/connectors"
	"github.com/weka/go-cloud-lib/protocol"
)

func GetSecret(secretId string) (secret string, err error) {
	svc := connectors.GetAWSSession().SecretsManager
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretId),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get secret value")
		return
	}
	secret = *result.SecretString
	return
}

func GetUsernameAndPassword(usernameId, passwordId string) (clusterCreds protocol.ClusterCreds, err error) {
	log.Info().Msgf("Fetching username %s and password %s", usernameId, passwordId)
	clusterCreds.Username, err = GetSecret(usernameId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}
	clusterCreds.Password, err = GetSecret(passwordId)
	return
}
