// Copyright The OpenTelemetry Authors
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

package controllers_test

import (
	"context"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	k8sreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/open-telemetry/opentelemetry-operator/apis/v1alpha1"
	"github.com/open-telemetry/opentelemetry-operator/controllers"
	"github.com/open-telemetry/opentelemetry-operator/internal/config"
	"github.com/open-telemetry/opentelemetry-operator/internal/manifests"
	ta "github.com/open-telemetry/opentelemetry-operator/internal/manifests/targetallocator/adapters"
	"github.com/open-telemetry/opentelemetry-operator/internal/naming"
)

const (
	baseTaImage    = "something:tag"
	updatedTaImage = "another:tag"
	expectHostname = "something-else.com"
	labelName      = "something"
	labelVal       = "great"
	annotationName = "io.opentelemetry/test"
	annotationVal  = "true"
)

var (
	extraPorts = v1.ServicePort{
		Name:       "port-web",
		Protocol:   "TCP",
		Port:       8080,
		TargetPort: intstr.FromInt32(8080),
	}
)

type check func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector)

func newParamsAssertNoErr(t *testing.T, taContainerImage string, file string) manifests.Params {
	p, err := newParams(taContainerImage, file)
	assert.NoError(t, err)
	if len(taContainerImage) == 0 {
		p.Instance.Spec.TargetAllocator.Enabled = false
	}
	return p
}

