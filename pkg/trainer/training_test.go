// Copyright 2018 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trainer

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"
	torchv1alpha1 "github.com/jose5918/pytorch-operator/pkg/apis/pytorch/v1alpha1"
	tfJobFake "github.com/kubeflow/tf-operator/pkg/client/clientset/versioned/fake"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

func TestIsRetryableTerminationState(t *testing.T) {
	type TestCase struct {
		State    v1.ContainerStateTerminated
		Expected bool
	}

	cases := []TestCase{
		{
			// Since reason is empty we don't trust the exit code.
			State: v1.ContainerStateTerminated{
				ExitCode: 0,
			},
			Expected: false,
		},
		{
			State: v1.ContainerStateTerminated{
				ExitCode: 0,
				Message:  "some reason",
			},
			Expected: false,
		},
		{
			State: v1.ContainerStateTerminated{
				ExitCode: 1,
				Message:  "some reason",
			},
			Expected: false,
		},
		{
			State: v1.ContainerStateTerminated{
				ExitCode: 1,
			},
			Expected: false,
		},
		{
			State: v1.ContainerStateTerminated{
				ExitCode: 244,
				Message:  "some reason",
			},
			Expected: true,
		},
		{
			State: v1.ContainerStateTerminated{
				ExitCode: 244,
				Reason:   "OOMKilled",
			},
			Expected: false,
		},
	}

	for _, c := range cases {
		actual := isRetryableTerminationState(&c.State)
		if actual != c.Expected {
			t.Errorf("isRetryableTerminationState(%+v)=%v want %v", c.State, actual, c.Expected)
		}
	}
}

func TestClusterSpec(t *testing.T) {
	type TestCase struct {
		Spec     *torchv1alpha1.TFJob
		Expected map[string][]string
	}

	cases := []TestCase{
		{
			Spec: &torchv1alpha1.TFJob{
				ObjectMeta: metav1.ObjectMeta{
					Name: "myjob",
				},
				Spec: torchv1alpha1.TFJobSpec{
					RuntimeId: "runtime",
					ReplicaSpecs: []*torchv1alpha1.TFReplicaSpec{
						{
							Replicas: proto.Int32(2),
							TFPort:   proto.Int32(22),
							Template: &v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "tensorflow",
										},
									},
								},
							},
							TFReplicaType: torchv1alpha1.PS,
						},
						{
							Replicas: proto.Int32(1),
							TFPort:   proto.Int32(42),
							Template: &v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "tensorflow",
										},
									},
								},
							},
							TFReplicaType: torchv1alpha1.MASTER,
						},
						{
							Replicas: proto.Int32(3),
							TFPort:   proto.Int32(40),
							Template: &v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "tensorflow",
										},
									},
								},
							},
							TFReplicaType: torchv1alpha1.WORKER,
						},
					},
				},
			},

			Expected: map[string][]string{
				"ps":     []string{"myjob-ps-runtime-0:22", "myjob-ps-runtime-1:22"},
				"master": []string{"myjob-master-runtime-0:42"},
				"worker": []string{"myjob-worker-runtime-0:40", "myjob-worker-runtime-1:40", "myjob-worker-runtime-2:40"},
			},
		},
	}

	for _, c := range cases {

		clientSet := fake.NewSimpleClientset()

		recorder := record.NewFakeRecorder(100)
		job, err := initJob(clientSet, &tfJobFake.Clientset{}, recorder, c.Spec)

		if err != nil {
			t.Fatalf("initJob failed: %v", err)
		}

		job.setup(&torchv1alpha1.ControllerConfig{})
		job.setupReplicas()
		actual := job.ClusterSpec()

		for k, v := range c.Expected {
			actualV, ok := actual[k]
			if !ok {
				t.Errorf("Actual cluster spec is missing key: %v", k)
				continue
			}
			if !reflect.DeepEqual(actualV, v) {
				t.Errorf("Key %v got %v want %v", k, actualV, v)
			}
		}
	}
}

