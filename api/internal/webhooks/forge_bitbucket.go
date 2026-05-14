// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import "encoding/json"

// ── Bitbucket Cloud event structs ─────────────────────────────────────────────

type bbPush struct {
	Push struct {
		Changes []struct {
			New struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Target struct {
					Hash    string `json:"hash"`
					Message string `json:"message"`
				} `json:"target"`
			} `json:"new"`
		} `json:"changes"`
	} `json:"push"`
}

type bbPR struct {
	PullRequest struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"` // OPEN | MERGED | DECLINED | SUPERSEDED
		Source struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
			Commit struct {
				Hash string `json:"hash"`
			} `json:"commit"`
		} `json:"source"`
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	} `json:"pullrequest"`
}

// parseBitbucket converts a Bitbucket Cloud webhook into a webhookEvent.
// eventKey is the X-Event-Key header value (e.g. "repo:push", "pullrequest:created").
func parseBitbucket(eventKey string, body []byte) (*webhookEvent, error) {
	switch eventKey {
	case "repo:push":
		var e bbPush
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		if len(e.Push.Changes) == 0 || e.Push.Changes[0].New.Name == "" {
			return nil, nil // delete or force-push with no new ref
		}
		ch := e.Push.Changes[0]
		if ch.New.Type == "tag" {
			return &webhookEvent{
				tagName:   ch.New.Name,
				commitSHA: ch.New.Target.Hash,
			}, nil
		}
		return &webhookEvent{
			trigger:       "push",
			runType:       "tracked",
			branch:        ch.New.Name,
			commitSHA:     ch.New.Target.Hash,
			commitMessage: firstLine(ch.New.Target.Message),
		}, nil

	case "pullrequest:created", "pullrequest:updated":
		var e bbPR
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		return &webhookEvent{
			trigger:       "pull_request",
			runType:       "proposed",
			branch:        e.PullRequest.Source.Branch.Name,
			commitSHA:     e.PullRequest.Source.Commit.Hash,
			commitMessage: e.PullRequest.Title,
			prNumber:      e.PullRequest.ID,
			prURL:         e.PullRequest.Links.HTML.Href,
		}, nil

	case "pullrequest:fulfilled", "pullrequest:rejected":
		var e bbPR
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, err
		}
		return &webhookEvent{
			prClosed: true,
			prNumber: e.PullRequest.ID,
			prURL:    e.PullRequest.Links.HTML.Href,
			branch:   e.PullRequest.Source.Branch.Name,
		}, nil

	default:
		return nil, nil // repo:fork, issue:created, etc.
	}
}
