/*
Copyright 2017 The Kubernetes Authors.

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

package common

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Wait for upto 10 minutes for nvidia drivers to be installed.
const driverInstallationTimeout = 10 * time.Minute

var _ = framework.KubeDescribe("GPU [Feature:GPU]", func() {
	f := framework.NewDefaultFramework("gpu-test")
	Context("attempt to use GPUs if available", func() {
		It("setup the node and create pods to test gpus", func() {
			maxGPUcount := resource.NewQuantity(0, resource.DecimalSI)
			getGPUsAvailable := func() bool {
				By("Getting a list node objects from the api server")
				nodeList, err := f.ClientSet.Core().Nodes().List(metav1.ListOptions{})
				framework.ExpectNoError(err, "getting node list")
				for _, node := range nodeList.Items {
					gpus := node.Status.Capacity.NvidiaGPU()
					if gpus.Value() > maxGPUcount.Value() {
						maxGPUcount = gpus
					}
				}
				return maxGPUcount.Value() != 0
			}
			Eventually(getGPUsAvailable, driverInstallationTimeout, framework.Poll).Should(Equal(false))
			gpusAvailable := maxGPUcount
			By("Creating a pod that will consume all GPUs")
			podSuccess := makePod(gpusAvailable.Value(), "gpus-success")
			podSuccess = f.PodClient().CreateSync(podSuccess)

			By("Checking the containers in the pod had restarted at-least twice successfully thereby ensuring GPUs are reused")
			const minContainerRestartCount = 2
			Eventually(func() bool {
				p, err := f.ClientSet.Core().Pods(f.Namespace.Name).Get(podSuccess.Name, metav1.GetOptions{})
				if err != nil {
					framework.Logf("failed to get pod status: %v", err)
					return false
				}
				if p.Status.ContainerStatuses[0].RestartCount < minContainerRestartCount {
					return false
				}
				return true
			}, time.Minute, time.Second).Should(BeTrue())

			By("Checking if the pod outputted Success to its logs")
			framework.ExpectNoError(f.PodClient().MatchContainerOutput(podSuccess.Name, podSuccess.Name, "Success"))

			By("Creating a new pod requesting a GPU and noticing that it is rejected by the Kubelet")
			podFailure := makePod(1, "gpu-failure")
			framework.WaitForPodCondition(f.ClientSet, f.Namespace.Name, podFailure.Name, "pod rejected", framework.PodStartTimeout, func(pod *v1.Pod) (bool, error) {
				if pod.Status.Phase == v1.PodFailed {
					return true, nil

				}
				return false, nil
			})

			By("stopping the original Pod with GPUs")
			gp := int64(0)
			deleteOptions := metav1.DeleteOptions{
				GracePeriodSeconds: &gp,
			}
			f.PodClient().DeleteSync(podSuccess.Name, &deleteOptions, framework.DefaultPodDeletionTimeout)

			By("attempting to start the failed pod again")
			f.PodClient().DeleteSync(podFailure.Name, &deleteOptions, framework.DefaultPodDeletionTimeout)
			podFailure = f.PodClient().CreateSync(podFailure)

			By("Checking if the pod outputted Success to its logs")
			framework.ExpectNoError(f.PodClient().MatchContainerOutput(podFailure.Name, podFailure.Name, "Success"))
		})
	})
})

func makePod(gpus int64, name string) *v1.Pod {
	resources := v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceNvidiaGPU: *resource.NewQuantity(gpus, resource.DecimalSI),
		},
	}
	gpuverificationCmd := fmt.Sprintf("if [[ %d -ne $(ls /dev/ | egrep '^nvidia[0-9]+$') ]]; then exit 1; fi; echo Success", gpus)
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyAlways,
			Containers: []v1.Container{
				{
					Image:     "gcr.io/google_containers/busybox:1.24",
					Name:      name,
					Command:   []string{"sh", "-c", gpuverificationCmd},
					Resources: resources,
				},
			},
		},
	}
}
