// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import (
	"encoding/json"
	"strings"
)

// ── Azure DevOps event structs ────────────────────────────────────────────────

type adoEvent struct {
	EventType string          `json:"eventType"`
	Resource  json.RawMessage `json:"resource"`
}

type adoPush struct {
	RefUpdates []struct {
		Name        string `json:"name"`
		NewObjectID string `json:"newObjectId"`
	} `json:"refUpdates"`
	Commits []struct {
		Comment string `json:"comment"`
	} `json:"commits"`
}

type adoPR struct {
	PullRequestID        int    `json:"pullRequestId"`
	Title                string `json:"title"`
	Status               string `json:"status"` // active | abandoned | completed
	SourceRefName        string `json:"sourceRefName"`
	LastMergeSourceCommit struct {
		CommitID string `json:"commitId"`
	} `json:"lastMergeSourceCommit"`
	Links struct {
		Web struct {
			Href string `json:"href"`
		} `json:"web"`
	} `json:"_links"`
}

// parseAzureDevOps converts an Azure DevOps service hook payload into a webhookEvent.
func parseAzureDevOps(body []byte) (*webhookEvent, error) {
	var env adoEvent
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}

	switch env.EventType {
	case "git.push":
		var res adoPush
		if err := json.Unmarshal(env.Resource, &res); err != nil {
			return nil, err
		}
		if len(res.RefUpdates) == 0 {
			return nil, nil
		}
		ref := res.RefUpdates[0]
		if strings.HasPrefix(ref.Name, "refs/tags/") {
			tag := strings.TrimPrefix(ref.Name, "refs/tags/")
			return &webhookEvent{tagName: tag, commitSHA: ref.NewObjectID}, nil
		}
		branch := strings.TrimPrefix(ref.Name, "refs/heads/")
		msg := ""
		if len(res.Commits) > 0 {
			msg = firstLine(res.Commits[0].Comment)
		}
		return &webhookEvent{
			trigger:       "push",
			runType:       "tracked",
			branch:        branch,
			commitSHA:     ref.NewObjectID,
			commitMessage: msg,
		}, nil

	case "git.pullrequest.created", "git.pullrequest.updated":
		var res adoPR
		if err := json.Unmarshal(env.Resource, &res); err != nil {
			return nil, err
		}
		branch := strings.TrimPrefix(res.SourceRefName, "refs/heads/")
		return &webhookEvent{
			trigger:       "pull_request",
			runType:       "proposed",
			branch:        branch,
			commitSHA:     res.LastMergeSourceCommit.CommitID,
			commitMessage: res.Title,
			prNumber:      res.PullRequestID,
			prURL:         res.Links.Web.Href,
		}, nil

	case "git.pullrequest.merged", "git.pullrequest.declined":
		var res adoPR
		if err := json.Unmarshal(env.Resource, &res); err != nil {
			return nil, err
		}
		branch := strings.TrimPrefix(res.SourceRefName, "refs/heads/")
		return &webhookEvent{
			prClosed: true,
			prNumber: res.PullRequestID,
			prURL:    res.Links.Web.Href,
			branch:   branch,
		}, nil

	default:
		return nil, nil // build.complete, work item events, etc.
	}
}
