/*
Copyright SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

import (
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// GardenletApplyConfiguration represents an declarative configuration of the Gardenlet type for use
// with apply.
type GardenletApplyConfiguration struct {
	Deployment      *GardenletDeploymentApplyConfiguration `json:"deployment,omitempty"`
	Config          *runtime.RawExtension                  `json:"config,omitempty"`
	Bootstrap       *seedmanagementv1alpha1.Bootstrap      `json:"bootstrap,omitempty"`
	MergeWithParent *bool                                  `json:"mergeWithParent,omitempty"`
}

// GardenletApplyConfiguration constructs an declarative configuration of the Gardenlet type for use with
// apply.
func Gardenlet() *GardenletApplyConfiguration {
	return &GardenletApplyConfiguration{}
}

// WithDeployment sets the Deployment field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Deployment field is set to the value of the last call.
func (b *GardenletApplyConfiguration) WithDeployment(value *GardenletDeploymentApplyConfiguration) *GardenletApplyConfiguration {
	b.Deployment = value
	return b
}

// WithConfig sets the Config field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Config field is set to the value of the last call.
func (b *GardenletApplyConfiguration) WithConfig(value runtime.RawExtension) *GardenletApplyConfiguration {
	b.Config = &value
	return b
}

// WithBootstrap sets the Bootstrap field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Bootstrap field is set to the value of the last call.
func (b *GardenletApplyConfiguration) WithBootstrap(value seedmanagementv1alpha1.Bootstrap) *GardenletApplyConfiguration {
	b.Bootstrap = &value
	return b
}

// WithMergeWithParent sets the MergeWithParent field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the MergeWithParent field is set to the value of the last call.
func (b *GardenletApplyConfiguration) WithMergeWithParent(value bool) *GardenletApplyConfiguration {
	b.MergeWithParent = &value
	return b
}