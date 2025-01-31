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

package deliverable_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vmware-tanzu/cartographer/pkg/apis/v1alpha1"
	realizer "github.com/vmware-tanzu/cartographer/pkg/realizer/deliverable"
	"github.com/vmware-tanzu/cartographer/pkg/realizer/deliverable/deliverablefakes"
	"github.com/vmware-tanzu/cartographer/pkg/templates"
)

var _ = Describe("Realize", func() {
	var (
		resourceRealizer *deliverablefakes.FakeResourceRealizer
		delivery         *v1alpha1.ClusterDelivery
		resource1        v1alpha1.ClusterDeliveryResource
		resource2        v1alpha1.ClusterDeliveryResource
		rlzr             realizer.Realizer
	)
	BeforeEach(func() {
		rlzr = realizer.NewRealizer()

		resourceRealizer = &deliverablefakes.FakeResourceRealizer{}
		resource1 = v1alpha1.ClusterDeliveryResource{
			Name: "resource1",
		}
		resource2 = v1alpha1.ClusterDeliveryResource{
			Name: "resource2",
		}
		delivery = &v1alpha1.ClusterDelivery{
			ObjectMeta: metav1.ObjectMeta{Name: "greatest-delivery"},
			Spec: v1alpha1.ClusterDeliverySpec{
				Resources: []v1alpha1.ClusterDeliveryResource{resource1, resource2},
			},
		}
	})

	It("realizes each resource in delivery order, accumulating output for each subsequent resource", func() {
		outputFromFirstResource := &templates.Output{Config: "whatever"}

		var executedResourceOrder []string

		resourceRealizer.DoCalls(func(ctx context.Context, resource *v1alpha1.ClusterDeliveryResource, deliveryName string, outputs realizer.Outputs) (*templates.Output, error) {
			executedResourceOrder = append(executedResourceOrder, resource.Name)
			Expect(deliveryName).To(Equal("greatest-delivery"))
			if resource.Name == "resource1" {
				Expect(outputs).To(Equal(realizer.NewOutputs()))
				return outputFromFirstResource, nil
			}

			if resource.Name == "resource2" {
				expectedSecondResourceOutputs := realizer.NewOutputs()
				expectedSecondResourceOutputs.AddOutput("resource1", outputFromFirstResource)
				Expect(outputs).To(Equal(expectedSecondResourceOutputs))
			}

			return &templates.Output{}, nil
		})

		Expect(rlzr.Realize(context.TODO(), resourceRealizer, delivery)).To(Succeed())

		Expect(executedResourceOrder).To(Equal([]string{"resource1", "resource2"}))
	})

	It("returns any error encountered realizing a resource", func() {
		resourceRealizer.DoReturns(nil, errors.New("realizing is hard"))
		Expect(rlzr.Realize(context.TODO(), resourceRealizer, delivery)).To(MatchError("realizing is hard"))
	})
})
