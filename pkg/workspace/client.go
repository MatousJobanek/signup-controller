package workspace

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	labelType             = "type"
	labelNamespace        = "namespace"
	labelOwnerClusterName = "ownerClusterName"

	defaultHostOperatorNamespace   = "toolchain-host-operator"
	defaultMemberOperatorNamespace = "toolchain-member-operator"

	toolchainAPIQPS   = 20.0
	toolchainAPIBurst = 30
	tokenKey          = "token"
	caCrtKey          = "ca.crt"
)

type Config struct {
	Config *rest.Config
	URL    string
}

func GetWorkspaceConfigs(cl client.Client, namespace string, opts ...client.ListOption) ([]*Config, error) {

	secrets := &v1.SecretList{}
	if err := cl.List(context.TODO(), secrets, append(opts, client.InNamespace(namespace))...); err != nil {
		return nil, err
	}

	if len(secrets.Items) == 0 {
		fmt.Println("getting all")
		secrets := &v1.SecretList{}
		if err := cl.List(context.TODO(), secrets, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		fmt.Println(secrets)
		return nil, nil
	}

	fmt.Println(secrets)

	var configs []*Config

	for _, secret := range secrets.Items {

		apiEndpoint, ok := secret.Annotations["url"]
		if !ok {
			fmt.Println("the url annotation is not set", secret.Name)
			continue
		}

		tokenValue, tokenFound := secret.Data[tokenKey]
		if !tokenFound || len(tokenValue) == 0 {
			return nil, fmt.Errorf("the secret for cluster %s is missing a non-empty value for %q", secret.Name, tokenKey)
		}
		//fmt.Println(string(tokenValue))
		//token, err := base64.StdEncoding.DecodeString(string(tokenValue))
		//if err != nil {
		//	fmt.Println("failed to parse token")
		//	return nil, err
		//}

		restConfig, err := clientcmd.BuildConfigFromFlags(apiEndpoint, "")
		if err != nil {
			return nil, err
		}

		caValue, caFound := secret.Data[caCrtKey]
		if !caFound || len(caValue) == 0 {
			return nil, fmt.Errorf("the secret for cluster %s is missing a non-empty value for %q", secret.Name, caCrtKey)
		}

		//ca, err := base64.StdEncoding.DecodeString(string(caValue))
		//if err != nil {
		//	fmt.Println("failed to parse ca")
		//	return nil, err
		//}
		//restConfig.CAData = caValue
		restConfig.BearerToken = string(tokenValue)
		restConfig.QPS = toolchainAPIQPS
		restConfig.Burst = toolchainAPIBurst
		restConfig.Timeout = 10 * time.Second

		configs = append(configs, &Config{
			URL:    apiEndpoint,
			Config: restConfig,
		})
	}

	return configs, nil
}
