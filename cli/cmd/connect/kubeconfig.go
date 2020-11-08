package connect

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/porter-dev/porter/cli/cmd/utils"
	"github.com/porter-dev/porter/internal/kubernetes/local"
	gcpLocal "github.com/porter-dev/porter/internal/providers/gcp/local"

	"github.com/porter-dev/porter/cli/cmd/api"
	"github.com/porter-dev/porter/internal/models"
)

// Kubeconfig creates a service account for a project by parsing the local
// kubeconfig and resolving actions that must be performed.
func Kubeconfig(
	kubeconfigPath string,
	contexts []string,
	host string,
	projectID uint,
) error {
	// if project ID is 0, ask the user to set the project ID or create a project
	if projectID == 0 {
		return fmt.Errorf("no project set, please run porter project set [id]")
	}

	// get the kubeconfig
	rawBytes, err := local.GetKubeconfigFromHost(kubeconfigPath, contexts)

	if err != nil {
		return err
	}

	// send kubeconfig to client
	client := api.NewClient(host+"/api", "cookie.json")

	saCandidates, err := client.CreateProjectCandidates(
		context.Background(),
		projectID,
		&api.CreateProjectCandidatesRequest{
			Kubeconfig: string(rawBytes),
		},
	)

	if err != nil {
		return err
	}

	for _, saCandidate := range saCandidates {
		resolvers := make(api.CreateProjectServiceAccountRequest, 0)

		for _, action := range saCandidate.Actions {
			switch action.Name {
			case models.ClusterCADataAction:
				resolveAction, err := resolveClusterCAAction(action.Filename)

				if err != nil {
					return err
				}

				resolvers = append(resolvers, resolveAction)
			case models.ClientCertDataAction:
				resolveAction, err := resolveClientCertAction(action.Filename)

				if err != nil {
					return err
				}

				resolvers = append(resolvers, resolveAction)
			case models.ClientKeyDataAction:
				resolveAction, err := resolveClientKeyAction(action.Filename)

				if err != nil {
					return err
				}

				resolvers = append(resolvers, resolveAction)
			case models.OIDCIssuerDataAction:
				resolveAction, err := resolveOIDCIssuerAction(action.Filename)

				if err != nil {
					return err
				}

				resolvers = append(resolvers, resolveAction)
			case models.TokenDataAction:
				resolveAction, err := resolveTokenDataAction(action.Filename)

				if err != nil {
					return err
				}

				resolvers = append(resolvers, resolveAction)
			case models.GCPKeyDataAction:
				resolveAction, err := resolveGCPKeyAction(
					saCandidate.ClusterEndpoint,
					saCandidate.ClusterName,
				)

				if err != nil {
					return err
				}

				resolvers = append(resolvers, resolveAction)
			case models.AWSKeyDataAction:
			}
		}

		sa, err := client.CreateProjectServiceAccount(
			context.Background(),
			projectID,
			saCandidate.ID,
			resolvers,
		)

		if err != nil {
			return err
		}

		for _, cluster := range sa.Clusters {
			fmt.Printf("created service account for cluster %s with id %d\n", cluster.Name, sa.ID)

			// sanity check to ensure it's working
			// namespaces, err := client.GetK8sNamespaces(
			// 	context.Background(),
			// 	projectID,
			// 	sa.ID,
			// 	cluster.ID,
			// )

			// if err != nil {
			// 	return err
			// }

			// for _, ns := range namespaces.Items {
			// 	fmt.Println(ns.ObjectMeta.GetName())
			// }
		}
	}

	return nil
}

// resolves a cluster ca data action
func resolveClusterCAAction(
	filename string,
) (*models.ServiceAccountAllActions, error) {
	fileBytes, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, err
	}

	return &models.ServiceAccountAllActions{
		Name:          models.ClusterCADataAction,
		ClusterCAData: base64.StdEncoding.EncodeToString(fileBytes),
	}, nil
}

