package repos

import (
	"fmt"
	"os"
	"strings"

	"github.com/konflux-ci/e2e-tests/magefiles/rulesengine"
	"k8s.io/klog"
)

var ReleaseServiceCatalogCICatalog = rulesengine.RuleCatalog{ReleaseServiceCatalogCIPairedRule, ReleaseServiceCatalogCIRule}

var ReleaseServiceCatalogCIPairedRule = rulesengine.Rule{Name: "Release-service-catalog repo CI Workflow Paired Rule",
	Description: "Execute the Paired workflow for release-service-catalog repo in CI",
	Condition: rulesengine.All{
		rulesengine.ConditionFunc(isPaired),
		rulesengine.None{
			rulesengine.ConditionFunc(isRehearse),
		},
		&ReleaseServiceCatalogRepoSetDefaultSettingsRule,
		rulesengine.Any{&InfraDeploymentsPRPairingRule, rulesengine.None{&InfraDeploymentsPRPairingRule}},
		&PreflightInstallGinkgoRule,
		&InstallKonfluxRule,
	},
	Actions: []rulesengine.Action{rulesengine.ActionFunc(ExecuteReleaseCatalogPairedAction)},
}

var ReleaseServiceCatalogCIRule = rulesengine.Rule{Name: "Release-service-catalog repo CI Workflow Rule",
	Description: "Execute the full workflow for release-service-catalog repo in CI",
	Condition: rulesengine.All{
		rulesengine.Any{
			rulesengine.None{rulesengine.ConditionFunc(isPaired)},
			rulesengine.ConditionFunc(isRehearse),
		},
		&ReleaseServiceCatalogRepoSetDefaultSettingsRule,
		rulesengine.Any{&InfraDeploymentsPRPairingRule, rulesengine.None{&InfraDeploymentsPRPairingRule}},
		&PreflightInstallGinkgoRule,
		&InstallKonfluxRule,
	},
	Actions: []rulesengine.Action{rulesengine.ActionFunc(ExecuteReleaseCatalogAction)},
}

var ReleaseServiceCatalogRepoSetDefaultSettingsRule = rulesengine.Rule{Name: "General Required Settings for release-service-catalog repository jobs",
	Description: "relese-service-catalog jobs default rule",
	Condition: rulesengine.Any{
		IsReleaseServiceCatalogRepoPR,
	},
	Actions: []rulesengine.Action{rulesengine.ActionFunc(func(rctx *rulesengine.RuleCtx) error {
		rctx.LabelFilter = "release-service-catalog"
		klog.Info("setting 'release-service-catalog' test label")

		if rctx.DryRun {
			klog.Info("setting up env vars for deploying component image")
			return nil
		}
		rctx.ComponentEnvVarPrefix = "RELEASE_SERVICE"

		//This is env variable is specified for release service catalog
		os.Setenv(fmt.Sprintf("%s_CATALOG_URL", rctx.ComponentEnvVarPrefix), fmt.Sprintf("https://github.com/%s/%s", rctx.PrRemoteName, rctx.RepoName))
		os.Setenv(fmt.Sprintf("%s_CATALOG_REVISION", rctx.ComponentEnvVarPrefix), rctx.PrCommitSha)
/*
		if strings.Contains(rctx.JobName, "rehearse") || !rctx.IsPaired {
			return nil
		}
		if rctx.IsPaired {
			os.Setenv(fmt.Sprintf("%s_IMAGE_REPO", rctx.ComponentEnvVarPrefix),
				"quay.io/redhat-user-workloads/rhtap-release-2-tenant/release-service/release-service")
			pairedSha := getPairedCommitSha("release-service")
			if pairedSha != "" {
				os.Setenv(fmt.Sprintf("%s_IMAGE_TAG", rctx.ComponentEnvVarPrefix), fmt.Sprintf("on-pr-%s", pairedSha))
			}
			os.Setenv(fmt.Sprintf("%s_PR_OWNER", rctx.ComponentEnvVarPrefix), rctx.PrRemoteName)
			os.Setenv(fmt.Sprintf("%s_PR_SHA", rctx.ComponentEnvVarPrefix), pairedSha)
		}
*/
		return nil
	})},
}

