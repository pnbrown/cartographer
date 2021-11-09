// Copyright 2021 VMware
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workload

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/vmware-tanzu/cartographer/pkg/apis/v1alpha1"
	"github.com/vmware-tanzu/cartographer/pkg/repository"
	"github.com/vmware-tanzu/cartographer/pkg/templates"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate

//counterfeiter:generate . ResourceRealizer
type ResourceRealizer interface {
	Do(ctx context.Context, resource *v1alpha1.SupplyChainResource, supplyChainName string, outputs Outputs) (*templates.Output, error)
}

type ClientBuilder func(secret *corev1.Secret, client client.Client) (client.Client, error)

func NewClientBuilder(host string) ClientBuilder {
	return func(secret *corev1.Secret, cl client.Client) (client.Client, error) {
		config, err := buildConfig(secret, host)
		if err != nil {
			panic("snap") //FIXME
		}

		client, err := client.New(config, client.Options{})
		if err != nil {
			panic("crackle") //FIXME
		}

		return client, nil
	}
}





/**/
func buildConfig(secret *corev1.Secret, host string) (*rest.Config, error) {
	caBytes, found := secret.Data[corev1.ServiceAccountRootCAKey]
	if !found {
		return nil, fmt.Errorf("couldn't find service account token ca")
	}

	tokenBytes, found := secret.Data[corev1.ServiceAccountTokenKey]
	if !found {
		return nil, fmt.Errorf("couldn't find service account token value")
	}

	tlsClientConfig := rest.TLSClientConfig{
		CAData: caBytes,
	}

	return &rest.Config{
		Host:            host,
		TLSClientConfig: tlsClientConfig,
		BearerToken:     string(tokenBytes),
	}, nil
}
/**/

type resourceRealizer struct {
	workload   *v1alpha1.Workload
	systemRepo repository.Repository
	workloadRepo repository.Repository
}

type ResourceRealizerBuilder func(ctx context.Context, secret *corev1.Secret, workload *v1alpha1.Workload, systemRepo repository.Repository) (ResourceRealizer, error)

//counterfeiter:generate sigs.k8s.io/controller-runtime/pkg/client.Client
func NewResourceRealizerBuilder(client client.Client, repositoryBuilder repository.RepositoryBuilder, clientBuilder ClientBuilder) ResourceRealizerBuilder {
	return func(ctx context.Context, secret *corev1.Secret, workload *v1alpha1.Workload, systemRepo repository.Repository) (ResourceRealizer, error) {
		workloadClient, err := clientBuilder(secret, client)
		if err != nil {
			panic("surelynot") //FIXME: go getem
		}

		logger := logr.FromContext(ctx)

		workloadRepo := repositoryBuilder(workloadClient,
			repository.NewCache(logger.WithName("workload-stamping-repo-cache")),
			logger.WithName("workload-stamping-repo"))

		return &resourceRealizer{
			workload:   workload,
			systemRepo: systemRepo,
			workloadRepo: workloadRepo,
		}, nil
	}
}

func (r *resourceRealizer) Do(ctx context.Context, resource *v1alpha1.SupplyChainResource, supplyChainName string, outputs Outputs) (*templates.Output, error) {
	template, err := r.systemRepo.GetClusterTemplate(resource.TemplateRef)
	if err != nil {
		return nil, GetClusterTemplateError{
			Err:         err,
			TemplateRef: resource.TemplateRef,
		}
	}

	labels := map[string]string{
		"carto.run/workload-name":             r.workload.Name,
		"carto.run/workload-namespace":        r.workload.Namespace,
		"carto.run/cluster-supply-chain-name": supplyChainName,
		"carto.run/resource-name":             resource.Name,
		"carto.run/template-kind":             template.GetKind(),
		"carto.run/cluster-template-name":     template.GetName(),
	}

	inputs := outputs.GenerateInputs(resource)
	workloadTemplatingContext := map[string]interface{}{
		"workload": r.workload,
		"params":   templates.ParamsBuilder(template.GetDefaultParams(), resource.Params),
		"sources":  inputs.Sources,
		"images":   inputs.Images,
		"configs":  inputs.Configs,
	}

	// Todo: this belongs in Stamp.
	if inputs.OnlyConfig() != nil {
		workloadTemplatingContext["config"] = inputs.OnlyConfig()
	}
	if inputs.OnlyImage() != nil {
		workloadTemplatingContext["image"] = inputs.OnlyImage()
	}
	if inputs.OnlySource() != nil {
		workloadTemplatingContext["source"] = inputs.OnlySource()
	}

	stampContext := templates.StamperBuilder(r.workload, workloadTemplatingContext, labels)
	stampedObject, err := stampContext.Stamp(ctx, template.GetResourceTemplate())
	if err != nil {
		return nil, StampError{
			Err:      err,
			Resource: resource,
		}
	}

	err = r.workloadRepo.EnsureObjectExistsOnCluster(stampedObject, true)
	if err != nil {
		return nil, ApplyStampedObjectError{
			Err:           err,
			StampedObject: stampedObject,
		}
	}

	output, err := template.GetOutput(stampedObject)
	if err != nil {
		return nil, RetrieveOutputError{
			Err:      err,
			resource: resource,
		}
	}

	return output, nil
}