// resolves a client cert data action
func resolveClientCertAction(
	filename string,
) (*models.ServiceAccountAllActions, error) {
	fileBytes, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, err
	}

	return &models.ServiceAccountAllActions{
		Name:           models.ClientCertDataAction,
		ClientCertData: base64.StdEncoding.EncodeToString(fileBytes),
	}, nil
}

// resolves a client key data action
func resolveClientKeyAction(
	filename string,
) (*models.ServiceAccountAllActions, error) {
	fileBytes, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, err
	}

	return &models.ServiceAccountAllActions{
		Name:          models.ClientKeyDataAction,
		ClientKeyData: base64.StdEncoding.EncodeToString(fileBytes),
	}, nil
}

// resolves an oidc issuer data action
func resolveOIDCIssuerAction(
	filename string,
) (*models.ServiceAccountAllActions, error) {
	fileBytes, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, err
	}

	return &models.ServiceAccountAllActions{
		Name:             models.OIDCIssuerDataAction,
		OIDCIssuerCAData: base64.StdEncoding.EncodeToString(fileBytes),
	}, nil
}

// resolves a token data action
func resolveTokenDataAction(
	filename string,
) (*models.ServiceAccountAllActions, error) {
	fileBytes, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, err
	}

	return &models.ServiceAccountAllActions{
		Name:      models.TokenDataAction,
		TokenData: string(fileBytes),
	}, nil
}

// resolves a gcp key data action
func resolveGCPKeyAction(endpoint string, clusterName string) (*models.ServiceAccountAllActions, error) {
	userResp, err := utils.PromptPlaintext(
		fmt.Sprintf(
			`Detected GKE cluster in kubeconfig for the endpoint %s (%s). 
Porter can set up a service account in your GCP project to connect to this cluster automatically.
Would you like to proceed? [y/n] `,
			endpoint,
			clusterName,
		),
	)

	if err != nil {
		return nil, err
	}

	if userResp := strings.ToLower(userResp); userResp == "y" || userResp == "yes" {
		agent, _ := gcpLocal.NewDefaultAgent()
		projID, err := agent.GetProjectIDForGKECluster(endpoint)

		if err != nil {
			return nil, err
		}

		agent.ProjectID = projID

		name := "porter-dashboard-" + utils.StringWithCharset(6, "abcdefghijklmnopqrstuvwxyz1234567890")

		// create the service account and give it the correct iam permissions
		resp, err := agent.CreateServiceAccount(name)

		if err != nil {
			fmt.Println("Automatic creation failed.")
			return resolveGCPKeyActionManual(endpoint, clusterName)
		}

		err = agent.SetServiceAccountIAMPolicy(resp)

		if err != nil {
			return nil, err
		}

		// get the service account key data to send to the server
		bytes, err := agent.CreateServiceAccountKey(resp)

		if err != nil {
			return nil, err
		}

		return &models.ServiceAccountAllActions{
			Name:       models.GCPKeyDataAction,
			GCPKeyData: string(bytes),
		}, nil
	}

	return resolveGCPKeyActionManual(endpoint, clusterName)
}

func resolveGCPKeyActionManual(endpoint string, clusterName string) (*models.ServiceAccountAllActions, error) {
	keyFileLocation, err := utils.PromptPlaintext(`Please provide the full path to a service account key file.
Key file location: `)

	if err != nil {
		return nil, err
	}

	// attempt to read the key file location
	if info, err := os.Stat(keyFileLocation); !os.IsNotExist(err) && !info.IsDir() {
		// read the file
		bytes, err := ioutil.ReadFile(keyFileLocation)

		if err != nil {
			return nil, err
		}

		return &models.ServiceAccountAllActions{
			Name:       models.GCPKeyDataAction,
			GCPKeyData: string(bytes),
		}, nil
	}

	return nil, errors.New("Key file not found")
}

// resolves an aws key data action