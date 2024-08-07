package pipelines

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/e2e-tests/pkg/clients/release"
	"github.com/konflux-ci/e2e-tests/pkg/constants"
	"github.com/konflux-ci/e2e-tests/pkg/framework"
	"github.com/konflux-ci/e2e-tests/pkg/utils"
	"github.com/konflux-ci/e2e-tests/pkg/utils/contract"
	releasecommon "github.com/konflux-ci/e2e-tests/tests/release"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = framework.ReleasePipelinesSuiteDescribe("[HACBS-1571]test-release-e2e-push-image-to-pyxis", Label("release-pipelines", "pushPyxis"), func() {
	defer GinkgoRecover()
	// Initialize the tests controllers
	var fw *framework.Framework
	AfterEach(framework.ReportFailure(&fw))
	var err error
	var devNamespace, managedNamespace, compName, additionalCompName string
	var avgControllerQueryTimeout = 5 * time.Minute

	var imageIDs []string
	var pyxisKeyDecoded, pyxisCertDecoded []byte
	var releasePR1, releasePR2 *pipeline.PipelineRun
	scGitRevision := fmt.Sprintf("test-pyxis-%s", util.GenerateRandomString(4))

	var snapshotPush *appservice.Snapshot
	var releaseCR1 *releaseApi.Release

	BeforeAll(func() {
		fw, err = framework.NewFramework(utils.GetGeneratedNamespace("push-pyxis"))
		Expect(err).NotTo(HaveOccurred())
		devNamespace = fw.UserNamespace
		managedNamespace = utils.GetGeneratedNamespace("push-pyxis-managed")

		_, err = fw.AsKubeAdmin.CommonController.CreateTestNamespace(managedNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating managedNamespace")

		sourceAuthJson := utils.GetEnv("QUAY_TOKEN", "")
		Expect(sourceAuthJson).ToNot(BeEmpty())


		keyPyxisStage := os.Getenv(constants.PYXIS_STAGE_KEY_ENV)
		Expect(keyPyxisStage).ToNot(BeEmpty())

		certPyxisStage := os.Getenv(constants.PYXIS_STAGE_CERT_ENV)
		Expect(certPyxisStage).ToNot(BeEmpty())

		// Create secret for the release registry repo "hacbs-release-tests".
		_, err = fw.AsKubeAdmin.CommonController.CreateRegistryAuthSecret(releasecommon.RedhatAppstudioUserSecret, managedNamespace, sourceAuthJson)
		Expect(err).ToNot(HaveOccurred())

		// Linking the build secret to the pipeline service account in dev namespace.
		err = fw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(devNamespace, releasecommon.HacbsReleaseTestsTokenSecret, constants.DefaultPipelineServiceAccount, true)
		Expect(err).ToNot(HaveOccurred())

		// Creating k8s secret to access Pyxis stage based on base64 decoded of key and cert
		pyxisKeyDecoded, err = base64.StdEncoding.DecodeString(string(keyPyxisStage))
		Expect(err).ToNot(HaveOccurred())

		pyxisCertDecoded, err = base64.StdEncoding.DecodeString(string(certPyxisStage))
		Expect(err).ToNot(HaveOccurred())

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pyxis",
				Namespace: managedNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"cert": pyxisCertDecoded,
				"key":  pyxisKeyDecoded,
			},
		}

		_, err = fw.AsKubeAdmin.CommonController.CreateSecret(managedNamespace, secret)
		Expect(err).ToNot(HaveOccurred())

		releasePublicKey := []byte(os.Getenv("RELEASE_PUBLIC_KEY"))

		Expect(fw.AsKubeAdmin.TektonController.CreateOrUpdateSigningSecret(
			releasePublicKey, "public-key", constants.TEKTON_CHAINS_NS)).To(Succeed())

		Expect(fw.AsKubeAdmin.TektonController.CreateOrUpdateSigningSecret(
			releasePublicKey, releasecommon.PublicSecretNameAuth, managedNamespace)).To(Succeed())

		defaultECP, err := fw.AsKubeAdmin.TektonController.GetEnterpriseContractPolicy("default", "enterprise-contract-service")
		Expect(err).NotTo(HaveOccurred())
		policy := contract.PolicySpecWithSourceConfig(defaultECP.Spec, ecp.SourceConfig{Include: []string{"@slsa3"}, Exclude: []string{"step_image_registries", "tasks.required_tasks_found:prefetch-dependencies"}})

		managedServiceAccount, err := fw.AsKubeAdmin.CommonController.CreateServiceAccount(releasecommon.ReleasePipelineServiceAccountDefault, managedNamespace, releasecommon.ManagednamespaceSecret, nil)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePipelineRoleBindingForServiceAccount(managedNamespace, managedServiceAccount)
		Expect(err).NotTo(HaveOccurred())

		err = fw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.RedhatAppstudioUserSecret, releasecommon.ReleasePipelineServiceAccountDefault, true)
		Expect(err).ToNot(HaveOccurred())

		compName = releasecommon.ComponentName
		additionalCompName = releasecommon.AdditionalComponentName

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(releasecommon.SourceReleasePlanName, devNamespace, releasecommon.ApplicationNameDefault, managedNamespace, "true", nil, nil)
		Expect(err).NotTo(HaveOccurred())

		data, err := json.Marshal(map[string]interface{}{
			"mapping": map[string]interface{}{
				"components": []map[string]interface{}{
					{
						"name":       compName,
						"repository": "quay.io/" + utils.GetQuayIOOrganization() + "/dcmetromap",
					},
					{
						"name":       additionalCompName,
						"repository": "quay.io/" + utils.GetQuayIOOrganization() + "/simplepython",
					},
				},
				"defaults": map[string]interface{}{
					"tags": []string{
						"latest",
					},
				},
			},
			"pyxis": map[string]interface{}{
				"server": "stage",
				"secret": "pyxis",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlanAdmission(releasecommon.TargetReleasePlanAdmissionName, managedNamespace, "", devNamespace, releasecommon.ReleaseStrategyPolicyDefault, releasecommon.ReleasePipelineServiceAccountDefault, []string{releasecommon.ApplicationNameDefault}, true, &tektonutils.PipelineRef{
			Resolver: "git",
			Params: []tektonutils.Param{
				{Name: "url", Value: releasecommon.RelSvcCatalogURL},
				{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
				{Name: "pathInRepo", Value: "pipelines/rh-push-to-external-registry/rh-push-to-external-registry.yaml"},
			},
		}, &runtime.RawExtension{
			Raw: data,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.TektonController.CreateEnterpriseContractPolicy(releasecommon.ReleaseStrategyPolicyDefault, managedNamespace, policy)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.TektonController.CreatePVCInAccessMode(releasecommon.ReleasePvcName, managedNamespace, corev1.ReadWriteOnce)
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.CommonController.CreateRole("role-release-service-account", managedNamespace, map[string][]string{
			"apiGroupsList": {""},
			"roleResources": {"secrets"},
			"roleVerbs":     {"get", "list", "watch"},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.CommonController.CreateRoleBinding("role-release-service-account-binding", managedNamespace, "ServiceAccount", releasecommon.ReleasePipelineServiceAccountDefault, managedNamespace, "Role", "role-release-service-account", "rbac.authorization.k8s.io")
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.HasController.CreateApplication(releasecommon.ApplicationNameDefault, devNamespace)
		Expect(err).NotTo(HaveOccurred())

		sampleImage := "quay.io/redhat-appstudio-qe/dcmetromap@sha256:544259be8bcd9e6a2066224b805d854d863064c9b64fa3a87bfcd03f5b0f28e6"
		sampleImage2 := "quay.io/redhat-appstudio-qe/simplepython@sha256:87ebb63d7b7ba0196093195592c03f5f6e23db9b889c7325e5e081feb16755a1"
		snapshotPush, err = fw.AsKubeAdmin.IntegrationController.CreateSnapshotWithImageAndSource(releasecommon.ComponentName, releasecommon.ApplicationNameDefault, devNamespace, sampleImage, releasecommon.GitSourceComponentUrl, releasecommon.DcMetroMapGitRevision, releasecommon.AdditionalComponentName, sampleImage2, releasecommon.AdditionalGitSourceComponentUrl, releasecommon.SimplePythonGitRevision)
		GinkgoWriter.Println("snapshotPush.Name: %s", snapshotPush.GetName())
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterAll(func() {
		err = fw.AsKubeAdmin.CommonController.Github.DeleteRef(constants.StrategyConfigsRepo, scGitRevision)
		if err != nil {
			Expect(err.Error()).To(ContainSubstring("Reference does not exist"))
		}
		if !CurrentSpecReport().Failed() {
			Expect(fw.AsKubeAdmin.CommonController.DeleteNamespace(managedNamespace)).NotTo(HaveOccurred())
			Expect(fw.SandboxController.DeleteUserSignup(fw.UserName)).To(BeTrue())
		}
	})

	var _ = Describe("Post-release verification", func() {
		/*

		It("verifies that Component 1 can be created and build PipelineRun is created for it in dev namespace and succeeds", func() {
			component1, err = fw.AsKubeAdmin.HasController.CreateComponent(componentObj1, devNamespace, "", "", releasecommon.ApplicationNameDefault, true, constants.DefaultDockerBuildPipelineBundle)
			Expect(err).NotTo(HaveOccurred())
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component1, "",
				fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
		})

		It("verifies that Component 2 can be created and build PipelineRun is created for it in dev namespace and succeeds", func() {
			component2, err = fw.AsKubeAdmin.HasController.CreateComponent(componentObj2, devNamespace, "", "", releasecommon.ApplicationNameDefault, true, constants.DefaultDockerBuildPipelineBundle)
			Expect(err).NotTo(HaveOccurred())
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component2, "",
				fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, nil)).To(Succeed())
		})

		It("tests that Snapshot is created for each Component", func() {
			Eventually(func() error {
				snapshot1, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", "", component1.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Snapshot for component %s/%s: %v\n", component1.GetNamespace(), component1.GetName(), err)
					return err
				}
				snapshot2, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", "", component2.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Snapshot for component %s/%s: %v\n", component2.GetNamespace(), component2.GetName(), err)
					return err
				}
				return nil
			}, 5*time.Minute, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Snapshots to be created in %s namespace", devNamespace)
		})
*/
		It("tests that Release CR is created for the Snapshot", func() {
			Eventually(func() error {
				releaseCR1, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", snapshotPush.GetName(), devNamespace)
				if err != nil {
					GinkgoWriter.Printf("cannot get the Release CR for snapshot %s/%s: %v\n", snapshotPush.GetNamespace(), releasecommon.ComponentName, err)
					return err
				}
				return nil
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Release CRs to be created in %s namespace", devNamespace)
		})

		It("verifies a release PipelineRun is started and succeeded in managed namespace", func() {
			releasePR1, err = fw.AsKubeAdmin.ReleaseController.WaitForReleasePipelineToGetStarted(releaseCR1, managedNamespace)
			Expect(err).NotTo(HaveOccurred())

			Expect(fw.AsKubeAdmin.ReleaseController.WaitForReleasePipelineToBeFinished(releaseCR1, managedNamespace)).To(Succeed(), fmt.Sprintf("Error when waiting for a release pipelinerun for release %s/%s to finish", releaseCR1.GetNamespace(), releaseCR1.GetName()))
		})

		It("validate the result of task create-pyxis-image contains image ids", func() {
			Eventually(func() []string {
				trReleaseLogs, err := fw.AsKubeAdmin.TektonController.GetTaskRunLogs(releasePR1.GetName(), "create-pyxis-image", releasePR1.GetNamespace())
				Expect(err).NotTo(HaveOccurred())

				trAdditionalReleaseLogs, err := fw.AsKubeAdmin.TektonController.GetTaskRunLogs(releasePR2.GetName(), "create-pyxis-image", releasePR2.GetNamespace())
				Expect(err).NotTo(HaveOccurred())

				trReleaseImageIDs, err := fw.AsKubeAdmin.ReleaseController.GetPyxisImageIDsFromCreatePyxisImageTaskLogs(trReleaseLogs)
				Expect(err).NotTo(HaveOccurred())
				trAdditionalReleaseIDs, err := fw.AsKubeAdmin.ReleaseController.GetPyxisImageIDsFromCreatePyxisImageTaskLogs(trAdditionalReleaseLogs)
				Expect(err).NotTo(HaveOccurred())

				Expect(trReleaseImageIDs).NotTo(BeEmpty(), fmt.Sprintf("Invalid ImageID in results of task create-pyxis-image. TaskRun log: %+s", trReleaseLogs))
				Expect(trAdditionalReleaseIDs).ToNot(BeEmpty(), fmt.Sprintf("Invalid ImageID in results of task create-pyxis-image. TaskRun log: %+s", trAdditionalReleaseLogs))

				Expect(trReleaseImageIDs).ToNot(HaveLen(len(trAdditionalReleaseIDs)), "the number of image IDs should not be the same in both taskrun results. (%+v vs. %+v)", trReleaseImageIDs, trAdditionalReleaseIDs)

				if len(trReleaseImageIDs) > len(trAdditionalReleaseIDs) {
					imageIDs = trReleaseImageIDs
				} else {
					imageIDs = trAdditionalReleaseIDs
				}

				return imageIDs
			}, avgControllerQueryTimeout, releasecommon.DefaultInterval).Should(HaveLen(2))
		})

		It("tests that Release CR has completed", func() {
			Eventually(func() error {
				var errMsg string
				for _, cr := range []*releaseApi.Release{releaseCR1} {
					cr, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", cr.Spec.Snapshot, devNamespace)
					Expect(err).ShouldNot(HaveOccurred())
					if !cr.IsReleased() {
						errMsg += fmt.Sprintf("release %s/%s is not marked as finished yet", cr.GetNamespace(), cr.GetName())
					}
				}
				if len(errMsg) > 1 {
					return fmt.Errorf(errMsg)
				}
				return nil
			}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed(), "timed out waiting for Release CRs to be created in %s namespace", devNamespace)
		})

		It("validates that imageIds from task create-pyxis-image exist in Pyxis.", func() {
			for _, imageID := range imageIDs {
				Eventually(func() error {
					body, err := fw.AsKubeAdmin.ReleaseController.GetPyxisImageByImageID(releasecommon.PyxisStageImagesApiEndpoint, imageID,
						[]byte(pyxisCertDecoded), []byte(pyxisKeyDecoded))
					Expect(err).NotTo(HaveOccurred(), "failed to get response body")

					sbomImage := release.Image{}
					Expect(json.Unmarshal(body, &sbomImage)).To(Succeed(), "failed to unmarshal body content: %s", string(body))

					return nil
				}, releasecommon.ReleaseCreationTimeout, releasecommon.DefaultInterval).Should(Succeed())
			}
		})
	})
})
