package github

import (
	"context"
	"fmt"

//	"github.com/google/go-github/v44/github"
	"github.com/google/go-github/v60/github"
	. "github.com/onsi/ginkgo/v2"
)

//https://github.com/google/go-github/blob/master/github/repos.go
func (g *Github) CheckIfForkExist(repositoryName, ownerName string) (bool, error) {
	opt := &github.RepositoryListByUserOptions{Type: "owner",}
	repos, _, err := g.client.Repositories.ListByUser(context.Background(), ownerName, opt)
	if err != nil {
		GinkgoWriter.Printf("error when listing forks by user %s : %v\n", ownerName, err)
		return false, err
	}
	if repos != nil {
		for _, repo := range repos {
			GinkgoWriter.Printf("Fork for owner %s is found in repo %s \n", ownerName, repo.FullName)
		}
		return true, nil
	}
	return false, nil
}

func (g *Github) CreateFork(repositoryName string) error {
	opt := &github.RepositoryCreateForkOptions{Organization: g.organization, Name: repositoryName, DefaultBranchOnly: true}
	repo, _, err := g.client.Repositories.CreateFork(context.Background(), g.organization, repositoryName, opt)
	if err != nil {
		GinkgoWriter.Printf("error when creating fork for repository %s : %v\n", repositoryName, err)
		return err
	}
	GinkgoWriter.Printf("Created a fork for repository %s\n", repo.Name)
	return nil
}

//https://github.com/google/go-github/blob/master/github/repos_merging.go#L62
func (g *Github) MergeUpstream(owner, repositoryName, baseBranch string) (bool, error) {
	input := &github.RepoMergeUpstreamRequest{Branch: &baseBranch}
	result, _, err := g.client.Repositories.MergeUpstream(context.Background(), owner, repositoryName, input)
	if err != nil {
		GinkgoWriter.Printf("error when syncing with upstream %s : %v\n", repositoryName, err)
		return false, err
	}
	GinkgoWriter.Printf("Merged with upstream repository %s\n", result.MergeType)
	return true, nil
}

//https://github.com/google/go-github/blob/master/github/repos_releases.go
func (g *Github) CheckIfReleaseExist(owner, repositoryName, tagName string) bool {
	opt := &github.ListOptions{Page: 2}
	releases, _, err := g.client.Repositories.ListReleases(context.Background(), owner, repositoryName, opt)
	if err != nil {
		GinkgoWriter.Printf("error when listing Releases %s : %v\n", repositoryName, err)
		return false
	}
	//https://github.com/google/go-github/blob/master/github/repos_releases.go#L21
	for _, release := range releases {
		releaseTagName :=  release.TagName
		if tagName == *releaseTagName {
			return true
		}
	}
	GinkgoWriter.Printf("Release tag %s is not found in repository %s \n", tagName, repositoryName)
	return false
}

func (g *Github) CheckIfRepositoryExist(repository string) bool {
	_, resp, err := g.client.Repositories.Get(context.Background(), g.organization, repository)
	if err != nil {
		GinkgoWriter.Printf("error when sending request to Github API: %v\n", err)
		return false
	}
	GinkgoWriter.Printf("repository %s status request to github: %d\n", repository, resp.StatusCode)
	return resp.StatusCode == 200
}

func (g *Github) CreateFile(repository, pathToFile, fileContent, branchName string) (*github.RepositoryContentResponse, error) {
	opts := &github.RepositoryContentFileOptions{
		Message: github.String("e2e test commit message"),
		Content: []byte(fileContent),
		Branch:  github.String(branchName),
	}

	file, _, err := g.client.Repositories.CreateFile(context.Background(), g.organization, repository, pathToFile, opts)
	if err != nil {
		return nil, fmt.Errorf("error when creating file contents: %v", err)
	}

	return file, nil
}

func (g *Github) GetFile(repository, pathToFile, branchName string) (*github.RepositoryContent, error) {
	opts := &github.RepositoryContentGetOptions{}
	if branchName != "" {
		opts.Ref = fmt.Sprintf(HEADS, branchName)
	}
	file, _, _, err := g.client.Repositories.GetContents(context.Background(), g.organization, repository, pathToFile, opts)
	if err != nil {
		return nil, fmt.Errorf("error when listing file contents: %v", err)
	}

	return file, nil
}

func (g *Github) UpdateFile(repository, pathToFile, newContent, branchName, fileSHA string) (*github.RepositoryContentResponse, error) {
	opts := &github.RepositoryContentGetOptions{}
	if branchName != "" {
		opts.Ref = fmt.Sprintf(HEADS, branchName)
	}
	newFileContent := &github.RepositoryContentFileOptions{
		Message: github.String("e2e test commit message"),
		SHA:     github.String(fileSHA),
		Content: []byte(newContent),
		Branch:  github.String(branchName),
	}
	updatedFile, _, err := g.client.Repositories.UpdateFile(context.Background(), g.organization, repository, pathToFile, newFileContent)
	if err != nil {
		return nil, fmt.Errorf("error when updating a file on github: %v", err)
	}

	return updatedFile, nil
}

func (g *Github) DeleteFile(repository, pathToFile, branchName string) error {
	getOpts := &github.RepositoryContentGetOptions{}
	deleteOpts := &github.RepositoryContentFileOptions{}

	if branchName != "" {
		getOpts.Ref = fmt.Sprintf(HEADS, branchName)
		deleteOpts.Branch = github.String(branchName)
	}
	file, _, _, err := g.client.Repositories.GetContents(context.Background(), g.organization, repository, pathToFile, getOpts)
	if err != nil {
		return fmt.Errorf("error when listing file contents on github: %v", err)
	}

	deleteOpts = &github.RepositoryContentFileOptions{
		Message: github.String("delete test files"),
		SHA:     github.String(file.GetSHA()),
	}

	_, _, err = g.client.Repositories.DeleteFile(context.Background(), g.organization, repository, pathToFile, deleteOpts)
	if err != nil {
		return fmt.Errorf("error when deleting file on github: %v", err)
	}
	return nil
}

func (g *Github) GetAllRepositories() ([]*github.Repository, error) {

	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	var allRepos []*github.Repository
	for {
		repos, resp, err := g.client.Repositories.ListByOrg(context.Background(), g.organization, opt)
		if err != nil {
			return nil, err
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return allRepos, nil
}

func (g *Github) DeleteRepository(repository *github.Repository) error {
	GinkgoWriter.Printf("Deleting repository %s\n", *repository.Name)
	_, err := g.client.Repositories.Delete(context.Background(), g.organization, *repository.Name)
	if err != nil {
		return err
	}
	return nil
}
