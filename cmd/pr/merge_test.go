package pr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v92/github"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

// fakeGitHub serves a PR whose GET responses step through states, so mergePR has to wait.
func fakeGitHub(t *testing.T, states []map[string]any, mergeResponses []int) (*github.Client, *[]string) {
	t.Helper()

	var calls []string
	gets, merges := 0, 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/pulls/1", func(w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "get")
		st := states[min(gets, len(states)-1)]
		gets++
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("PUT /repos/o/r/pulls/1/update-branch", func(w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "update-branch")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"message":"Updating pull request branch."}`))
	})
	mux.HandleFunc("PUT /repos/o/r/pulls/1/merge", func(w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "merge")
		code := mergeResponses[min(merges, len(mergeResponses)-1)]
		merges++
		w.WriteHeader(code)
		if code == http.StatusOK {
			_, _ = w.Write([]byte(`{"merged":true,"sha":"abcdef123456"}`))
		} else {
			_, _ = w.Write([]byte(`{"message":"Required status check \"ci\" is expected."}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	base := srv.URL + "/"
	client, err := github.NewClient(github.WithURLs(&base, &base), github.WithAuthToken("x"))
	if err != nil {
		t.Fatal(err)
	}
	return client, &calls
}

func pr(mergeable any, state string) map[string]any {
	return map[string]any{"number": 1, "state": "open", "mergeable": mergeable, "mergeable_state": state,
		"base": map[string]any{"ref": "main"}}
}

func testOpts() *mergeOptions {
	return &mergeOptions{strategy: "merge", waitTimeout: 5 * time.Second, pollInterval: time.Millisecond, updateBranch: true}
}

func TestMergePRWaitsUntilMergeable(t *testing.T) {
	client, calls := fakeGitHub(t,
		[]map[string]any{pr(nil, "unknown"), pr(true, "behind"), pr(true, "blocked"), pr(true, "clean")},
		[]int{http.StatusMethodNotAllowed, http.StatusOK})

	err := mergePR(context.Background(), client, gh.Repo{Owner: "o", Name: "r"}, &github.PullRequest{Number: github.Ptr(1)}, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	want := "get,get,update-branch,get,merge,get,merge"
	if got := strings.Join(*calls, ","); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
}

func TestMergePRConflict(t *testing.T) {
	client, calls := fakeGitHub(t, []map[string]any{pr(false, "dirty")}, []int{http.StatusOK})

	err := mergePR(context.Background(), client, gh.Repo{Owner: "o", Name: "r"}, &github.PullRequest{Number: github.Ptr(1)}, testOpts())
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if got := strings.Join(*calls, ","); got != "get" {
		t.Errorf("calls = %s, want get", got)
	}
}

func TestMergePRTimeout(t *testing.T) {
	client, _ := fakeGitHub(t, []map[string]any{pr(true, "blocked")}, []int{http.StatusMethodNotAllowed})

	opts := testOpts()
	opts.waitTimeout = 20 * time.Millisecond
	err := mergePR(context.Background(), client, gh.Repo{Owner: "o", Name: "r"}, &github.PullRequest{Number: github.Ptr(1)}, opts)
	if err == nil || !strings.Contains(err.Error(), "still not mergeable") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestMergePRFallsBackToSquash(t *testing.T) {
	var methods []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/pulls/1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pr(true, "clean"))
	})
	mux.HandleFunc("PUT /repos/o/r/pulls/1/merge", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MergeMethod string `json:"merge_method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		methods = append(methods, body.MergeMethod)
		if body.MergeMethod == "merge" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"Merge commits are not allowed on this repository."}`))
			return
		}
		_, _ = w.Write([]byte(`{"merged":true,"sha":"abcdef123456"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base := srv.URL + "/"
	client, err := github.NewClient(github.WithURLs(&base, &base), github.WithAuthToken("x"))
	if err != nil {
		t.Fatal(err)
	}

	err = mergePR(context.Background(), client, gh.Repo{Owner: "o", Name: "r"}, &github.PullRequest{Number: github.Ptr(1)}, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, ","); got != "merge,squash" {
		t.Errorf("merge methods = %s, want merge,squash", got)
	}
}
