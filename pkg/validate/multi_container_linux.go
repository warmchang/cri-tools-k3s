/*
Copyright 2018 The Kubernetes Authors.

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

package validate

import (
	"context"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	internalapi "k8s.io/cri-api/pkg/apis"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	"sigs.k8s.io/cri-tools/pkg/common"
	"sigs.k8s.io/cri-tools/pkg/framework"
)

const (
	httpdContainerPort = 80
	httpdImage         = framework.DefaultRegistryE2ETestImagesPrefix + "httpd:2.4.39-4"
)

var _ = framework.KubeDescribe("Multiple Containers [Conformance]", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService

	BeforeEach(func() {
		rc = f.CRIClient.CRIRuntimeClient
		ic = f.CRIClient.CRIImageClient
	})

	Context("when running multiple containers in a pod", func() {
		var podID, httpdContainerID, busyboxContainerID, logDir string
		var podConfig *runtimeapi.PodSandboxConfig

		BeforeEach(func() {
			By("create a PodSandbox")
			podID, podConfig, logDir = createMultiContainerTestPodSandbox(rc)

			By("create a httpd container")
			httpdContainerID = createMultiContainerTestHttpdContainer(rc, ic, "httpd", podID, podConfig)
			By("start the httpd container")
			testStartContainer(rc, httpdContainerID)

			By("create a busybox container")
			busyboxContainerID = createMultiContainerTestBusyboxContainer(rc, ic, "busybox", podID, podConfig)
			By("start the busybox container")
			testStartContainer(rc, busyboxContainerID)
		})

		AfterEach(func() {
			By("stop PodSandbox")
			Expect(rc.StopPodSandbox(context.TODO(), podID)).To(Succeed())
			By("delete PodSandbox")
			Expect(rc.RemovePodSandbox(context.TODO(), podID)).To(Succeed())
			By("cleanup log path")
			Expect(os.RemoveAll(logDir)).To(Succeed())
		})

		It("should support network", func() {
			checkMainPage(rc, podID, 0, httpdContainerPort)
		})

		It("should support container log", func() {
			verifyContainerLog := func(path, expected string) func() (bool, error) {
				return func() (bool, error) {
					content, err := os.ReadFile(path)
					if err != nil {
						return false, err
					}

					return strings.Contains(string(content), expected), nil
				}
			}
			httpdStatus, err := rc.ContainerStatus(context.TODO(), httpdContainerID, false)
			Expect(err).NotTo(HaveOccurred(), "get httpd container status")
			Eventually(verifyContainerLog(httpdStatus.GetStatus().GetLogPath(),
				"httpd"), time.Minute, 100*time.Millisecond).Should(BeTrue())

			busyboxStatus, err := rc.ContainerStatus(context.TODO(), busyboxContainerID, false)
			Expect(err).NotTo(HaveOccurred(), "get busybox container status")
			Eventually(verifyContainerLog(busyboxStatus.GetStatus().GetLogPath(),
				defaultLog), time.Minute, 100*time.Millisecond).Should(BeTrue())
		})

		It("should support container exec", func() {
			Expect(execSyncContainer(rc, httpdContainerID, []string{"echo", "httpd"})).To(Equal("httpd\n"))
			Expect(execSyncContainer(rc, busyboxContainerID, []string{"echo", "busybox"})).To(Equal("busybox\n"))
		})
	})
})

// createMultiContainerTestPodSandbox creates a sandbox with log directory and a container port for httpd container.
func createMultiContainerTestPodSandbox(c internalapi.RuntimeService) (sandboxID string, podConfig *runtimeapi.PodSandboxConfig, logDir string) {
	podSandboxName := "PodSandbox-for-multi-container-test-" + framework.NewUUID()
	uid := framework.DefaultUIDPrefix + framework.NewUUID()
	namespace := framework.DefaultNamespacePrefix + framework.NewUUID()
	logDir, podLogPath := createLogTempDir(podSandboxName)
	podConfig = &runtimeapi.PodSandboxConfig{
		Metadata:     framework.BuildPodSandboxMetadata(podSandboxName, uid, namespace, framework.DefaultAttempt),
		LogDirectory: podLogPath,
		PortMappings: []*runtimeapi.PortMapping{
			{
				ContainerPort: httpdContainerPort,
			},
		},
		Labels: framework.DefaultPodLabels,
		Linux: &runtimeapi.LinuxPodSandboxConfig{
			CgroupParent: common.GetCgroupParent(context.TODO(), c),
		},
	}

	return framework.RunPodSandbox(c, podConfig), podConfig, logDir
}

// createMultiContainerTestHttpdContainer creates an httpd container.
func createMultiContainerTestHttpdContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, prefix string,
	podID string, podConfig *runtimeapi.PodSandboxConfig,
) string {
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: httpdImage},
		Linux:    &runtimeapi.LinuxContainerConfig{},
		LogPath:  containerName + ".log",
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// createMultiContainerTestBusyboxContainer creates a busybox container.
func createMultiContainerTestBusyboxContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService,
	prefix string, podID string, podConfig *runtimeapi.PodSandboxConfig,
) string {
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.TestContext.TestImageList.DefaultTestContainerImage},
		Command:  []string{"sh", "-c", "echo " + defaultLog + "; sleep 1000"},
		LogPath:  containerName + ".log",
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}
