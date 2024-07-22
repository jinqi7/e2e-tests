package engine

import (
	"github.com/konflux-ci/e2e-tests/magefiles/rulesengine"
	"github.com/konflux-ci/e2e-tests/magefiles/rulesengine/repos"
)

var MageEngine = rulesengine.RuleEngine{
	"tests": {
		"e2e-repo":          repos.E2ETestRulesCatalog,
		"infra-deployments": repos.InfraDeploymentsTestRulesCatalog,
		"release-pipelines": repos.ReleaseCatalogTestRulesCatalog,
	},
	"demo": {
		"local-workflow": repos.DemoCatalog,
	},
}