func TestOpenTelemetryCollectorReconciler_Reconcile(t *testing.T) {
	addedMetadataDeployment := paramsWithMode(v1alpha1.ModeDeployment)
	addedMetadataDeployment.Instance.Labels = map[string]string{
		labelName: labelVal,
	}
	addedMetadataDeployment.Instance.Annotations = map[string]string{
		annotationName: annotationVal,
	}
	deploymentExtraPorts := paramsWithModeAndReplicas(v1alpha1.ModeDeployment, 3)
	deploymentExtraPorts.Instance.Spec.Ports = append(deploymentExtraPorts.Instance.Spec.Ports, extraPorts)
	ingressParams := newParamsAssertNoErr(t, "", testFileIngress)
	ingressParams.Instance.Spec.Ingress.Type = "ingress"
	updatedIngressParams := newParamsAssertNoErr(t, "", testFileIngress)
	updatedIngressParams.Instance.Spec.Ingress.Type = "ingress"
	updatedIngressParams.Instance.Spec.Ingress.Annotations = map[string]string{"blub": "blob"}
	updatedIngressParams.Instance.Spec.Ingress.Hostname = expectHostname
	routeParams := newParamsAssertNoErr(t, "", testFileIngress)
	routeParams.Instance.Spec.Ingress.Type = v1alpha1.IngressTypeRoute
	routeParams.Instance.Spec.Ingress.Route.Termination = v1alpha1.TLSRouteTerminationTypeInsecure
	updatedRouteParams := newParamsAssertNoErr(t, "", testFileIngress)
	updatedRouteParams.Instance.Spec.Ingress.Type = v1alpha1.IngressTypeRoute
	updatedRouteParams.Instance.Spec.Ingress.Route.Termination = v1alpha1.TLSRouteTerminationTypeInsecure
	updatedRouteParams.Instance.Spec.Ingress.Hostname = expectHostname

	type args struct {
		params manifests.Params
		// an optional list of updates to supply after the initial object
		updates []manifests.Params
	}
	type want struct {
		// result check
		result controllerruntime.Result
		// a check to run against the current state applied
		checks []check
		// if an error from creation validation is expected
		validateErr assert.ErrorAssertionFunc
		// if an error from reconciliation is expected
		wantErr assert.ErrorAssertionFunc
	}
	tests := []struct {
		name string
		args args
		want []want
	}{
		{
			name: "deployment collector",
			args: args{
				params:  addedMetadataDeployment,
				updates: []manifests.Params{deploymentExtraPorts},
			},
			want: []want{
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							d := appsv1.Deployment{}
							exists, err := populateObjectIfExists(t, &d, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Equal(t, int32(2), *d.Spec.Replicas)
							assert.Contains(t, d.Annotations, annotationName)
							assert.Contains(t, d.Labels, labelName)
							exists, err = populateObjectIfExists(t, &v1.Service{}, namespacedObjectName(appliedInstance, naming.Service))
							assert.NoError(t, err)
							assert.True(t, exists)
							exists, err = populateObjectIfExists(t, &v1.ServiceAccount{}, namespacedObjectName(appliedInstance, naming.ServiceAccount))
							assert.NoError(t, err)
							assert.True(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							d := appsv1.Deployment{}
							exists, err := populateObjectIfExists(t, &d, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Equal(t, int32(3), *d.Spec.Replicas)
							// confirm that we don't remove annotations and labels even if we don't set them
							assert.Contains(t, d.Annotations, annotationName)
							assert.Contains(t, d.Labels, labelName)
							actual := v1.Service{}
							exists, err = populateObjectIfExists(t, &actual, namespacedObjectName(appliedInstance, naming.Service))
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Contains(t, actual.Spec.Ports, extraPorts)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
			},
		},
		{
			name: "invalid mode",
			args: args{
				params:  paramsWithMode("bad"),
				updates: []manifests.Params{},
			},
			want: []want{
				{
					result:  controllerruntime.Result{},
					checks:  []check{},
					wantErr: assert.NoError,
					validateErr: func(t assert.TestingT, err2 error, msgAndArgs ...interface{}) bool {
						return assert.ErrorContains(t, err2, "Unsupported value: \"bad\"", msgAndArgs)
					},
				},
			},
		},
		{
			name: "invalid prometheus configuration",
			args: args{
				params:  newParamsAssertNoErr(t, baseTaImage, testFileIngress),
				updates: []manifests.Params{},
			},
			want: []want{
				{
					result:  controllerruntime.Result{},
					checks:  []check{},
					wantErr: assert.NoError,
					validateErr: func(t assert.TestingT, err2 error, msgAndArgs ...interface{}) bool {
						return assert.ErrorContains(t, err2, "no prometheus available as part of the configuration", msgAndArgs)
					},
				},
			},
		},
		{
			name: "deployment collector with ingress",
			args: args{
				params:  ingressParams,
				updates: []manifests.Params{updatedIngressParams},
			},
			want: []want{
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							d := networkingv1.Ingress{}
							exists, err := populateObjectIfExists(t, &d, namespacedObjectName(appliedInstance, naming.Ingress))
							assert.NoError(t, err)
							assert.True(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							d := networkingv1.Ingress{}
							exists, err := populateObjectIfExists(t, &d, namespacedObjectName(appliedInstance, naming.Ingress))
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Equal(t, "something-else.com", d.Spec.Rules[0].Host)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
			},
		},
		{
			name: "deployment collector with routes",
			args: args{
				params:  routeParams,
				updates: []manifests.Params{updatedRouteParams},
			},
			want: []want{
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							got := routev1.Route{}
							nsn := types.NamespacedName{Namespace: appliedInstance.Namespace, Name: "otlp-grpc-test-route"}
							exists, err := populateObjectIfExists(t, &got, nsn)
							assert.NoError(t, err)
							assert.True(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							got := routev1.Route{}
							nsn := types.NamespacedName{Namespace: appliedInstance.Namespace, Name: "otlp-grpc-test-route"}
							exists, err := populateObjectIfExists(t, &got, nsn)
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Equal(t, "otlp-grpc.something-else.com", got.Spec.Host)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
			},
		},
		{
			name: "hpa v2 deployment collector",
			args: args{
				params:  paramsWithHPA(3, 5),
				updates: []manifests.Params{paramsWithHPA(1, 9)},
			},
			want: []want{
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							actual := autoscalingv2beta2.HorizontalPodAutoscaler{}
							exists, hpaErr := populateObjectIfExists(t, &actual, namespacedObjectName(appliedInstance, naming.HorizontalPodAutoscaler))
							assert.NoError(t, hpaErr)
							require.Len(t, actual.Spec.Metrics, 1)
							assert.Equal(t, int32(90), *actual.Spec.Metrics[0].Resource.Target.AverageUtilization)
							assert.Equal(t, int32(3), *actual.Spec.MinReplicas)
							assert.Equal(t, int32(5), actual.Spec.MaxReplicas)
							assert.True(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							actual := autoscalingv2beta2.HorizontalPodAutoscaler{}
							exists, hpaErr := populateObjectIfExists(t, &actual, namespacedObjectName(appliedInstance, naming.HorizontalPodAutoscaler))
							assert.NoError(t, hpaErr)
							require.Len(t, actual.Spec.Metrics, 1)
							assert.Equal(t, int32(90), *actual.Spec.Metrics[0].Resource.Target.AverageUtilization)
							assert.Equal(t, int32(1), *actual.Spec.MinReplicas)
							assert.Equal(t, int32(9), actual.Spec.MaxReplicas)
							assert.True(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
			},
		},
		{
			name: "daemonset collector",
			args: args{
				params: paramsWithMode(v1alpha1.ModeDaemonSet),
			},
			want: []want{
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							exists, err := populateObjectIfExists(t, &appsv1.DaemonSet{}, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
			},
		},
		{
			name: "stateful should update collector with TA",
			args: args{
				params: paramsWithMode(v1alpha1.ModeStatefulSet),
				updates: []manifests.Params{
					newParamsAssertNoErr(t, baseTaImage, promFile),
					newParamsAssertNoErr(t, baseTaImage, updatedPromFile),
					newParamsAssertNoErr(t, updatedTaImage, updatedPromFile),
				},
			},
			want: []want{
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							exists, err := populateObjectIfExists(t, &v1.ConfigMap{}, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
							exists, err = populateObjectIfExists(t, &appsv1.StatefulSet{}, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
							// Check the TA doesn't exist
							exists, err = populateObjectIfExists(t, &v1.ConfigMap{}, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.False(t, exists)
							exists, err = populateObjectIfExists(t, &appsv1.Deployment{}, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.False(t, exists)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							exists, err := populateObjectIfExists(t, &v1.ConfigMap{}, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
							actual := v1.ConfigMap{}
							exists, err = populateObjectIfExists(t, &appsv1.Deployment{}, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.True(t, exists)
							exists, err = populateObjectIfExists(t, &actual, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.True(t, exists)
							exists, err = populateObjectIfExists(t, &v1.ServiceAccount{}, namespacedObjectName(appliedInstance, naming.TargetAllocatorServiceAccount))
							assert.NoError(t, err)
							assert.True(t, exists)

							promConfig, err := ta.ConfigToPromConfig(newParamsAssertNoErr(t, baseTaImage, promFile).Instance.Spec.Config)
							assert.NoError(t, err)

							taConfig := make(map[interface{}]interface{})
							taConfig["label_selector"] = map[string]string{
								"app.kubernetes.io/instance":   "default.test",
								"app.kubernetes.io/managed-by": "opentelemetry-operator",
								"app.kubernetes.io/component":  "opentelemetry-collector",
								"app.kubernetes.io/part-of":    "opentelemetry",
							}
							taConfig["config"] = promConfig["config"]
							taConfig["allocation_strategy"] = "least-weighted"
							taConfig["prometheus_cr"] = map[string]string{
								"scrape_interval": "30s",
							}
							taConfigYAML, _ := yaml.Marshal(taConfig)
							assert.Equal(t, string(taConfigYAML), actual.Data["targetallocator.yaml"])
							assert.NotContains(t, actual.Data["targetallocator.yaml"], "0.0.0.0:10100")
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							exists, err := populateObjectIfExists(t, &v1.ConfigMap{}, namespacedObjectName(appliedInstance, naming.Collector))
							assert.NoError(t, err)
							assert.True(t, exists)
							actual := v1.ConfigMap{}
							exists, err = populateObjectIfExists(t, &appsv1.Deployment{}, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.True(t, exists)
							exists, err = populateObjectIfExists(t, &actual, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Contains(t, actual.Data["targetallocator.yaml"], "0.0.0.0:10100")
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
				{
					result: controllerruntime.Result{},
					checks: []check{
						func(t *testing.T, appliedInstance v1alpha1.OpenTelemetryCollector) {
							actual := appsv1.Deployment{}
							exists, err := populateObjectIfExists(t, &actual, namespacedObjectName(appliedInstance, naming.TargetAllocator))
							assert.NoError(t, err)
							assert.True(t, exists)
							assert.Equal(t, actual.Spec.Template.Spec.Containers[0].Image, updatedTaImage)
						},
					},
					wantErr:     assert.NoError,
					validateErr: assert.NoError,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContext := context.Background()
			nsn := types.NamespacedName{Name: tt.args.params.Instance.Name, Namespace: tt.args.params.Instance.Namespace}
			reconciler := controllers.NewReconciler(controllers.Params{
				Client:   k8sClient,
				Log:      logger,
				Scheme:   testScheme,
				Recorder: record.NewFakeRecorder(20),
				Config: config.New(
					config.WithCollectorImage("default-collector"),
					config.WithTargetAllocatorImage("default-ta-allocator"),
				),
			})
			assert.True(t, len(tt.want) > 0, "must have at least one group of checks to run")
			firstCheck := tt.want[0]
			createErr := k8sClient.Create(testContext, &tt.args.params.Instance)
			if !firstCheck.validateErr(t, createErr) {
				return
			}
			req := k8sreconcile.Request{
				NamespacedName: nsn,
			}
			got, reconcileErr := reconciler.Reconcile(testContext, req)
			if !firstCheck.wantErr(t, reconcileErr) {
				require.NoError(t, k8sClient.Delete(testContext, &tt.args.params.Instance))
				return
			}
			assert.Equal(t, firstCheck.result, got)
			for _, check := range firstCheck.checks {
				check(t, tt.args.params.Instance)
			}
			// run the next set of checks
			for pid, updateParam := range tt.args.updates {
				existing := v1alpha1.OpenTelemetryCollector{}
				found, err := populateObjectIfExists(t, &existing, nsn)
				assert.True(t, found)
				assert.NoError(t, err)

				updateParam.Instance.SetResourceVersion(existing.ResourceVersion)
				updateParam.Instance.SetUID(existing.UID)
				err = k8sClient.Update(testContext, &updateParam.Instance)
				assert.NoError(t, err)
				if err != nil {
					continue
				}
				req := k8sreconcile.Request{
					NamespacedName: nsn,
				}
				_, err = reconciler.Reconcile(testContext, req)
				// account for already checking the initial group
				checkGroup := tt.want[pid+1]
				if !checkGroup.wantErr(t, err) {
					return
				}
				assert.Equal(t, checkGroup.result, got)
				for _, check := range checkGroup.checks {
					check(t, updateParam.Instance)
				}
			}
			// Only delete upon a successful creation
			if createErr == nil {
				require.NoError(t, k8sClient.Delete(testContext, &tt.args.params.Instance))
			}
		})
	}
}

func namespacedObjectName(instance v1alpha1.OpenTelemetryCollector, namingFunc func(string) string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      namingFunc(instance.Name),
	}
}
