/*
Copyright 2016 The Kubernetes Authors.

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

package lgtm

import (
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/github/fakegithub"
	"k8s.io/test-infra/prow/plugins"
	"k8s.io/test-infra/prow/repoowners"
)

type fakeOwnersClient struct {
	approvers map[string]sets.String
	reviewers map[string]sets.String
}

var _ repoowners.Interface = &fakeOwnersClient{}

func (f *fakeOwnersClient) LoadRepoAliases(org, repo, base string) (repoowners.RepoAliases, error) {
	return nil, nil
}

func (f *fakeOwnersClient) LoadRepoOwners(org, repo, base string) (repoowners.RepoOwnerInterface, error) {
	return &fakeRepoOwners{approvers: f.approvers, reviewers: f.reviewers}, nil
}

type fakeRepoOwners struct {
	approvers map[string]sets.String
	reviewers map[string]sets.String
}

type fakePruner struct {
	GithubClient  *fakegithub.FakeClient
	IssueComments []github.IssueComment
}

func (fp *fakePruner) PruneComments(shouldPrune func(github.IssueComment) bool) {
	for _, comment := range fp.IssueComments {
		if shouldPrune(comment) {
			fp.GithubClient.IssueCommentsDeleted = append(fp.GithubClient.IssueCommentsDeleted, comment.Body)
		}
	}
}

var _ repoowners.RepoOwnerInterface = &fakeRepoOwners{}

func (f *fakeRepoOwners) FindApproverOwnersForFile(path string) string  { return "" }
func (f *fakeRepoOwners) FindReviewersOwnersForFile(path string) string { return "" }
func (f *fakeRepoOwners) FindLabelsForFile(path string) sets.String     { return nil }
func (f *fakeRepoOwners) IsNoParentOwners(path string) bool             { return false }
func (f *fakeRepoOwners) LeafApprovers(path string) sets.String         { return nil }
func (f *fakeRepoOwners) Approvers(path string) sets.String             { return f.approvers[path] }
func (f *fakeRepoOwners) LeafReviewers(path string) sets.String         { return nil }
func (f *fakeRepoOwners) Reviewers(path string) sets.String             { return f.reviewers[path] }
func (f *fakeRepoOwners) RequiredReviewers(path string) sets.String     { return nil }

var approvers = map[string]sets.String{
	"doc/README.md": {
		"cjwagner": {},
		"jessica":  {},
	},
}

var reviewers = map[string]sets.String{
	"doc/README.md": {
		"alice": {},
		"bob":   {},
		"mark":  {},
		"sam":   {},
	},
}

func TestLGTMComment(t *testing.T) {
	var testcases = []struct {
		name          string
		body          string
		commenter     string
		hasLGTM       bool
		shouldToggle  bool
		shouldComment bool
		shouldAssign  bool
		skipCollab    bool
		storeTreeHash bool
	}{
		{
			name:         "non-lgtm comment",
			body:         "uh oh",
			commenter:    "o",
			hasLGTM:      false,
			shouldToggle: false,
		},
		{
			name:          "lgtm comment by reviewer, no lgtm on pr",
			body:          "/lgtm",
			commenter:     "reviewer1",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
		},
		{
			name:          "LGTM comment by reviewer, no lgtm on pr",
			body:          "/LGTM",
			commenter:     "reviewer1",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
		},
		{
			name:         "lgtm comment by reviewer, lgtm on pr",
			body:         "/lgtm",
			commenter:    "reviewer1",
			hasLGTM:      true,
			shouldToggle: false,
		},
		{
			name:          "lgtm comment by author",
			body:          "/lgtm",
			commenter:     "author",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: true,
		},
		{
			name:          "lgtm cancel by author",
			body:          "/lgtm cancel",
			commenter:     "author",
			hasLGTM:       true,
			shouldToggle:  true,
			shouldAssign:  false,
			shouldComment: false,
		},
		{
			name:          "lgtm comment by non-reviewer",
			body:          "/lgtm",
			commenter:     "o",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			shouldAssign:  true,
		},
		{
			name:          "lgtm comment by non-reviewer, with trailing space",
			body:          "/lgtm ",
			commenter:     "o",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			shouldAssign:  true,
		},
		{
			name:          "lgtm comment by non-reviewer, with no-issue",
			body:          "/lgtm no-issue",
			commenter:     "o",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			shouldAssign:  true,
		},
		{
			name:          "lgtm comment by non-reviewer, with no-issue and trailing space",
			body:          "/lgtm no-issue \r",
			commenter:     "o",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			shouldAssign:  true,
		},
		{
			name:          "lgtm comment by rando",
			body:          "/lgtm",
			commenter:     "not-in-the-org",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: true,
			shouldAssign:  false,
		},
		{
			name:          "lgtm cancel by non-reviewer",
			body:          "/lgtm cancel",
			commenter:     "o",
			hasLGTM:       true,
			shouldToggle:  true,
			shouldComment: false,
			shouldAssign:  true,
		},
		{
			name:          "lgtm cancel by rando",
			body:          "/lgtm cancel",
			commenter:     "not-in-the-org",
			hasLGTM:       true,
			shouldToggle:  false,
			shouldComment: true,
			shouldAssign:  false,
		},
		{
			name:         "lgtm cancel comment by reviewer",
			body:         "/lgtm cancel",
			commenter:    "reviewer1",
			hasLGTM:      true,
			shouldToggle: true,
		},
		{
			name:         "lgtm cancel comment by reviewer, with trailing space",
			body:         "/lgtm cancel \r",
			commenter:    "reviewer1",
			hasLGTM:      true,
			shouldToggle: true,
		},
		{
			name:         "lgtm cancel comment by reviewer, no lgtm",
			body:         "/lgtm cancel",
			commenter:    "reviewer1",
			hasLGTM:      false,
			shouldToggle: false,
		},
		{
			name:          "lgtm comment, based off OWNERS only",
			body:          "/lgtm",
			commenter:     "sam",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			skipCollab:    true,
		},
	}
	SHA := "0bd3ed50c88cd53a09316bf7a298f900e9371652"
	for _, tc := range testcases {
		t.Logf("Running scenario %q", tc.name)
		fc := &fakegithub.FakeClient{
			IssueComments: make(map[int][]github.IssueComment),
			PullRequests: map[int]*github.PullRequest{
				5: {
					Base: github.PullRequestBranch{
						Ref: "master",
					},
					MergeSHA: &SHA,
				},
			},
			PullRequestChanges: map[int][]github.PullRequestChange{
				5: {
					{Filename: "doc/README.md"},
				},
			},
		}
		e := &github.GenericCommentEvent{
			Action:      github.GenericCommentActionCreated,
			IssueState:  "open",
			IsPR:        true,
			Body:        tc.body,
			User:        github.User{Login: tc.commenter},
			IssueAuthor: github.User{Login: "author"},
			Number:      5,
			Assignees:   []github.User{{Login: "reviewer1"}, {Login: "reviewer2"}},
			Repo:        github.Repo{Owner: github.User{Login: "org"}, Name: "repo"},
			HTMLURL:     "<url>",
		}
		if tc.hasLGTM {
			fc.LabelsAdded = []string{"org/repo#5:" + LGTMLabel}
		}
		oc := &fakeOwnersClient{approvers: approvers, reviewers: reviewers}
		pc := &plugins.Configuration{}
		if tc.skipCollab {
			pc.Owners.SkipCollaborators = []string{"org/repo"}
		}
		pc.Lgtm = append(pc.Lgtm, plugins.Lgtm{
			Repos:         []string{"org/repo"},
			StoreTreeHash: true,
		})
		fp := &fakePruner{
			GithubClient:  fc,
			IssueComments: fc.IssueComments[5],
		}
		if err := handleGenericComment(fc, pc, oc, logrus.WithField("plugin", PluginName), fp, *e); err != nil {
			t.Errorf("didn't expect error from lgtmComment: %v", err)
			continue
		}
		if tc.shouldAssign {
			found := false
			for _, a := range fc.AssigneesAdded {
				if a == fmt.Sprintf("%s/%s#%d:%s", "org", "repo", 5, tc.commenter) {
					found = true
					break
				}
			}
			if !found || len(fc.AssigneesAdded) != 1 {
				t.Errorf("should have assigned %s but added assignees are %s", tc.commenter, fc.AssigneesAdded)
			}
		} else if len(fc.AssigneesAdded) != 0 {
			t.Errorf("should not have assigned anyone but assigned %s", fc.AssigneesAdded)
		}
		if tc.shouldToggle {
			if tc.hasLGTM {
				if len(fc.LabelsRemoved) == 0 {
					t.Errorf("should have removed LGTM.")
				} else if len(fc.LabelsAdded) > 1 {
					t.Errorf("should not have added LGTM.")
				}
			} else {
				if len(fc.LabelsAdded) == 0 {
					t.Errorf("should have added LGTM.")
				} else if len(fc.LabelsRemoved) > 0 {
					t.Errorf("should not have removed LGTM.")
				}
			}
		} else if len(fc.LabelsRemoved) > 0 {
			t.Errorf("should not have removed LGTM.")
		} else if (tc.hasLGTM && len(fc.LabelsAdded) > 1) || (!tc.hasLGTM && len(fc.LabelsAdded) > 0) {
			t.Errorf("should not have added LGTM.")
		}
		if tc.shouldComment && len(fc.IssueComments[5]) != 1 {
			t.Errorf("should have commented.")
		} else if !tc.shouldComment && len(fc.IssueComments[5]) != 0 {
			t.Errorf("should not have commented.")
		}
	}
}

func TestLGTMCommentWithLGTMNoti(t *testing.T) {
	var testcases = []struct {
		name         string
		body         string
		commenter    string
		shouldDelete bool
	}{
		{
			name:         "non-lgtm comment",
			body:         "uh oh",
			commenter:    "o",
			shouldDelete: false,
		},
		{
			name:         "lgtm comment by reviewer, no lgtm on pr",
			body:         "/lgtm",
			commenter:    "reviewer1",
			shouldDelete: true,
		},
		{
			name:         "LGTM comment by reviewer, no lgtm on pr",
			body:         "/LGTM",
			commenter:    "reviewer1",
			shouldDelete: true,
		},
		{
			name:         "lgtm comment by author",
			body:         "/lgtm",
			commenter:    "author",
			shouldDelete: false,
		},
		{
			name:         "lgtm comment by non-reviewer",
			body:         "/lgtm",
			commenter:    "o",
			shouldDelete: true,
		},
		{
			name:         "lgtm comment by non-reviewer, with trailing space",
			body:         "/lgtm ",
			commenter:    "o",
			shouldDelete: true,
		},
		{
			name:         "lgtm comment by non-reviewer, with no-issue",
			body:         "/lgtm no-issue",
			commenter:    "o",
			shouldDelete: true,
		},
		{
			name:         "lgtm comment by non-reviewer, with no-issue and trailing space",
			body:         "/lgtm no-issue \r",
			commenter:    "o",
			shouldDelete: true,
		},
		{
			name:         "lgtm comment by rando",
			body:         "/lgtm",
			commenter:    "not-in-the-org",
			shouldDelete: false,
		},
		{
			name:         "lgtm cancel comment by reviewer, no lgtm",
			body:         "/lgtm cancel",
			commenter:    "reviewer1",
			shouldDelete: false,
		},
	}
	SHA := "0bd3ed50c88cd53a09316bf7a298f900e9371652"
	for _, tc := range testcases {
		fc := &fakegithub.FakeClient{
			IssueComments: make(map[int][]github.IssueComment),
			PullRequests: map[int]*github.PullRequest{
				5: {
					MergeSHA: &SHA,
				},
			},
		}
		e := &github.GenericCommentEvent{
			Action:      github.GenericCommentActionCreated,
			IssueState:  "open",
			IsPR:        true,
			Body:        tc.body,
			User:        github.User{Login: tc.commenter},
			IssueAuthor: github.User{Login: "author"},
			Number:      5,
			Assignees:   []github.User{{Login: "reviewer1"}, {Login: "reviewer2"}},
			Repo:        github.Repo{Owner: github.User{Login: "org"}, Name: "repo"},
			HTMLURL:     "<url>",
		}
		botName, err := fc.BotName()
		if err != nil {
			t.Fatalf("For case %s, could not get Bot nam", tc.name)
		}
		ic := github.IssueComment{
			User: github.User{
				Login: botName,
			},
			Body: removeLGTMLabelNoti,
		}
		fc.IssueComments[5] = append(fc.IssueComments[5], ic)
		oc := &fakeOwnersClient{approvers: approvers, reviewers: reviewers}
		pc := &plugins.Configuration{}
		fp := &fakePruner{
			GithubClient:  fc,
			IssueComments: fc.IssueComments[5],
		}
		if err := handleGenericComment(fc, pc, oc, logrus.WithField("plugin", PluginName), fp, *e); err != nil {
			t.Errorf("For case %s, didn't expect error from lgtmComment: %v", tc.name, err)
			continue
		}
		deleted := false
		for _, body := range fc.IssueCommentsDeleted {
			if body == removeLGTMLabelNoti {
				deleted = true
				break
			}
		}
		if tc.shouldDelete {
			if !deleted {
				t.Errorf("For case %s, LGTM removed notification should have been deleted", tc.name)
			}
		} else {
			if deleted {
				t.Errorf("For case %s, LGTM removed notification should not have been deleted", tc.name)
			}
		}
	}
}

func TestLGTMFromApproveReview(t *testing.T) {
	var testcases = []struct {
		name          string
		state         github.ReviewState
		body          string
		reviewer      string
		hasLGTM       bool
		shouldToggle  bool
		shouldComment bool
		shouldAssign  bool
		storeTreeHash bool
	}{
		{
			name:          "Request changes review by reviewer, no lgtm on pr",
			state:         github.ReviewStateChangesRequested,
			reviewer:      "reviewer1",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldAssign:  false,
			shouldComment: false,
		},
		{
			name:         "Request changes review by reviewer, lgtm on pr",
			state:        github.ReviewStateChangesRequested,
			reviewer:     "reviewer1",
			hasLGTM:      true,
			shouldToggle: true,
			shouldAssign: false,
		},
		{
			name:          "Approve review by reviewer, no lgtm on pr",
			state:         github.ReviewStateApproved,
			reviewer:      "reviewer1",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			storeTreeHash: true,
		},
		{
			name:          "Approve review by reviewer, no lgtm on pr, do not store tree_hash",
			state:         github.ReviewStateApproved,
			reviewer:      "reviewer1",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: false,
		},
		{
			name:         "Approve review by reviewer, lgtm on pr",
			state:        github.ReviewStateApproved,
			reviewer:     "reviewer1",
			hasLGTM:      true,
			shouldToggle: false,
			shouldAssign: false,
		},
		{
			name:          "Approve review by non-reviewer, no lgtm on pr",
			state:         github.ReviewStateApproved,
			reviewer:      "o",
			hasLGTM:       false,
			shouldToggle:  true,
			shouldComment: true,
			shouldAssign:  true,
			storeTreeHash: true,
		},
		{
			name:          "Request changes review by non-reviewer, no lgtm on pr",
			state:         github.ReviewStateChangesRequested,
			reviewer:      "o",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: false,
			shouldAssign:  true,
		},
		{
			name:          "Approve review by rando",
			state:         github.ReviewStateApproved,
			reviewer:      "not-in-the-org",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: true,
			shouldAssign:  false,
		},
		{
			name:          "Comment review by issue author, no lgtm on pr",
			state:         github.ReviewStateCommented,
			reviewer:      "author",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: false,
			shouldAssign:  false,
		},
		{
			name:          "Comment body has /lgtm on Comment Review ",
			state:         github.ReviewStateCommented,
			reviewer:      "reviewer1",
			body:          "/lgtm",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: false,
			shouldAssign:  false,
		},
		{
			name:          "Comment body has /lgtm cancel on Approve Review",
			state:         github.ReviewStateApproved,
			reviewer:      "reviewer1",
			body:          "/lgtm cancel",
			hasLGTM:       false,
			shouldToggle:  false,
			shouldComment: false,
			shouldAssign:  false,
		},
	}
	SHA := "0bd3ed50c88cd53a09316bf7a298f900e9371652"
	for _, tc := range testcases {
		fc := &fakegithub.FakeClient{
			IssueComments: make(map[int][]github.IssueComment),
			LabelsAdded:   []string{},
			PullRequests: map[int]*github.PullRequest{
				5: {
					MergeSHA: &SHA,
				},
			},
		}
		e := &github.ReviewEvent{
			Review:      github.Review{Body: tc.body, State: tc.state, HTMLURL: "<url>", User: github.User{Login: tc.reviewer}},
			PullRequest: github.PullRequest{User: github.User{Login: "author"}, Assignees: []github.User{{Login: "reviewer1"}, {Login: "reviewer2"}}, Number: 5},
			Repo:        github.Repo{Owner: github.User{Login: "org"}, Name: "repo"},
		}
		if tc.hasLGTM {
			fc.LabelsAdded = append(fc.LabelsAdded, "org/repo#5:"+LGTMLabel)
		}
		oc := &fakeOwnersClient{approvers: approvers, reviewers: reviewers}
		pc := &plugins.Configuration{}
		pc.Lgtm = append(pc.Lgtm, plugins.Lgtm{
			Repos:         []string{"org/repo"},
			StoreTreeHash: tc.storeTreeHash,
		})
		fp := &fakePruner{
			GithubClient:  fc,
			IssueComments: fc.IssueComments[5],
		}
		if err := handlePullRequestReview(fc, pc, oc, logrus.WithField("plugin", PluginName), fp, *e); err != nil {
			t.Errorf("For case %s, didn't expect error from pull request review: %v", tc.name, err)
			continue
		}
		if tc.shouldAssign {
			found := false
			for _, a := range fc.AssigneesAdded {
				if a == fmt.Sprintf("%s/%s#%d:%s", "org", "repo", 5, tc.reviewer) {
					found = true
					break
				}
			}
			if !found || len(fc.AssigneesAdded) != 1 {
				t.Errorf("For case %s, should have assigned %s but added assignees are %s", tc.name, tc.reviewer, fc.AssigneesAdded)
			}
		} else if len(fc.AssigneesAdded) != 0 {
			t.Errorf("For case %s, should not have assigned anyone but assigned %s", tc.name, fc.AssigneesAdded)
		}
		if tc.shouldToggle {
			if tc.hasLGTM {
				if len(fc.LabelsRemoved) == 0 {
					t.Errorf("For case %s, should have removed LGTM.", tc.name)
				} else if len(fc.LabelsAdded) > 1 {
					t.Errorf("For case %s, should not have added LGTM.", tc.name)
				}
			} else {
				if len(fc.LabelsAdded) == 0 {
					t.Errorf("For case %s, should have added LGTM.", tc.name)
				} else if len(fc.LabelsRemoved) > 0 {
					t.Errorf("For case %s, should not have removed LGTM.", tc.name)
				}
			}
		} else if len(fc.LabelsRemoved) > 0 {
			t.Errorf("For case %s, should not have removed LGTM.", tc.name)
		} else if (tc.hasLGTM && len(fc.LabelsAdded) > 1) || (!tc.hasLGTM && len(fc.LabelsAdded) > 0) {
			t.Errorf("For case %s, should not have added LGTM.", tc.name)
		}
		if tc.shouldComment && len(fc.IssueComments[5]) != 1 {
			t.Errorf("For case %s, should have commented.", tc.name)
		} else if !tc.shouldComment && len(fc.IssueComments[5]) != 0 {
			t.Errorf("For case %s, should not have commented.", tc.name)
		}
	}
}

func TestHandlePullRequest(t *testing.T) {
	SHA := "0bd3ed50c88cd53a09316bf7a298f900e9371652"
	treeSHA := "6dcb09b5b57875f334f61aebed695e2e4193db5e"
	cases := []struct {
		name             string
		event            github.PullRequestEvent
		removeLabelErr   error
		createCommentErr error

		err           error
		labelsAdded   []string
		labelsRemoved []string
		issueComments map[int][]github.IssueComment

		expectNoComments bool
	}{
		{
			name: "pr_synchronize, no RemoveLabel error",
			event: github.PullRequestEvent{
				Action: github.PullRequestActionSynchronize,
				PullRequest: github.PullRequest{
					Number: 101,
					Base: github.PullRequestBranch{
						Repo: github.Repo{
							Owner: github.User{
								Login: "kubernetes",
							},
							Name: "kubernetes",
						},
					},
					MergeSHA: &SHA,
				},
			},
			labelsRemoved: []string{LGTMLabel},
			issueComments: map[int][]github.IssueComment{
				101: {
					{
						Body: removeLGTMLabelNoti,
						User: github.User{Login: fakegithub.Bot},
					},
				},
			},
			expectNoComments: false,
		},
		{
			name: "pr_assigned",
			event: github.PullRequestEvent{
				Action: "assigned",
			},
			expectNoComments: true,
		},
		{
			name: "pr_synchronize, same tree-hash, keep label",
			event: github.PullRequestEvent{
				Action: github.PullRequestActionSynchronize,
				PullRequest: github.PullRequest{
					Number: 101,
					Base: github.PullRequestBranch{
						Repo: github.Repo{
							Owner: github.User{
								Login: "kubernetes",
							},
							Name: "kubernetes",
						},
					},
					MergeSHA: &SHA,
				},
			},
			issueComments: map[int][]github.IssueComment{
				101: {
					{
						Body: fmt.Sprintf(addLGTMLabelNotification, treeSHA),
						User: github.User{Login: fakegithub.Bot},
					},
				},
			},
			expectNoComments: true,
		},
		{
			name: "pr_synchronize, same tree-hash, keep label, edited comment",
			event: github.PullRequestEvent{
				Action: github.PullRequestActionSynchronize,
				PullRequest: github.PullRequest{
					Number: 101,
					Base: github.PullRequestBranch{
						Repo: github.Repo{
							Owner: github.User{
								Login: "kubernetes",
							},
							Name: "kubernetes",
						},
					},
					MergeSHA: &SHA,
				},
			},
			labelsRemoved: []string{LGTMLabel},
			issueComments: map[int][]github.IssueComment{
				101: {
					{
						Body:      fmt.Sprintf(addLGTMLabelNotification, treeSHA),
						User:      github.User{Login: fakegithub.Bot},
						CreatedAt: time.Date(1981, 2, 21, 12, 30, 0, 0, time.UTC),
						UpdatedAt: time.Date(1981, 2, 21, 12, 31, 0, 0, time.UTC),
					},
				},
			},
			expectNoComments: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeGitHub := &fakegithub.FakeClient{
				IssueComments: c.issueComments,
				PullRequests: map[int]*github.PullRequest{
					101: {
						Base: github.PullRequestBranch{
							Ref: "master",
						},
						MergeSHA: &SHA,
					},
				},
				Commits:     make(map[string]github.SingleCommit),
				LabelsAdded: c.labelsAdded,
			}
			fakeGitHub.LabelsAdded = append(fakeGitHub.LabelsAdded, "kubernetes/kubernetes#101:lgtm")
			commit := github.SingleCommit{}
			commit.Commit.Tree.SHA = treeSHA
			fakeGitHub.Commits[SHA] = commit
			pc := &plugins.Configuration{}
			pc.Lgtm = append(pc.Lgtm, plugins.Lgtm{
				Repos:         []string{"kubernetes/kubernetes"},
				StoreTreeHash: true,
			})
			err := handlePullRequest(
				logrus.WithField("plugin", "approve"),
				fakeGitHub,
				pc,
				&c.event,
			)

			if err != nil && c.err == nil {
				t.Fatalf("handlePullRequest error: %v", err)
			}

			if err == nil && c.err != nil {
				t.Fatalf("handlePullRequest wanted error: %v, got nil", c.err)
			}

			if got, want := err, c.err; !equality.Semantic.DeepEqual(got, want) {
				t.Fatalf("handlePullRequest error mismatch: got %v, want %v", got, want)
			}

			if got, want := len(fakeGitHub.LabelsRemoved), len(c.labelsRemoved); got != want {
				t.Logf("labelsRemoved: got %v, want: %v", fakeGitHub.LabelsRemoved, c.labelsRemoved)
				t.Fatalf("labelsRemoved length mismatch: got %d, want %d", got, want)
			}

			if got, want := fakeGitHub.IssueComments, c.issueComments; !equality.Semantic.DeepEqual(got, want) {
				t.Fatalf("LGTM revmoved notifications mismatch: got %v, want %v", got, want)
			}
			if c.expectNoComments && len(fakeGitHub.IssueCommentsAdded) > 0 {
				t.Fatalf("expected no comments but got %v", fakeGitHub.IssueCommentsAdded)
			}
			if !c.expectNoComments && len(fakeGitHub.IssueCommentsAdded) == 0 {
				t.Fatalf("expected comments but got none")
			}
		})
	}
}

func TestAddTreeHashComment(t *testing.T) {
	SHA := "0bd3ed50c88cd53a09316bf7a298f900e9371652"
	treeSHA := "6dcb09b5b57875f334f61aebed695e2e4193db5e"
	pc := &plugins.Configuration{}
	pc.Lgtm = append(pc.Lgtm, plugins.Lgtm{
		Repos:         []string{"kubernetes/kubernetes"},
		StoreTreeHash: true,
	})
	rc := reviewCtx{
		author:      "alice",
		issueAuthor: "bob",
		repo: github.Repo{
			Owner: github.User{
				Login: "kubernetes",
			},
			Name: "kubernetes",
		},
		number: 101,
		body:   "/lgtm",
	}
	fc := &fakegithub.FakeClient{
		Commits:       make(map[string]github.SingleCommit),
		IssueComments: map[int][]github.IssueComment{},
		PullRequests: map[int]*github.PullRequest{
			101: {
				Base: github.PullRequestBranch{
					Ref: "master",
				},
				MergeSHA: &SHA,
			},
		},
	}
	commit := github.SingleCommit{}
	commit.Commit.Tree.SHA = treeSHA
	fc.Commits[SHA] = commit
	handle(true, pc, &fakeOwnersClient{}, rc, fc, logrus.WithField("plugin", PluginName), &fakePruner{})
	found := false
	for _, body := range fc.IssueCommentsAdded {
		if addLGTMLabelNotificationRe.MatchString(body) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tree_hash comment but got none")
	}
}

func TestRemoveTreeHashComment(t *testing.T) {
	treeSHA := "6dcb09b5b57875f334f61aebed695e2e4193db5e"
	pc := &plugins.Configuration{}
	pc.Lgtm = append(pc.Lgtm, plugins.Lgtm{
		Repos:         []string{"kubernetes/kubernetes"},
		StoreTreeHash: true,
	})
	rc := reviewCtx{
		author:      "alice",
		issueAuthor: "bob",
		repo: github.Repo{
			Owner: github.User{
				Login: "kubernetes",
			},
			Name: "kubernetes",
		},
		assignees: []github.User{{Login: "alice"}},
		number:    101,
		body:      "/lgtm cancel",
	}
	fc := &fakegithub.FakeClient{
		IssueComments: map[int][]github.IssueComment{
			101: {
				{
					Body: fmt.Sprintf(addLGTMLabelNotification, treeSHA),
					User: github.User{Login: fakegithub.Bot},
				},
			},
		},
	}
	fc.LabelsAdded = []string{"kubernetes/kubernetes#101:" + LGTMLabel}
	fp := &fakePruner{
		GithubClient:  fc,
		IssueComments: fc.IssueComments[101],
	}
	handle(false, pc, &fakeOwnersClient{}, rc, fc, logrus.WithField("plugin", PluginName), fp)
	found := false
	for _, body := range fc.IssueCommentsDeleted {
		if addLGTMLabelNotificationRe.MatchString(body) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected deleted tree_hash comment but got none")
	}
}