var ReleaseCatalogTestPairedRule = rulesengine.Rule{Name: "Release Service Catalog PR paired Test Execution",
	Description: "Runs release catalog tests except for the fbc tests on release-service-catalog repo when PR paired and not a rehearsal job",
	Condition: rulesengine.All{
		IsReleaseServiceCatalogRepoPR,
		rulesengine.ConditionFunc(isPaired),
		rulesengine.None{
			rulesengine.ConditionFunc(isRehearse),
		},
	},
	Actions: []rulesengine.Action{rulesengine.ActionFunc(ExecuteReleasePairedAction)}}

var ReleaseCatalogTestRule = rulesengine.Rule{Name: "Release Service Catalog Test Execution",
	Description: "Runs all release catalog tests on release-service-catalog repo on PR/rehearsal jobs",
	Condition: rulesengine.All{
		IsReleaseServiceCatalogRepoPR,
		rulesengine.Any{
			rulesengine.None{rulesengine.ConditionFunc(isPaired)},
			rulesengine.ConditionFunc(isRehearse),
		},
	},
	Actions: []rulesengine.Action{rulesengine.ActionFunc(ExecuteReleaseCatalogAction)}}

var ReleaseServiceCatalogTestRulesCatalog = rulesengine.RuleCatalog{ReleaseCatalogTestRule, ReleaseCatalogTestPairedRule}

var IsReleaseServiceCatalogRepoPR = rulesengine.ConditionFunc(func(rctx *rulesengine.RuleCtx) (bool, error) {
	klog.Info("checking if repository is release-service-catalog")
	return rctx.RepoName == "release-service-catalog", nil
})

/*
func getPairedCommitSha(repoForPairing string) string {
	var pullRequests []gh.PullRequest

	url := fmt.Sprintf("https://api.github.com/repos/redhat-appstudio/%s/pulls?per_page=100", repoForPairing)
	if err := sendHttpRequestAndParseResponse(url, "GET", &pullRequests); err != nil {
		klog.Infof("cannot determine %s Github branches for author %s: %v. will stick with the redhat-appstudio/%s main branch for running tests", repoForPairing, pr.RemoteName, err, repoForPairing)
		return ""
	}

	for _, pull := range pullRequests {
		if pull.GetHead().GetRef() == pr.BranchName && pull.GetUser().GetLogin() == pr.RemoteName {
			return pull.GetHead().GetSHA()
		}
	}

	klog.Infof("cannot determine %s commit sha for author %s", repoForPairing, pr.RemoteName)
	return ""
}
*/

var isRehearse = func(rctx *rulesengine.RuleCtx) (bool, error) {

	return strings.Contains(rctx.JobName, "rehearse"), nil
}

var isPaired = func(rctx *rulesengine.RuleCtx) (bool, error) {
	return rctx.IsPaired, nil
}

/*
func ExecuteReleaseCatalogPairedAction(rctx *rulesengine.RuleCtx) error {
	os.Setenv(fmt.Sprintf("%s_IMAGE_REPO", rctx.ComponentEnvVarPrefix),
		"quay.io/redhat-user-workloads/rhtap-release-2-tenant/release-service/release-service")
	pairedSha := getPairedCommitSha("release-service")
	if pairedSha != "" {
		os.Setenv(fmt.Sprintf("%s_IMAGE_TAG", rctx.ComponentEnvVarPrefix), fmt.Sprintf("on-pr-%s", pairedSha))
	}
	os.Setenv(fmt.Sprintf("%s_PR_OWNER", rctx.ComponentEnvVarPrefix), rctx.PrRemoteName)
	os.Setenv(fmt.Sprintf("%s_PR_SHA", rctx.ComponentEnvVarPrefix), pairedSha)

	rctx.LabelFilter = "release-pipelines && !fbc-tests"
	return ExecuteTestAction(rctx)
}

func ExecuteReleaseCatalogAction(rctx *rulesengine.RuleCtx) error {
	rctx.LabelFilter = "release-pipelines"
	return ExecuteTestAction(rctx)
}
*/
