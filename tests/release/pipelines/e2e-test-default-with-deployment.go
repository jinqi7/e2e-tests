package pipelines

import (
	"fmt"
	"github.com/devfile/library/v2/pkg/util"
	//ecp "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appservice "github.com/redhat-appstudio/application-api/api/v1alpha1"
	"github.com/redhat-appstudio/e2e-tests/pkg/constants"
	"github.com/redhat-appstudio/e2e-tests/pkg/framework"
	"github.com/redhat-appstudio/e2e-tests/pkg/utils"
	"github.com/redhat-appstudio/e2e-tests/tests/release"
	releaseApi "github.com/redhat-appstudio/release-service/api/v1alpha1"
	//tektonutils "github.com/redhat-appstudio/release-service/tekton/utils"
	//corev1 "k8s.io/api/core/v1"
	"os"
)

var _ = framework.ReleaseSuiteDescribe("[HACBS-1199]test-release-e2e-with-deployment", Label("release-pipelines", "withDeployment", "HACBS"), func() {
	defer GinkgoRecover()
	// Initialize the tests controllers
	var fw *framework.Framework
	AfterEach(framework.ReportFailure(&fw))
	var err error
	//var compName string
	var devNamespace string

	var component *appservice.Component
	var releaseCR *releaseApi.Release
	var managedNamespace = utils.GetGeneratedNamespace("release-e2e-deploy-managed")

	scGitRevision := fmt.Sprintf("test-deployment-%s", util.GenerateRandomString(4))

	BeforeAll(func() {
		fw, err = framework.NewFramework("release-e2e-deploy")
		Expect(err).NotTo(HaveOccurred())
		devNamespace = fw.UserNamespace

		_, err = fw.AsKubeAdmin.CommonController.CreateTestNamespace(managedNamespace)
		Expect(err).NotTo(HaveOccurred(), "Error when creating managed namespace: %v", err)


		release.CreateAndLinkRegistryAuthSecret(*fw, devNamespace, managedNamespace)

		release.CreateECPolicy(*fw, managedNamespace, []string{"minimal"})


		_, err = fw.AsKubeAdmin.ReleaseController.CreateReleasePlan(sourceReleasePlanName, devNamespace, applicationNameDefault, managedNamespace, "")
		Expect(err).NotTo(HaveOccurred())

		_, err = fw.AsKubeAdmin.GitOpsController.CreatePocEnvironment(releaseEnvironment, managedNamespace)
		Expect(err).NotTo(HaveOccurred())

		release.CreateReleasePlanAdmission(*fw, devNamespace, managedNamespace, "pipelines/deploy-release/deploy-release.yaml", releaseEnvironment, nil)
		Expect(err).NotTo(HaveOccurred())

		release.CreateAndBindReleasePplSvcAccount(*fw, devNamespace, managedNamespace)

		_, err = fw.AsKubeAdmin.HasController.CreateApplication(applicationNameDefault, devNamespace)
		Expect(err).NotTo(HaveOccurred())

		component = release.TestCreateComponent(*fw, devNamespace, applicationNameDefault, componentName)

		workingDir, err := os.Getwd()
		if err != nil {
			GinkgoWriter.Printf(err.Error())
		}

		// Download copy-applications.sh script from release-utils repo
		scriptFileName := "copy-application.sh"
		args := []string{"https://raw.githubusercontent.com/hacbs-release/release-utils/main/copy-application.sh", "-o", scriptFileName}
		Expect(utils.ExecuteCommandInASpecificDirectory("curl", args, workingDir)).To(Succeed())
		defer os.Remove(fmt.Sprintf("%s/%s", workingDir, scriptFileName))

		args = []string{"775", "copy-application.sh"}
		Expect(utils.ExecuteCommandInASpecificDirectory("chmod", args, workingDir)).To(Succeed())
		// Copying application in dev namespace to managed namespace
		args = []string{managedNamespace, "-a", devNamespace + "/" + applicationNameDefault}
		Expect(utils.ExecuteCommandInASpecificDirectory("./copy-application.sh", args, workingDir)).To(Succeed())

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

		It("verifies that a build PipelineRun is created in dev namespace and succeeds", func() {
			Expect(fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "", 2, fw.AsKubeAdmin.TektonController)).To(Succeed())
		})

		It("tests an associated Release CR is created", func() {
			Eventually(func() error {
				releaseCR, err = fw.AsKubeAdmin.ReleaseController.GetFirstReleaseInNamespace(devNamespace)
				return err
			}, releaseCreationTimeout, defaultInterval).Should(Succeed())
		})

		It("verifies that Release PipelineRun is triggered", func() {
			Eventually(func() error {
				pr, err := fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseCR.GetName(), releaseCR.GetNamespace())
				if err != nil {
					GinkgoWriter.Printf("release pipelineRun for release '%s' in namespace '%s' not created yet: %+v\n", releaseCR.GetName(), releaseCR.GetNamespace(), err)
					return err
				}
				if !pr.HasStarted() {
					return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pr.GetNamespace(), pr.GetName())
				}
				return nil
			}, releasePipelineRunCreationTimeout, defaultInterval).Should(Succeed(), fmt.Sprintf("timed out waiting for a pipelinerun to start for a release %s/%s", releaseCR.GetName(), releaseCR.GetNamespace()))
		})

		It("verifies that Release PipelineRun should eventually succeed", func() {
			Eventually(func() error {
				pr, err := fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, releaseCR.GetName(), releaseCR.GetNamespace())
				Expect(err).ShouldNot(HaveOccurred())
				if !pr.IsDone() {
					return fmt.Errorf("release pipelinerun %s/%s did not finish yet", pr.GetNamespace(), pr.GetName())
				}
				Expect(utils.HasPipelineRunSucceeded(pr)).To(BeTrue(), fmt.Sprintf("release pipelinerun %s/%s did not succeed", pr.GetNamespace(), pr.GetName()))
				return nil
			}, releasePipelineRunCompletionTimeout, defaultInterval).Should(Succeed())
		})
		// phase Deployed happens before phase Released
		It("tests a Release CR reports that the deployment was successful", func() {
			Eventually(func() error {
				releaseCR, err := fw.AsKubeAdmin.ReleaseController.GetFirstReleaseInNamespace(devNamespace)
				if err != nil {
					return err
				}
				if !releaseCR.IsDeployed() {
					return fmt.Errorf("release %s/%s is not marked as deployed yet", releaseCR.GetNamespace(), releaseCR.GetName())
				}
				return nil
			}, releaseDeploymentTimeout, defaultInterval).Should(Succeed())
		})
		It("tests a Release CR is marked as successfully deployed", func() {
			Eventually(func() error {
				releaseCR, err := fw.AsKubeAdmin.ReleaseController.GetFirstReleaseInNamespace(devNamespace)
				if err != nil {
					return err
				}
				if !releaseCR.IsReleased() {
					return fmt.Errorf("release %s/%s is not marked as finished yet", releaseCR.GetNamespace(), releaseCR.GetName())
				}
				return nil
			}, releaseFinishedTimeout, defaultInterval).Should(Succeed())
		})
	})
})