func TestJobSetup(t *testing.T) {
	// Verify the setup will fill in the RuntimeId.
	clientSet := fake.NewSimpleClientset()

	type testCase struct {
		jobSpec      *torchv1alpha1.TFJob
		expectMounts int
		expectPhase  torchv1alpha1.TFJobPhase
		expectReason string
		expectState  torchv1alpha1.State
	}

	testCases := []testCase{
		{
			jobSpec: &torchv1alpha1.TFJob{
				Spec: torchv1alpha1.TFJobSpec{
					ReplicaSpecs: []*torchv1alpha1.TFReplicaSpec{
						{
							Replicas: proto.Int32(1),
							TFPort:   proto.Int32(10),
							Template: &v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "tensorflow",
										},
									},
								},
							},
							TFReplicaType: torchv1alpha1.MASTER,
						},
					},
				},
			},
			expectMounts: 0,
			expectPhase:  torchv1alpha1.TFJobPhaseCreating,
			expectState:  torchv1alpha1.StateRunning,
		},
		{
			jobSpec: &torchv1alpha1.TFJob{
				Spec: torchv1alpha1.TFJobSpec{
					ReplicaSpecs: []*torchv1alpha1.TFReplicaSpec{
						{
							Replicas: proto.Int32(2),
							TFPort:   proto.Int32(10),
							Template: &v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "tensorflow",
											Resources: v1.ResourceRequirements{
												Requests: map[v1.ResourceName]resource.Quantity{
													"nvidia-gpu": resource.MustParse("1"),
												},
											},
										},
									},
								},
							},
							TFReplicaType: torchv1alpha1.WORKER,
						},
					},
					TerminationPolicy: &torchv1alpha1.TerminationPolicySpec{
						Chief: &torchv1alpha1.ChiefSpec{
							ReplicaName:  string(torchv1alpha1.WORKER),
							ReplicaIndex: 0,
						},
					},
				},
			},
			expectMounts: 1,
			expectPhase:  torchv1alpha1.TFJobPhaseCreating,
			expectState:  torchv1alpha1.StateRunning,
		},
		{
			// The job should fail setup because the spec is invalid.
			jobSpec: &torchv1alpha1.TFJob{
				Spec: torchv1alpha1.TFJobSpec{
					ReplicaSpecs: []*torchv1alpha1.TFReplicaSpec{
						{
							Replicas: proto.Int32(2),
							TFPort:   proto.Int32(10),
							Template: &v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "tensorflow",
											Resources: v1.ResourceRequirements{
												Requests: map[v1.ResourceName]resource.Quantity{
													"nvidia-gpu": resource.MustParse("1"),
												},
											},
										},
									},
								},
							},
							TFReplicaType: torchv1alpha1.WORKER,
						},
					},
				},
			},
			expectMounts: 0,
			expectPhase:  torchv1alpha1.TFJobPhaseFailed,
			expectState:  torchv1alpha1.StateFailed,
			expectReason: "invalid job spec: Missing ReplicaSpec for chief: MASTER",
		},
	}

	config := &torchv1alpha1.ControllerConfig{
		Accelerators: map[string]torchv1alpha1.AcceleratorConfig{
			"nvidia-gpu": torchv1alpha1.AcceleratorConfig{
				Volumes: []torchv1alpha1.AcceleratorVolume{
					{
						Name:      "cuda-lib",
						HostPath:  "/home/cuda",
						MountPath: "/usr/local/cuda",
					},
				},
			},
		},
	}

	for _, c := range testCases {

		recorder := record.NewFakeRecorder(100)
		job, err := initJob(clientSet, &tfJobFake.Clientset{}, recorder, c.jobSpec)

		job.setup(config)

		if err != nil {
			t.Errorf("j.setup error: %v", err)
		}

		if job.status.Phase != c.expectPhase {
			t.Errorf("job.job.Status.Phase Want: %v Got:%v ", c.expectPhase, job.status.Phase)
		}

		if job.status.Reason != c.expectReason {
			t.Errorf("job.job.Status.Reason Want: %v Got:%v ", c.expectReason, job.status.Reason)
		}

		if job.status.State != c.expectState {
			t.Errorf("job.job.Status.State Want: %v Got:%v ", c.expectState, job.status.State)
		}

		// Make sure the runtime id is set if the job didn't fail.
		if c.expectState != torchv1alpha1.StateFailed && job.job.Spec.RuntimeId == "" {
			t.Errorf("RuntimeId should not be empty after calling setup.")
		}

		if len(job.job.Spec.ReplicaSpecs[0].Template.Spec.Volumes) != c.expectMounts {
			t.Errorf("Expect %v Volumes got %v", c.expectMounts, len(job.job.Spec.ReplicaSpecs[0].Template.Spec.Volumes))
		}

		if len(job.job.Spec.ReplicaSpecs[0].Template.Spec.Containers[0].VolumeMounts) != c.expectMounts {
			t.Errorf("Expect %v VolumeMounts got %v", c.expectMounts, len(job.job.Spec.ReplicaSpecs[0].Template.Spec.Containers[0].VolumeMounts))
		}
	}
}
