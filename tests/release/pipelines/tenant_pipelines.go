package pipelines

import (
	"encoding/json"
	"fmt"

	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
	"k8s.io/apimachinery/pkg/runtime"

	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/clients/has"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	"github.com/konflux-ci/e2e-tests/pkg/utils/contract"
	"github.com/konflux-ci/e2e-tests/pkg/utils/tekton"
	releasecommon "github.com/konflux-ci/e2e-tests/tests/release"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

var _ = framework.ReleaseServiceSuiteDescribe("Release service tenant pipeline", Label("release-pipelines", "tenant"), func() {
	defer GinkgoRecover()

	var fw *framework.Framework
	AfterEach(framework.ReportFailure(&fw))
	var err error
	var compName string
	var devNamespace string
	var releasedImagePushRepo = "quay.io/redhat-appstudio-qe/dcmetromap"

	var component *appservice.Component
	var releaseCR *releaseApi.Release

	BeforeAll(func() {
		// Initialize the tests controllers
		fw, err = framework.NewFramework(utils.GetGeneratedNamespace("tenant-dev"))
		Expect(err).NotTo(HaveOccurred())
		devNamespace = fw.UserNamespace

		sourceAuthJson := utils.GetEnv("QUAY_TOKEN", "")
		Expect(sourceAuthJson).ToNot(BeEmpty())

		// Linking the build secret to the pipeline service account in dev namespace.
		err = fw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(devNamespace, releasecommon.HacbsReleaseTestsTokenSecret, constants.DefaultPipelineServiceAccount, true)
		Expect(err).ToNot(HaveOccurred())

		// using cdq since git ref is not known
		var componentDetected appservice.ComponentDetectionDescription
		cdq, err := fw.AsKubeAdmin.HasController.CreateComponentDetectionQuery(releasecommon.ComponentName, devNamespace, releasecommon.GitSourceComponentUrl, "", "", "", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(cdq.Status.ComponentDetected).To(HaveLen(1), "Expected length of the detected Components was not 1")

		for _, compDetected := range cdq.Status.ComponentDetected {
			compName = compDetected.ComponentStub.ComponentName
			componentDetected = compDetected
		}

		_, err = fw.AsKubeAdmin.HasController.CreateApplication(releasecommon.ApplicationNameDefault, devNamespace)
		Expect(err).NotTo(HaveOccurred())

		component, err = fw.AsKubeAdmin.HasController.CreateComponent(componentDetected.ComponentStub, devNamespace, "", "", releasecommon.ApplicationNameDefault, false, map[string]string{})
		Expect(err).NotTo(HaveOccurred())

		data, err := json.Marshal(map[string]interface{}{
			"mapping": map[string]interface{}{
				"components": []map[string]interface{}{
					{
						"component":  compName,
						"repository": releasedImagePushRepo,
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		tenantPipeline := &tektonutils.ParameterizedPipeline{}
		tenantPipeline.PipelineRef = tektonutils.PipelineRef{
			Resolver: "git",
			Params: []tektonutils.Param{
				{Name: "url", Value: releasecommon.RelSvcCatalogURL},
				{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
				{Name: "pathInRepo", Value: "pipelines/e2e/e2e.yaml"},
			},
		}

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(releasecommon.SourceReleasePlanName, devNamespace, releasecommon.ApplicationNameDefault, "", "", &runtime.RawExtension{
                        Raw: data,
                }, tenantPipeline)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.TektonController.CreatePVCInAccessMode(releasecommon.ReleasePvcName, devNamespace, corev1.ReadWriteOnce)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if !CurrentSpecReport().Failed() {
			Expect(fw.AsKubeAdmin.CommonController.DeleteNamespace(devNamespace)).NotTo(HaveOccurred())
			Expect(fw.SandboxController.DeleteUserSignup(fw.UserName)).To(BeTrue())
		}
	})

	var _ = Describe("Post-release verification", func() {
		It("verifies that a build PipelineRun is created in dev namespace and succeeds", func() {
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "",
				fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
		})

		It("verifies that a Release CR should have been created in the dev namespace", func() {
			Eventually(func() error {
				releaseCR, err = fw.AsKubeAdmin.ReleaseController.GetFirstReleaseInNamespace(devNamespace)
				return err
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed())
		})

		It("verifies that Release PipelineRun is triggered", func() {
			Expect(fw.AsKubeAdmin.ReleaseController.WaitForReleasePipelineToBeFinished(releaseCR, devNamespace)).To(Succeed(), fmt.Sprintf("Error when waiting for a release pipelinerun for release %s/%s to finish", releaseCR.GetNamespace(), releaseCR.GetName()))
		})

		It("verifies that a Release is marked as succeeded.", func() {
			Eventually(func() error {
				releaseCR, err = fw.AsKubeAdmin.ReleaseController.GetFirstReleaseInNamespace(devNamespace)
				if err != nil {
					return err
				}
				if !releaseCR.IsReleased() {
					return fmt.Errorf("release %s/%s is not marked as finished yet", releaseCR.GetNamespace(), releaseCR.GetName())
				}
				return nil
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed())
		})
	})
})
